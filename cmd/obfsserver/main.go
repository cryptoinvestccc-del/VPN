// Command obfsserver runs the server side of the obfuscated WireGuard
// tunnel: it listens publicly for obfuscated traffic and forwards decoded
// WireGuard packets to a local WireGuard server.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/cryptoinvestccc-del/vpn/internal/clients"
	"github.com/cryptoinvestccc-del/vpn/internal/config"
	"github.com/cryptoinvestccc-del/vpn/internal/metrics"
	"github.com/cryptoinvestccc-del/vpn/internal/transport"
)

// watchForReload re-reads the credential file on SIGHUP, so granting or
// revoking access does not require restarting the server and dropping
// every other client's tunnel with it.
func watchForReload(ctx context.Context, registry *clients.Registry) {
	if registry == nil {
		return
	}
	hangup := make(chan os.Signal, 1)
	signal.Notify(hangup, syscall.SIGHUP)

	go func() {
		defer signal.Stop(hangup)
		for {
			select {
			case <-ctx.Done():
				return
			case <-hangup:
				if err := registry.Reload(); err != nil {
					// The previous set stays in force: a typo in the
					// credential file must not lock everyone out.
					log.Printf("obfsserver: reload failed, keeping the previous credentials: %v", err)
					continue
				}
				log.Printf("obfsserver: credentials reloaded, %d clients enabled",
					registry.Current().Count())
			}
		}
	}()
}

func main() {
	configPath := flag.String("config", "obfsserver.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("obfsserver: failed to load config: %v", err)
	}

	psks, err := cfg.PSKs()
	if err != nil {
		log.Fatalf("obfsserver: invalid psk: %v", err)
	}

	var registry *clients.Registry
	if cfg.ClientsFile != "" {
		registry, err = clients.NewRegistry(cfg.ClientsFile)
		if err != nil {
			log.Fatalf("obfsserver: %v", err)
		}
		log.Printf("obfsserver: %d clients enabled from %s (SIGHUP reloads)",
			registry.Current().Count(), cfg.ClientsFile)
	}

	if cfg.LocalAddr == "" {
		log.Fatal("obfsserver: local_addr is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	watchForReload(ctx, registry)

	var stats *metrics.Registry
	if cfg.MetricsAddr != "" {
		stats = metrics.New(cfg.PerClientMetrics)
		if cfg.PerClientMetrics {
			log.Print("obfsserver: per-client metrics are on — the server will keep " +
				"per-device usage counters, which is a usage log in all but name")
		}
		go func() {
			if err := metrics.Serve(ctx, cfg.MetricsAddr, stats); err != nil {
				log.Printf("obfsserver: metrics endpoint stopped: %v", err)
			}
		}()
	}

	switch cfg.Mode {
	case "", "udp":
		if cfg.ListenWireAddr == "" {
			log.Fatal("obfsserver: listen_wire_addr is required in udp mode")
		}
		if len(psks) == 0 && registry == nil {
			log.Fatal("obfsserver: udp mode needs either psk or clients_file")
		}
		log.Printf("obfsserver: [udp] listen=%s -> local=%s", cfg.ListenWireAddr, cfg.LocalAddr)
		err = transport.RunServer(ctx, transport.Config{
			PSKs:           psks,
			Clients:        registry,
			Metrics:        stats,
			LocalAddr:      cfg.LocalAddr,
			ListenWireAddr: cfg.ListenWireAddr,
		})
	case "tls":
		if cfg.ListenTLSAddr == "" || cfg.CertFile == "" || cfg.KeyFile == "" {
			log.Fatal("obfsserver: listen_tls_addr, cert_file and key_file are required in tls mode")
		}
		if len(psks) == 0 && registry == nil {
			log.Fatal("obfsserver: tls mode needs either psk or clients_file — a credential is what " +
				"authorizes a peer, and the certificate is public, so its pin cannot serve that purpose")
		}
		fallback := cfg.FallbackAddr
		if fallback == "" {
			fallback = "none (canned response)"
		}
		log.Printf("obfsserver: [tls] listen=%s -> local=%s (fallback=%s)", cfg.ListenTLSAddr, cfg.LocalAddr, fallback)
		err = transport.RunServerTLS(ctx, transport.TLSConfig{
			PSKs:          psks,
			Clients:       registry,
			Metrics:       stats,
			LocalAddr:     cfg.LocalAddr,
			ListenTLSAddr: cfg.ListenTLSAddr,
			CertFile:      cfg.CertFile,
			KeyFile:       cfg.KeyFile,
			FallbackAddr:  cfg.FallbackAddr,
		})
	default:
		log.Fatalf("obfsserver: unknown mode %q (want \"udp\" or \"tls\")", cfg.Mode)
	}

	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("obfsserver: %v", err)
	}
	log.Print("obfsserver: shut down")
}

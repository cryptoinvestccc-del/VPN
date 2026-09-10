// Command obfsserver runs the server side of the obfuscated WireGuard
// tunnel: it listens publicly for obfuscated traffic and forwards decoded
// WireGuard packets to a local WireGuard server.
package main

import (
	"flag"
	"log"

	"github.com/cryptoinvestccc-del/vpn/internal/config"
	"github.com/cryptoinvestccc-del/vpn/internal/transport"
)

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

	if cfg.LocalAddr == "" {
		log.Fatal("obfsserver: local_addr is required")
	}

	switch cfg.Mode {
	case "", "udp":
		if cfg.ListenWireAddr == "" {
			log.Fatal("obfsserver: listen_wire_addr is required in udp mode")
		}
		if len(psks) == 0 {
			log.Fatal("obfsserver: psk is required in udp mode (no handshake exists yet to auto-derive one from)")
		}
		log.Printf("obfsserver: [udp] listen=%s -> local=%s", cfg.ListenWireAddr, cfg.LocalAddr)
		err = transport.RunServer(transport.Config{
			PSKs:           psks,
			LocalAddr:      cfg.LocalAddr,
			ListenWireAddr: cfg.ListenWireAddr,
		})
	case "tls":
		if cfg.ListenTLSAddr == "" || cfg.CertFile == "" || cfg.KeyFile == "" {
			log.Fatal("obfsserver: listen_tls_addr, cert_file and key_file are required in tls mode")
		}
		keySource := "auto-derived from TLS session"
		if len(psks) > 0 {
			keySource = "static psk"
		}
		log.Printf("obfsserver: [tls] listen=%s -> local=%s (key=%s)", cfg.ListenTLSAddr, cfg.LocalAddr, keySource)
		err = transport.RunServerTLS(transport.TLSConfig{
			PSKs:          psks,
			LocalAddr:     cfg.LocalAddr,
			ListenTLSAddr: cfg.ListenTLSAddr,
			CertFile:      cfg.CertFile,
			KeyFile:       cfg.KeyFile,
		})
	default:
		log.Fatalf("obfsserver: unknown mode %q (want \"udp\" or \"tls\")", cfg.Mode)
	}

	if err != nil {
		log.Fatalf("obfsserver: %v", err)
	}
}

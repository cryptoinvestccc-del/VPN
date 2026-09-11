// Command obfsclient runs the client side of the obfuscated WireGuard
// tunnel: it listens locally for plaintext WireGuard packets and forwards
// them, obfuscated, to a remote obfsserver.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os/signal"
	"syscall"

	"github.com/cryptoinvestccc-del/vpn/internal/config"
	"github.com/cryptoinvestccc-del/vpn/internal/transport"
)

func main() {
	configPath := flag.String("config", "obfsclient.yaml", "path to config file")
	profileArg := flag.String("profile", "", "connection profile: an obfsvpn:// link, or a file holding one")
	writeWG := flag.String("write-wireguard", "", "write the profile's WireGuard config here before connecting")
	wgStyle := flag.String("wireguard-style", "wg-quick", `dialect for -write-wireguard: "wg-quick" (desktop) or "app" (the WireGuard phone apps, which reject config hooks)`)
	flag.Parse()

	var (
		cfg config.File
		err error
	)
	if *profileArg != "" {
		p, perr := loadProfile(*profileArg)
		if perr != nil {
			log.Fatalf("obfsclient: %v", perr)
		}
		log.Printf("obfsclient: %s", p)
		cfg = configFromProfile(p)

		if *writeWG != "" {
			style, serr := wireGuardStyle(*wgStyle)
			if serr != nil {
				log.Fatalf("obfsclient: %v", serr)
			}
			if err := writeWireGuardConfig(p, *writeWG, style); err != nil {
				log.Fatalf("obfsclient: %v", err)
			}
		}
	} else {
		if *writeWG != "" {
			log.Fatal("obfsclient: -write-wireguard needs -profile; a YAML config carries no WireGuard settings")
		}
		cfg, err = config.Load(*configPath)
		if err != nil {
			log.Fatalf("obfsclient: failed to load config: %v", err)
		}
	}

	psks, err := cfg.PSKs()
	if err != nil {
		log.Fatalf("obfsclient: invalid psk: %v", err)
	}

	if cfg.LocalAddr == "" {
		log.Fatal("obfsclient: local_addr is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch cfg.Mode {
	case "", "udp":
		if cfg.RemoteWireAddr == "" {
			log.Fatal("obfsclient: remote_wire_addr is required in udp mode")
		}
		if len(psks) == 0 {
			log.Fatal("obfsclient: psk is required in udp mode (no handshake exists yet to auto-derive one from)")
		}
		log.Printf("obfsclient: [udp] local=%s -> remote=%s (junk=%d)", cfg.LocalAddr, cfg.RemoteWireAddr, cfg.JunkPackets)
		err = transport.RunClient(ctx, transport.Config{
			PSKs:           psks,
			LocalAddr:      cfg.LocalAddr,
			RemoteWireAddr: cfg.RemoteWireAddr,
			JunkPackets:    cfg.JunkPackets,
		})
	case "tls":
		if cfg.RemoteTLSAddr == "" || cfg.PinnedCertSHA256 == "" {
			log.Fatal("obfsclient: remote_tls_addr and pinned_cert_sha256 are required in tls mode")
		}
		if len(psks) == 0 {
			log.Fatal("obfsclient: psk is required in tls mode")
		}
		log.Printf("obfsclient: [tls] local=%s -> remote=%s (sni=%s)", cfg.LocalAddr, cfg.RemoteTLSAddr, cfg.ServerName)
		err = transport.RunClientTLS(ctx, transport.TLSConfig{
			PSKs:             psks,
			LocalAddr:        cfg.LocalAddr,
			RemoteTLSAddr:    cfg.RemoteTLSAddr,
			ServerName:       cfg.ServerName,
			PinnedCertSHA256: cfg.PinnedCertSHA256,
		})
	default:
		log.Fatalf("obfsclient: unknown mode %q (want \"udp\" or \"tls\")", cfg.Mode)
	}

	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("obfsclient: %v", err)
	}
	log.Print("obfsclient: shut down")
}

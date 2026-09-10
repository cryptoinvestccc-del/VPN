// Command obfsclient runs the client side of the obfuscated WireGuard
// tunnel: it listens locally for plaintext WireGuard packets and forwards
// them, obfuscated, to a remote obfsserver.
package main

import (
	"flag"
	"log"

	"github.com/cryptoinvestccc-del/vpn/internal/config"
	"github.com/cryptoinvestccc-del/vpn/internal/transport"
)

func main() {
	configPath := flag.String("config", "obfsclient.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("obfsclient: failed to load config: %v", err)
	}

	psk, err := cfg.PSK()
	if err != nil {
		log.Fatalf("obfsclient: invalid psk: %v", err)
	}

	if cfg.LocalAddr == "" {
		log.Fatal("obfsclient: local_addr is required")
	}

	switch cfg.Mode {
	case "", "udp":
		if cfg.RemoteWireAddr == "" {
			log.Fatal("obfsclient: remote_wire_addr is required in udp mode")
		}
		log.Printf("obfsclient: [udp] local=%s -> remote=%s (junk=%d)", cfg.LocalAddr, cfg.RemoteWireAddr, cfg.JunkPackets)
		err = transport.RunClient(transport.Config{
			PSK:            psk,
			LocalAddr:      cfg.LocalAddr,
			RemoteWireAddr: cfg.RemoteWireAddr,
			JunkPackets:    cfg.JunkPackets,
		})
	case "tls":
		if cfg.RemoteTLSAddr == "" || cfg.PinnedCertSHA256 == "" {
			log.Fatal("obfsclient: remote_tls_addr and pinned_cert_sha256 are required in tls mode")
		}
		log.Printf("obfsclient: [tls] local=%s -> remote=%s (sni=%s)", cfg.LocalAddr, cfg.RemoteTLSAddr, cfg.ServerName)
		err = transport.RunClientTLS(transport.TLSConfig{
			PSK:              psk,
			LocalAddr:        cfg.LocalAddr,
			RemoteTLSAddr:    cfg.RemoteTLSAddr,
			ServerName:       cfg.ServerName,
			PinnedCertSHA256: cfg.PinnedCertSHA256,
		})
	default:
		log.Fatalf("obfsclient: unknown mode %q (want \"udp\" or \"tls\")", cfg.Mode)
	}

	if err != nil {
		log.Fatalf("obfsclient: %v", err)
	}
}

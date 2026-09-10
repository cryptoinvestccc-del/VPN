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

	if cfg.LocalAddr == "" || cfg.RemoteWireAddr == "" {
		log.Fatal("obfsclient: local_addr and remote_wire_addr are required")
	}

	log.Printf("obfsclient: local=%s -> remote=%s (junk=%d)", cfg.LocalAddr, cfg.RemoteWireAddr, cfg.JunkPackets)

	err = transport.RunClient(transport.Config{
		PSK:            psk,
		LocalAddr:      cfg.LocalAddr,
		RemoteWireAddr: cfg.RemoteWireAddr,
		JunkPackets:    cfg.JunkPackets,
	})
	if err != nil {
		log.Fatalf("obfsclient: %v", err)
	}
}

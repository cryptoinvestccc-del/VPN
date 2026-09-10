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

	psk, err := cfg.PSK()
	if err != nil {
		log.Fatalf("obfsserver: invalid psk: %v", err)
	}

	if cfg.LocalAddr == "" || cfg.ListenWireAddr == "" {
		log.Fatal("obfsserver: local_addr and listen_wire_addr are required")
	}

	log.Printf("obfsserver: listen=%s -> local=%s", cfg.ListenWireAddr, cfg.LocalAddr)

	err = transport.RunServer(transport.Config{
		PSK:            psk,
		LocalAddr:      cfg.LocalAddr,
		ListenWireAddr: cfg.ListenWireAddr,
	})
	if err != nil {
		log.Fatalf("obfsserver: %v", err)
	}
}

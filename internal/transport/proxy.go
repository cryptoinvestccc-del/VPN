// Package transport implements the two ends of the obfuscated UDP tunnel:
// a local-facing proxy that talks plaintext WireGuard to a local peer, and
// a wire-facing proxy that exchanges obfuscated packets with the other side.
package transport

import (
	"log"
	"net"

	"github.com/cryptoinvestccc-del/vpn/internal/obfuscator"
)

// Config configures one end of the obfuscated tunnel.
type Config struct {
	// PSKs are the shared secrets used to wrap/unwrap packets, current
	// key first. Wrap uses PSKs[0]; Unwrap accepts any of them, which is
	// what allows rotating to a new key without downtime (see
	// config.File.PSKs).
	PSKs [][32]byte

	// LocalAddr is where we listen for/send plaintext WireGuard packets
	// (typically 127.0.0.1:<wg-port> on the client, or forwards to the
	// real WireGuard server on the server side).
	LocalAddr string

	// RemoteWireAddr is the other obfuscator endpoint (across the
	// network, subject to DPI).
	RemoteWireAddr string

	// ListenWireAddr is where we listen for obfuscated wire traffic.
	// On the client this is typically not bound (we dial out instead);
	// on the server this is the public-facing listener.
	ListenWireAddr string

	// JunkPackets is how many decoy packets to send to RemoteWireAddr
	// before the first real packet, to break "first packet looks like a
	// WG handshake" fingerprinting. 0 disables it.
	JunkPackets int
}

const maxUDPPacket = 65535

// RunClient proxies plaintext WireGuard packets from a local WireGuard
// client (connected to LocalAddr) out to RemoteWireAddr in obfuscated form,
// and delivers obfuscated responses back as plaintext.
func RunClient(cfg Config) error {
	obf, err := obfuscator.NewMulti(cfg.PSKs)
	if err != nil {
		return err
	}

	localConn, err := net.ListenPacket("udp", cfg.LocalAddr)
	if err != nil {
		return err
	}
	defer localConn.Close()

	remoteAddr, err := net.ResolveUDPAddr("udp", cfg.RemoteWireAddr)
	if err != nil {
		return err
	}
	wireConn, err := net.DialUDP("udp", nil, remoteAddr)
	if err != nil {
		return err
	}
	defer wireConn.Close()

	if cfg.JunkPackets > 0 {
		if err := sendJunk(wireConn, cfg.JunkPackets); err != nil {
			log.Printf("transport: junk send failed (continuing): %v", err)
		}
	}

	var lastLocalAddr net.Addr
	go func() {
		buf := make([]byte, maxUDPPacket)
		for {
			n, addr, err := localConn.ReadFrom(buf)
			if err != nil {
				return
			}
			lastLocalAddr = addr

			wrapped, err := obf.Wrap(buf[:n])
			if err != nil {
				log.Printf("transport: wrap failed: %v", err)
				continue
			}
			if _, err := wireConn.Write(wrapped); err != nil {
				log.Printf("transport: write to wire failed: %v", err)
			}
		}
	}()

	buf := make([]byte, maxUDPPacket)
	for {
		n, err := wireConn.Read(buf)
		if err != nil {
			return err
		}
		plaintext, err := obf.Unwrap(buf[:n])
		if err != nil {
			// Could be a junk packet reflected back, or noise
			// injected by an on-path observer. Drop silently.
			continue
		}
		if lastLocalAddr == nil {
			continue
		}
		if _, err := localConn.WriteTo(plaintext, lastLocalAddr); err != nil {
			log.Printf("transport: write to local failed: %v", err)
		}
	}
}

// RunServer listens for obfuscated wire traffic and forwards decoded
// WireGuard packets to a local WireGuard server, relaying responses back
// in obfuscated form.
func RunServer(cfg Config) error {
	obf, err := obfuscator.NewMulti(cfg.PSKs)
	if err != nil {
		return err
	}

	wireConn, err := net.ListenPacket("udp", cfg.ListenWireAddr)
	if err != nil {
		return err
	}
	defer wireConn.Close()

	localAddr, err := net.ResolveUDPAddr("udp", cfg.LocalAddr)
	if err != nil {
		return err
	}
	localConn, err := net.DialUDP("udp", nil, localAddr)
	if err != nil {
		return err
	}
	defer localConn.Close()

	var lastWireAddr net.Addr
	go func() {
		buf := make([]byte, maxUDPPacket)
		for {
			n, err := localConn.Read(buf)
			if err != nil {
				return
			}
			if lastWireAddr == nil {
				continue
			}
			wrapped, err := obf.Wrap(buf[:n])
			if err != nil {
				log.Printf("transport: wrap failed: %v", err)
				continue
			}
			if _, err := wireConn.WriteTo(wrapped, lastWireAddr); err != nil {
				log.Printf("transport: write to wire failed: %v", err)
			}
		}
	}()

	buf := make([]byte, maxUDPPacket)
	for {
		n, addr, err := wireConn.ReadFrom(buf)
		if err != nil {
			return err
		}
		plaintext, err := obf.Unwrap(buf[:n])
		if err != nil {
			// Junk packet or forged/garbage traffic: drop silently,
			// never respond (an oracle response would help an
			// active DPI prober distinguish us from random noise).
			continue
		}
		lastWireAddr = addr
		if _, err := localConn.Write(plaintext); err != nil {
			log.Printf("transport: write to local failed: %v", err)
		}
	}
}

func sendJunk(conn *net.UDPConn, count int) error {
	for i := 0; i < count; i++ {
		junk, err := obfuscator.Junk()
		if err != nil {
			return err
		}
		if _, err := conn.Write(junk); err != nil {
			return err
		}
	}
	return nil
}

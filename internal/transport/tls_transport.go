package transport

import (
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"net"

	"github.com/cryptoinvestccc-del/vpn/internal/obfuscator"
	"github.com/cryptoinvestccc-del/vpn/internal/tlscert"
)

// TLSConfig configures the TLS-carried variant of the tunnel. Unlike the
// plain UDP transport, TCP-over-TLS requires framing (length prefixes)
// since TLS delivers a byte stream, not discrete packets.
type TLSConfig struct {
	// PSKs are the shared secrets used to wrap/unwrap packets, current
	// key first — see Config.PSKs for the rotation rationale. Leave this
	// empty to auto-derive the obfuscation key from each TLS session
	// instead (see deriveSessionObfuscator): no manual key distribution
	// needed, and the key changes on every reconnect automatically. An
	// explicit PSK is still supported for defense-in-depth or if you
	// want a stable key across reconnects for some reason.
	PSKs [][32]byte

	// LocalAddr: same meaning as in Config (local WireGuard endpoint).
	LocalAddr string

	// Server side.
	ListenTLSAddr string
	CertFile      string
	KeyFile       string

	// Client side.
	RemoteTLSAddr string
	ServerName    string
	// PinnedCertSHA256 is the expected hex SHA-256 fingerprint of the
	// server's certificate (from `tlscert.PinFromCertFile`). Required:
	// we do not trust a public CA here, since the certificate is
	// self-signed and only exists to make the handshake look like real
	// TLS to on-path observers.
	PinnedCertSHA256 string
}

const maxFrameSize = 2048 // generous bound for an obfuscated WG packet

// keyExportLabel identifies our use of RFC 5705 TLS keying material
// export. Both sides must use the same label/context to derive the same
// key; it carries no secrecy itself.
const keyExportLabel = "obfsvpn obfuscation key v1"

// deriveSessionObfuscator derives the obfuscation key for one TLS
// connection from the already-established TLS session secret (RFC 5705
// exporter), instead of requiring a manually shared PSK. Both ends call
// this with the same label after their own handshake completes and get
// the same 32 bytes, because it's derived from the TLS master secret
// they just agreed on via ECDHE — no separate key exchange needed, and a
// fresh key is produced on every reconnect for free.
//
// This does not add confidentiality beyond what TLS itself provides
// against an on-path attacker: anyone who can decrypt the TLS session
// (e.g. by possessing its private key) can compute the same exporter
// value. Its purpose is automating key *management* for the padding/
// obfuscation layer, not adding a second independent secret.
func deriveSessionObfuscator(conn *tls.Conn) (*obfuscator.Obfuscator, error) {
	if err := conn.Handshake(); err != nil {
		return nil, err
	}
	state := conn.ConnectionState()
	material, err := state.ExportKeyingMaterial(keyExportLabel, nil, 32)
	if err != nil {
		return nil, err
	}
	var key [32]byte
	copy(key[:], material)
	return obfuscator.New(key)
}

// obfuscatorFor returns a ready-to-use Obfuscator for a TLS connection:
// the statically configured one when PSKs were supplied, or one derived
// per-session from the TLS exporter otherwise.
func obfuscatorFor(conn *tls.Conn, staticPSKs [][32]byte) (*obfuscator.Obfuscator, error) {
	if len(staticPSKs) > 0 {
		return obfuscator.NewMulti(staticPSKs)
	}
	return deriveSessionObfuscator(conn)
}

// frame format on the TLS stream: 2-byte big-endian length || payload
func writeFrame(w io.Writer, payload []byte) error {
	if len(payload) > maxFrameSize {
		return errors.New("transport: frame too large")
	}
	var hdr [2]byte
	binary.BigEndian.PutUint16(hdr[:], uint16(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func readFrame(r io.Reader) ([]byte, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint16(hdr[:])
	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// RunServerTLS accepts real TLS connections (so active DPI probing sees a
// legitimate TLS handshake) and relays obfuscated frames to/from a local
// WireGuard server.
func RunServerTLS(cfg TLSConfig) error {
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return err
	}

	ln, err := tls.Listen("tcp", cfg.ListenTLSAddr, &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	})
	if err != nil {
		return err
	}
	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go func() {
			tlsConn, ok := conn.(*tls.Conn)
			if !ok {
				log.Printf("transport: unexpected connection type from tls.Listen")
				conn.Close()
				return
			}
			obf, err := obfuscatorFor(tlsConn, cfg.PSKs)
			if err != nil {
				log.Printf("transport: failed to establish session key: %v", err)
				conn.Close()
				return
			}
			if err := serveTLSConn(conn, obf, cfg.LocalAddr); err != nil {
				log.Printf("transport: tls session ended: %v", err)
			}
		}()
	}
}

func serveTLSConn(conn net.Conn, obf *obfuscator.Obfuscator, localAddr string) error {
	defer conn.Close()

	udpAddr, err := net.ResolveUDPAddr("udp", localAddr)
	if err != nil {
		return err
	}
	localConn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return err
	}
	defer localConn.Close()

	errCh := make(chan error, 2)

	go func() {
		buf := make([]byte, maxUDPPacket)
		for {
			n, err := localConn.Read(buf)
			if err != nil {
				errCh <- err
				return
			}
			wrapped, err := obf.Wrap(buf[:n])
			if err != nil {
				log.Printf("transport: wrap failed: %v", err)
				continue
			}
			if err := writeFrame(conn, wrapped); err != nil {
				errCh <- err
				return
			}
		}
	}()

	go func() {
		for {
			frame, err := readFrame(conn)
			if err != nil {
				errCh <- err
				return
			}
			plaintext, err := obf.Unwrap(frame)
			if err != nil {
				// Junk/garbage frame: drop, keep the TLS
				// session alive (a closed connection on bad
				// input would itself be a signal to a prober).
				continue
			}
			if _, err := localConn.Write(plaintext); err != nil {
				log.Printf("transport: write to local failed: %v", err)
			}
		}
	}()

	return <-errCh
}

// RunClientTLS dials the server over real TLS (certificate pinned by
// SHA-256 fingerprint, since it's self-signed) and bridges local
// WireGuard traffic through it.
func RunClientTLS(cfg TLSConfig) error {
	if cfg.PinnedCertSHA256 == "" {
		return errors.New("transport: pinned_cert_sha256 is required for TLS client mode")
	}

	localConn, err := net.ListenPacket("udp", cfg.LocalAddr)
	if err != nil {
		return err
	}
	defer localConn.Close()

	tlsConf := &tls.Config{
		ServerName:         cfg.ServerName,
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true, // we verify via pinning below, not a CA
		VerifyConnection: func(state tls.ConnectionState) error {
			pin, err := tlscert.PinFromConnState(state)
			if err != nil {
				return err
			}
			if pin != cfg.PinnedCertSHA256 {
				return errors.New("transport: server certificate pin mismatch (possible MITM)")
			}
			return nil
		},
	}

	conn, err := tls.Dial("tcp", cfg.RemoteTLSAddr, tlsConf)
	if err != nil {
		return err
	}
	defer conn.Close()

	obf, err := obfuscatorFor(conn, cfg.PSKs)
	if err != nil {
		return err
	}

	var lastLocalAddr net.Addr
	errCh := make(chan error, 2)

	go func() {
		buf := make([]byte, maxUDPPacket)
		for {
			n, addr, err := localConn.ReadFrom(buf)
			if err != nil {
				errCh <- err
				return
			}
			lastLocalAddr = addr
			wrapped, err := obf.Wrap(buf[:n])
			if err != nil {
				log.Printf("transport: wrap failed: %v", err)
				continue
			}
			if err := writeFrame(conn, wrapped); err != nil {
				errCh <- err
				return
			}
		}
	}()

	go func() {
		for {
			frame, err := readFrame(conn)
			if err != nil {
				errCh <- err
				return
			}
			plaintext, err := obf.Unwrap(frame)
			if err != nil {
				continue
			}
			if lastLocalAddr == nil {
				continue
			}
			if _, err := localConn.WriteTo(plaintext, lastLocalAddr); err != nil {
				log.Printf("transport: write to local failed: %v", err)
			}
		}
	}()

	return <-errCh
}

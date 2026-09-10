package transport

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cryptoinvestccc-del/vpn/internal/obfuscator"
	"github.com/cryptoinvestccc-del/vpn/internal/tlscert"
)

// TLSConfig configures the TLS-carried variant of the tunnel. Unlike the
// plain UDP transport, TCP-over-TLS requires framing (length prefixes)
// since TLS delivers a byte stream, not discrete packets.
type TLSConfig struct {
	// PSKs are the shared secrets that authorize a peer, current key
	// first — see Config.PSKs for the rotation rationale. Required: the
	// per-connection packet keys are derived from a PSK together with
	// the TLS session (see deriveTrafficObfuscator), and without one any
	// stranger who completes a handshake could send traffic into the
	// WireGuard server behind this tunnel.
	PSKs [][32]byte

	// LocalAddr: same meaning as in Config (local WireGuard endpoint).
	LocalAddr string

	// Server side.
	ListenTLSAddr string
	CertFile      string
	KeyFile       string

	// FallbackAddr is a real HTTP server that unauthorized connections
	// are handed to, so the port answers probes exactly as the site
	// behind it would. Optional: without it, a canned nginx-style
	// response is returned instead, which is plausible but not
	// indistinguishable from a genuine site.
	FallbackAddr string

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

const (
	// MaxTunnelPacket is the largest plaintext packet the tunnel carries.
	// It covers jumbo frames, well past any WireGuard MTU in practice,
	// and bounds per-session buffers so a few thousand concurrent
	// sessions can't exhaust memory.
	MaxTunnelPacket = 9000

	// maxFrameSize bounds a single framed packet on the TLS stream. It
	// covers the largest packet Wrap can emit for a MaxTunnelPacket
	// payload, so framing never becomes the reason a legitimate packet
	// is dropped.
	maxFrameSize = MaxTunnelPacket + obfuscator.Overhead + obfuscator.MaxPadding

	// handshakeTimeout caps how long a connection may take to complete
	// its TLS handshake. Without it a probe can hold sockets open
	// indefinitely by starting handshakes it never finishes.
	handshakeTimeout = 15 * time.Second

	// idleTimeout closes TLS sessions that stop carrying traffic. The
	// client's keepalive-free WireGuard peer still re-handshakes every
	// couple of minutes, so this only reaps genuinely dead sessions.
	idleTimeout = 5 * time.Minute

	// idleCheckInterval is how often an idle session wakes to check
	// whether it has been quiet long enough to close.
	idleCheckInterval = 30 * time.Second

	// Reconnect backoff bounds for the client.
	reconnectMinDelay = 500 * time.Millisecond
	reconnectMaxDelay = 30 * time.Second

	// healthySessionDuration is how long a session must last to count as
	// working, which resets the reconnect backoff.
	healthySessionDuration = 60 * time.Second
)

// keyExportLabel identifies our use of RFC 5705 TLS keying material
// export. Both sides must use the same label/context to derive the same
// key; it carries no secrecy itself.
const keyExportLabel = "obfsvpn obfuscation key v1"

// ErrPinMismatch reports that the server presented a certificate other
// than the pinned one. It is deliberately fatal to the client rather than
// retryable: the benign explanations (a rotated certificate) and the
// hostile one (an interceptor) are indistinguishable from here, and only
// the operator can tell them apart.
var ErrPinMismatch = errors.New("transport: server certificate pin mismatch (possible MITM)")

// errFrameTooLarge marks a packet the framing can't carry. It is a
// per-packet fault, not a session fault: tearing down a working tunnel
// because one oversized datagram arrived would turn a dropped packet into
// an outage.
var errFrameTooLarge = errors.New("transport: frame exceeds maximum tunnel packet size")

// tooManySessions keeps the session-limit notice to one line per process
// rather than one per refused connection, which under a flood would be
// the flood.
var tooManySessions sync.Once

// frame format on the TLS stream: 2-byte big-endian length || payload
func writeFrame(w io.Writer, payload []byte) error {
	if len(payload) > maxFrameSize {
		return errFrameTooLarge
	}
	frame := make([]byte, 2+len(payload))
	binary.BigEndian.PutUint16(frame[:2], uint16(len(payload)))
	copy(frame[2:], payload)

	// One Write, so a frame always lands in a single TLS record: two
	// writes would split the length prefix and body into separate
	// records, handing a traffic analyst a fixed 2-byte record pattern
	// before every packet.
	_, err := w.Write(frame)
	return err
}

func readFrame(r io.Reader, buf []byte) ([]byte, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	size := int(binary.BigEndian.Uint16(hdr[:]))
	if size > len(buf) {
		return nil, errors.New("transport: frame exceeds buffer")
	}
	if _, err := io.ReadFull(r, buf[:size]); err != nil {
		return nil, err
	}
	return buf[:size], nil
}

// RunServerTLS accepts real TLS connections (so active DPI probing sees a
// legitimate TLS handshake) and relays obfuscated frames to/from a local
// WireGuard server. Each connection gets its own socket to the local
// WireGuard server, so concurrent clients never share a reply path.
func RunServerTLS(ctx context.Context, cfg TLSConfig) error {
	// Derived so in-flight handshakes and the shutdown watcher end when
	// this function returns.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return err
	}
	if len(cert.Certificate) > 0 {
		warnOnCertificateExpiry(cert.Certificate[0])
	}

	ln, err := tls.Listen("tcp", cfg.ListenTLSAddr, &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	})
	if err != nil {
		return err
	}
	defer ln.Close()

	go closeOnDone(ctx, ln)

	sessions := newSemaphore(maxConcurrentSessions)
	handshakes := newSemaphore(maxConcurrentHandshakes)

	for {
		conn, err := ln.Accept()
		if err != nil {
			return ctxErrOr(ctx, err)
		}

		// Refuse rather than queue when the session table is full: a
		// connection admitted here would sit unserved anyway, and the
		// bound exists precisely so that cannot happen.
		if !sessions.tryAcquire() {
			tooManySessions.Do(func() {
				log.Printf("transport: at the %d-session limit; further connections are "+
					"refused until sessions free up", maxConcurrentSessions)
			})
			conn.Close()
			continue
		}

		go func() {
			defer sessions.release()
			handleTLSConn(ctx, conn, cfg, handshakes)
		}()
	}
}

func handleTLSConn(ctx context.Context, conn net.Conn, cfg TLSConfig, handshakes semaphore) {
	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		conn.Close()
		return
	}

	// Complete the handshake under a deadline before doing anything
	// else, then clear it: the session itself is long-lived.
	if err := tlsConn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		conn.Close()
		return
	}

	// Bound how many handshakes run at once. The signature each one costs
	// is the expensive part of accepting a connection, and a flood of
	// them is the cheapest way to attack a TLS listener.
	handshakes.acquire()
	err := tlsConn.HandshakeContext(ctx)
	handshakes.release()

	if err != nil {
		// A failed handshake is routine here: DPI probes and internet
		// background scanning both produce them.
		conn.Close()
		return
	}
	if err := tlsConn.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		return
	}

	obf, err := deriveTrafficObfuscator(tlsConn, cfg.PSKs)
	if err != nil {
		log.Printf("transport: failed to establish session key: %v", err)
		conn.Close()
		return
	}

	// A peer must prove it holds the pre-shared key before it gets a path
	// to the WireGuard server. Anything else — a censor probing the port,
	// a scanner, a browser that wandered in — is handed to the fallback,
	// which answers the way an ordinary web server would.
	recorder := newRecordingReader(tlsConn)
	firstPacket, err := authenticatePeer(tlsConn, obf, recorder)
	if err != nil {
		serveFallback(tlsConn, recorder, cfg.FallbackAddr)
		return
	}
	recorder.stop()

	if err := serveTLSConn(tlsConn, obf, cfg.LocalAddr, firstPacket); err != nil {
		log.Printf("transport: tls session ended: %v", err)
	}
}

func serveTLSConn(conn net.Conn, obf *obfuscator.Obfuscator, localAddr string, firstPacket []byte) error {
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

	// The packet that authenticated this peer is ordinary traffic and
	// still has to reach WireGuard.
	if len(firstPacket) > 0 {
		if _, err := localConn.Write(firstPacket); err != nil {
			return err
		}
	}

	errCh := make(chan error, 2)

	// Tracks traffic in *either* direction. Timing the session out on
	// silence from the local WireGuard server alone would tear down a
	// tunnel that is actively carrying packets toward it — for instance
	// while a peer retries a handshake that isn't being answered yet.
	var lastActivity atomic.Int64
	lastActivity.Store(time.Now().UnixNano())

	go func() {
		buf := make([]byte, MaxTunnelPacket)
		for {
			// A coarse deadline keeps this loop interruptible
			// without paying for a syscall on every packet.
			if err := localConn.SetReadDeadline(time.Now().Add(idleCheckInterval)); err != nil {
				errCh <- err
				return
			}
			n, err := localConn.Read(buf)
			if err != nil {
				if isTimeout(err) {
					idle := time.Since(time.Unix(0, lastActivity.Load()))
					if idle < idleTimeout {
						continue
					}
				}
				errCh <- err
				return
			}
			lastActivity.Store(time.Now().UnixNano())
			wrapped, err := obf.Wrap(buf[:n])
			if err != nil {
				log.Printf("transport: wrap failed: %v", err)
				continue
			}
			warnIfOversized(len(wrapped))
			if err := writeFrame(conn, wrapped); err != nil {
				if errors.Is(err, errFrameTooLarge) {
					log.Printf("transport: dropping oversized packet (%d bytes)", n)
					continue
				}
				errCh <- err
				return
			}
		}
	}()

	go func() {
		buf := make([]byte, maxFrameSize)
		for {
			frame, err := readFrame(conn, buf)
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
			lastActivity.Store(time.Now().UnixNano())
			if _, err := localConn.Write(plaintext); err != nil {
				log.Printf("transport: write to local failed: %v", err)
			}
		}
	}()

	// Closing the connections on return unblocks whichever pump is still
	// running, so neither goroutine outlives the session.
	return <-errCh
}

// RunClientTLS dials the server over real TLS (certificate pinned by
// SHA-256 fingerprint, since it's self-signed) and bridges local
// WireGuard traffic through it, reconnecting with backoff whenever the
// session drops. A VPN client that gave up on the first network blip
// would leave the user's tunnel dead until someone restarted the service.
func RunClientTLS(ctx context.Context, cfg TLSConfig) error {
	if cfg.PinnedCertSHA256 == "" {
		return errors.New("transport: pinned_cert_sha256 is required for TLS client mode")
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	localConn, err := net.ListenPacket("udp", cfg.LocalAddr)
	if err != nil {
		return err
	}
	defer localConn.Close()

	go closeOnDone(ctx, localConn)

	// Learned from the local WireGuard peer's first packet and shared
	// across reconnects, so replies keep flowing to the right socket.
	var localPeer atomic.Pointer[net.Addr]

	delay := reconnectMinDelay
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		startedAt := time.Now()
		err := runClientTLSSession(ctx, cfg, localConn, &localPeer)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// A session that stayed up is evidence the server is healthy,
		// so the next reconnect starts fast again. Without this reset
		// a single earlier outage would leave every later reconnect
		// waiting the maximum backoff.
		if time.Since(startedAt) > healthySessionDuration {
			delay = reconnectMinDelay
		}
		if errors.Is(err, ErrPinMismatch) {
			// Not a transient fault: the server presented a
			// certificate we don't trust. Retrying would just keep
			// handing traffic to whoever is impersonating it.
			return err
		}
		if err != nil {
			log.Printf("transport: tls session lost (%v); reconnecting in %s", err, delay)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}

		delay *= 2
		if delay > reconnectMaxDelay {
			delay = reconnectMaxDelay
		}
	}
}

// runClientTLSSession runs one TLS session to completion, returning when
// it drops so the caller can reconnect.
func runClientTLSSession(ctx context.Context, cfg TLSConfig, localConn net.PacketConn, localPeer *atomic.Pointer[net.Addr]) error {
	// crypto/tls does not preserve error identity through the handshake,
	// so the verifier records the mismatch here for the caller to see.
	var pinMismatch atomic.Bool

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
				pinMismatch.Store(true)
				return ErrPinMismatch
			}
			return nil
		},
	}

	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: handshakeTimeout},
		Config:    tlsConf,
	}
	dialCtx, cancelDial := context.WithTimeout(ctx, handshakeTimeout)
	defer cancelDial()

	rawConn, err := dialer.DialContext(dialCtx, "tcp", cfg.RemoteTLSAddr)
	if err != nil {
		if pinMismatch.Load() {
			return ErrPinMismatch
		}
		return err
	}
	conn := rawConn.(*tls.Conn)
	defer conn.Close()

	obf, err := deriveTrafficObfuscator(conn, cfg.PSKs)
	if err != nil {
		return err
	}

	// Ends this session's pumps when the process is shutting down.
	sessionCtx, endSession := context.WithCancel(ctx)
	defer endSession()
	go func() {
		<-sessionCtx.Done()
		conn.Close()
	}()

	errCh := make(chan error, 2)

	// Local reads are shared across reconnects (the socket outlives the
	// session), so this pump must stop when the session ends rather than
	// keep consuming packets meant for the next connection. A short read
	// deadline lets it notice.
	go func() {
		buf := make([]byte, maxUDPPacket)
		for {
			if sessionCtx.Err() != nil {
				errCh <- sessionCtx.Err()
				return
			}
			if err := localConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				errCh <- err
				return
			}
			n, addr, err := localConn.ReadFrom(buf)
			if err != nil {
				if isTimeout(err) {
					continue
				}
				errCh <- err
				return
			}
			localPeer.Store(&addr)

			wrapped, err := obf.Wrap(buf[:n])
			if err != nil {
				log.Printf("transport: wrap failed: %v", err)
				continue
			}
			warnIfOversized(len(wrapped))
			if err := writeFrame(conn, wrapped); err != nil {
				if errors.Is(err, errFrameTooLarge) {
					log.Printf("transport: dropping oversized packet (%d bytes)", n)
					continue
				}
				errCh <- err
				return
			}
		}
	}()

	go func() {
		buf := make([]byte, maxFrameSize)
		for {
			frame, err := readFrame(conn, buf)
			if err != nil {
				errCh <- err
				return
			}
			plaintext, err := obf.Unwrap(frame)
			if err != nil {
				continue
			}
			peer := localPeer.Load()
			if peer == nil {
				continue
			}
			if _, err := localConn.WriteTo(plaintext, *peer); err != nil {
				log.Printf("transport: write to local failed: %v", err)
			}
		}
	}()

	err = <-errCh
	endSession()
	// Wait for the local pump to observe the cancellation before
	// returning, so the next session starts with sole ownership of the
	// local socket.
	<-errCh
	return err
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

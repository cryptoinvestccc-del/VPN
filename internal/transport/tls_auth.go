package transport

import (
	"bytes"
	"crypto/hkdf"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"time"

	"github.com/cryptoinvestccc-del/vpn/internal/obfuscator"
)

const (
	// trafficKeyLabel separates this derivation from any other use of the
	// same inputs.
	trafficKeyLabel = "obfsvpn traffic key v2"

	// authTimeout is how long a freshly connected peer has to prove it
	// holds the pre-shared key by sending one packet that authenticates.
	// It doubles as the slowloris bound: a peer that announces a frame
	// and stalls is dropped rather than holding a session open.
	authTimeout = 10 * time.Second

	// stalledPeerTimeout is the outer bound a stalled peer can occupy a
	// connection for. Kept equal to authTimeout, and named separately so
	// the test that checks the property reads clearly.
	stalledPeerTimeout = authTimeout

	// drainWindow and drainLimit bound how much of an unauthorized
	// peer's request is collected before the fallback answers it.
	drainWindow = 2 * time.Second
	drainLimit  = 64 << 10
)

// ErrUnauthenticated reports that a peer never proved knowledge of the
// pre-shared key.
var ErrUnauthenticated = errors.New("transport: peer did not authenticate")

// deriveTrafficObfuscator derives this connection's packet keys from two
// inputs that must both be present:
//
//   - the TLS session's exporter value (RFC 5705), which binds the keys to
//     this specific connection, so frames captured from one session cannot
//     be replayed into another; and
//   - the pre-shared key, which is what actually authorizes the peer.
//
// Deriving from the exporter alone — as an earlier version of this code
// did — authenticates nobody: the server's certificate is shown to every
// client that connects, so its fingerprint is not a secret, and any
// stranger completing a handshake computes the same exporter value the
// server does. The pre-shared key is the only input an unauthorized peer
// cannot supply.
//
// One key is derived per configured PSK, so key rotation keeps working:
// the server accepts frames under the current or the previous key.
func deriveTrafficObfuscator(conn *tls.Conn, psks [][32]byte) (*obfuscator.Obfuscator, error) {
	if len(psks) == 0 {
		return nil, errors.New("transport: a pre-shared key is required in TLS mode")
	}

	state := conn.ConnectionState()
	exporter, err := state.ExportKeyingMaterial(keyExportLabel, nil, 32)
	if err != nil {
		return nil, err
	}

	keys := make([][32]byte, 0, len(psks))
	for _, psk := range psks {
		derived, err := hkdf.Key(sha256.New, exporter, psk[:], trafficKeyLabel, 32)
		if err != nil {
			return nil, err
		}
		var key [32]byte
		copy(key[:], derived)
		keys = append(keys, key)
	}
	return obfuscator.NewMulti(keys)
}

// recordingReader passes reads through while keeping a copy of everything
// consumed, so bytes already read from a peer that turns out to be
// unauthorized can still be handed to the fallback handler — which needs
// them to answer as an ordinary web server would.
type recordingReader struct {
	r        io.Reader
	consumed bytes.Buffer
	enabled  bool
}

func newRecordingReader(r io.Reader) *recordingReader {
	return &recordingReader{r: r, enabled: true}
}

func (rr *recordingReader) Read(p []byte) (int, error) {
	n, err := rr.r.Read(p)
	if n > 0 && rr.enabled {
		rr.consumed.Write(p[:n])
	}
	return n, err
}

// stop ends recording once a peer has authenticated, so an established
// session doesn't accumulate a copy of its own traffic.
func (rr *recordingReader) stop() { rr.enabled = false }

// authenticatePeer reads the first frame from a newly connected peer and
// requires it to authenticate under the derived keys. The decrypted
// packet is returned so it can be forwarded — it is ordinary traffic, not
// a separate authentication handshake, which keeps the exchange
// indistinguishable from the rest of the session.
func authenticatePeer(conn *tls.Conn, obf *obfuscator.Obfuscator, r io.Reader) ([]byte, error) {
	if err := conn.SetReadDeadline(time.Now().Add(authTimeout)); err != nil {
		return nil, err
	}

	buf := make([]byte, maxFrameSize)
	frame, err := readFrame(r, buf)
	if err != nil {
		return nil, err
	}

	packet, err := obf.Unwrap(frame)
	if err != nil {
		return nil, ErrUnauthenticated
	}

	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return nil, err
	}
	return packet, nil
}

// serveFallback makes an unauthorized connection look like what a censor
// expects to find on a TLS port: an ordinary web server.
//
// This matters as much as the access control itself. A server that
// accepts arbitrary bytes, answers nothing and never hangs up is a
// fingerprint — no real HTTPS server behaves that way, so an active
// prober can identify the port by how it *fails*. Handing unauthorized
// peers to a real site (or, failing that, a plausible canned response)
// removes that signal.
func serveFallback(conn net.Conn, r *recordingReader, fallbackAddr string) {
	defer conn.Close()

	// Framing consumed only the first two bytes of whatever the peer
	// sent — for an HTTP request, that's "GE" of "GET". Read the rest of
	// what is already in flight so the fallback sees a whole request:
	// answering a truncated one would be a fingerprint of its own.
	drainPending(conn, r)
	consumed := r.consumed.Bytes()

	if fallbackAddr != "" {
		if proxyToFallback(conn, consumed, fallbackAddr) {
			return
		}
		// Backend unreachable: fall through to the canned response
		// rather than dropping the peer, which would restore the very
		// signal this exists to remove.
	}

	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, _ = conn.Write(cannedResponse(consumed))
}

// drainPending collects what the peer has already sent, bounded in both
// size and time so a peer that keeps talking — or one that says nothing
// more — cannot stall the fallback.
func drainPending(conn net.Conn, r *recordingReader) {
	_ = conn.SetReadDeadline(time.Now().Add(drainWindow))
	_, _ = io.CopyN(io.Discard, r, drainLimit)
	_ = conn.SetReadDeadline(time.Time{})
}

// proxyToFallback hands the connection to a real HTTP server, replaying
// the bytes already read. Reports whether the handover succeeded.
func proxyToFallback(conn net.Conn, consumed []byte, fallbackAddr string) bool {
	backend, err := net.DialTimeout("tcp", fallbackAddr, 5*time.Second)
	if err != nil {
		return false
	}
	defer backend.Close()

	if len(consumed) > 0 {
		if _, err := backend.Write(consumed); err != nil {
			return false
		}
	}

	// The response path decides when this ends: once the site has
	// answered and closed, we close too. The request path is not waited
	// on — a peer that holds its write side open forever would otherwise
	// keep the connection alive indefinitely.
	go func() {
		_, _ = io.Copy(backend, conn)
	}()
	_, _ = io.Copy(conn, backend)
	return true
}

// cannedResponse answers the way a stock nginx would: a 404 for something
// that parses as an HTTP request, a 400 for anything else.
func cannedResponse(consumed []byte) []byte {
	if looksLikeHTTPRequest(consumed) {
		return []byte("HTTP/1.1 404 Not Found\r\n" +
			"Server: nginx\r\n" +
			"Content-Type: text/html\r\n" +
			"Content-Length: 146\r\n" +
			"Connection: close\r\n" +
			"\r\n" +
			"<html>\r\n<head><title>404 Not Found</title></head>\r\n" +
			"<body>\r\n<center><h1>404 Not Found</h1></center>\r\n" +
			"<hr><center>nginx</center>\r\n</body>\r\n</html>\r\n")
	}

	return []byte("HTTP/1.1 400 Bad Request\r\n" +
		"Server: nginx\r\n" +
		"Content-Type: text/html\r\n" +
		"Content-Length: 150\r\n" +
		"Connection: close\r\n" +
		"\r\n" +
		"<html>\r\n<head><title>400 Bad Request</title></head>\r\n" +
		"<body>\r\n<center><h1>400 Bad Request</h1></center>\r\n" +
		"<hr><center>nginx</center>\r\n</body>\r\n</html>\r\n")
}

func looksLikeHTTPRequest(data []byte) bool {
	for _, method := range [][]byte{
		[]byte("GET "), []byte("POST "), []byte("HEAD "), []byte("PUT "),
		[]byte("DELETE "), []byte("OPTIONS "), []byte("PATCH "), []byte("CONNECT "),
	} {
		if bytes.HasPrefix(data, method) {
			return true
		}
	}
	return false
}

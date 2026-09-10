package transport

import (
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cryptoinvestccc-del/vpn/internal/obfuscator"
	"github.com/cryptoinvestccc-del/vpn/internal/tlscert"
)

// captureValidPacket produces one genuine wrapped packet, standing in for
// what an on-path observer records off the wire. The observer cannot read
// it, but it is a valid, authenticating packet they can resend.
func captureValidPacket(t *testing.T, psk [32]byte) []byte {
	t.Helper()
	obf, err := obfuscator.New(psk)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := obf.Wrap([]byte("a captured wireguard packet"))
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

// TestSessionTableRejectsReplayedPacket is the same attack at the level
// where it can be observed directly.
func TestSessionTableRejectsReplayedPacket(t *testing.T) {
	psk := testPSK(t)
	obf, err := obfuscator.New(psk)
	if err != nil {
		t.Fatal(err)
	}

	peerAddr, _, closePeer := realisticWireGuardPeer(t)
	defer closePeer()
	localAddr, err := net.ResolveUDPAddr("udp", peerAddr)
	if err != nil {
		t.Fatal(err)
	}
	wireConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer wireConn.Close()

	table := newSessionTable(wireConn, localAddr, singleClientRegistry(t, psk))
	defer table.closeAll()

	captured, err := obf.Wrap([]byte("captured packet"))
	if err != nil {
		t.Fatal(err)
	}

	// First arrival: legitimate, opens a session.
	if _, err := table.open(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40001}, captured, sharedClientID, obf); err != nil {
		t.Fatalf("first delivery should be accepted: %v", err)
	}

	// The same bytes replayed from other addresses must be refused.
	for i := 2; i <= 20; i++ {
		addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40000 + i}
		_, err := table.open(addr, captured, sharedClientID, obf)
		if !errors.Is(err, errReplayedPacket) {
			t.Fatalf("replay from %s was accepted (err=%v); one captured packet can spawn unlimited sessions", addr, err)
		}
	}

	if got := table.count(); got != 1 {
		t.Fatalf("replays created %d sessions, want 1", got)
	}
}

// TestSessionTableEvictsRatherThanRefusing checks that a full table keeps
// serving new peers by dropping the least recently used one. Refusing new
// peers instead would let whoever filled the table lock every later
// client out.
func TestSessionTableEvictsRatherThanRefusing(t *testing.T) {
	psk := testPSK(t)
	obf, err := obfuscator.New(psk)
	if err != nil {
		t.Fatal(err)
	}

	peerAddr, _, closePeer := realisticWireGuardPeer(t)
	defer closePeer()
	localAddr, err := net.ResolveUDPAddr("udp", peerAddr)
	if err != nil {
		t.Fatal(err)
	}
	wireConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer wireConn.Close()

	table := newSessionTable(wireConn, localAddr, singleClientRegistry(t, psk))
	table.maxSessions = 8
	defer table.closeAll()

	for i := 0; i < 8; i++ {
		addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, byte(i)), Port: 5000}
		packet, err := obf.Wrap([]byte{byte(i)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := table.open(addr, packet, sharedClientID, obf); err != nil {
			t.Fatal(err)
		}
	}

	// A new peer arriving at a full table must still be served.
	newcomer := &net.UDPAddr{IP: net.IPv4(10, 0, 1, 1), Port: 5000}
	packet, err := obf.Wrap([]byte("newcomer"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := table.open(newcomer, packet, sharedClientID, obf); err != nil {
		t.Fatalf("a full session table locked out a new client: %v", err)
	}
	if got := table.count(); got > 8 {
		t.Fatalf("table grew past its limit: %d sessions", got)
	}
}

// TestAnonymousClientCannotTunnelWithoutPSK probes the access-control
// property of TLS mode. The server's certificate is public by
// construction — every client sees it during the handshake — so its pin
// is not a secret and cannot serve as an authenticator. If deriving the
// obfuscation key from the TLS session is all that stands between a
// stranger and the WireGuard server, then anyone who can reach the port
// can push packets into it.
func TestAnonymousClientCannotTunnelWithoutPSK(t *testing.T) {
	ctx := testContext(t)
	certPath, keyPath, _ := testCert(t)

	psk := testPSK(t)

	peerAddr, sources, closePeer := realisticWireGuardPeer(t)
	defer closePeer()

	serverTLSAddr := freeTCPAddr(t)
	go func() {
		_ = RunServerTLS(ctx, TLSConfig{
			PSKs:          [][32]byte{psk},
			LocalAddr:     peerAddr,
			ListenTLSAddr: serverTLSAddr,
			CertFile:      certPath,
			KeyFile:       keyPath,
		})
	}()
	time.Sleep(200 * time.Millisecond)

	before := sources()

	// A stranger: no PSK, no credentials, nothing but the address.
	conn, err := tls.Dial("tcp", serverTLSAddr, &tls.Config{
		ServerName:         "test.local",
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// They completed the handshake, so they can compute the exporter
	// value — but without the pre-shared key that is not enough to
	// derive the packet keys. Their best attempt uses a key of their own.
	var guessed [32]byte
	obf, err := clientObfuscator(conn, [][32]byte{guessed})
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := obf.Wrap([]byte("traffic from an unauthorized client"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFrame(conn, wrapped); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)

	if sources() > before {
		t.Fatal("an anonymous client with no shared secret reached the WireGuard server through the tunnel")
	}
}

// TestTLSSessionSurvivesStalledPeer covers slowloris: a peer that opens a
// connection, starts a frame and never finishes it must not hold server
// resources indefinitely.
func TestTLSSessionSurvivesStalledPeer(t *testing.T) {
	ctx := testContext(t)
	certPath, keyPath, _ := testCert(t)

	psk := testPSK(t)

	peerAddr, _, closePeer := realisticWireGuardPeer(t)
	defer closePeer()

	serverTLSAddr := freeTCPAddr(t)
	go func() {
		_ = RunServerTLS(ctx, TLSConfig{
			PSKs:          [][32]byte{psk},
			LocalAddr:     peerAddr,
			ListenTLSAddr: serverTLSAddr,
			CertFile:      certPath,
			KeyFile:       keyPath,
		})
	}()
	time.Sleep(200 * time.Millisecond)

	conn, err := tls.Dial("tcp", serverTLSAddr, &tls.Config{
		ServerName:         "test.local",
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Announce a frame, then send nothing more.
	if _, err := conn.Write([]byte{0x01, 0x00}); err != nil {
		t.Fatal(err)
	}

	// The server must let go of the connection rather than holding it
	// open forever waiting for a body that never comes. Reaching EOF is
	// what proves it did; whether a fallback response precedes the close
	// is a separate property, covered by the probe tests below.
	if err := conn.SetReadDeadline(time.Now().Add(stalledPeerTimeout + 10*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, conn); err != nil && isTimeout(err) {
		t.Fatalf("server held a stalled connection open past %s", stalledPeerTimeout)
	}
}

// TestUnauthorizedPeerGetsWebServerResponse covers the property that makes
// TLS mode worth using: a censor probing the port must find something that
// behaves like an ordinary web server. A port that accepts arbitrary bytes,
// answers nothing and never hangs up is itself a fingerprint — no real
// HTTPS server does that, so probing would identify the tunnel by how it
// fails rather than by what it carries.
func TestUnauthorizedPeerGetsWebServerResponse(t *testing.T) {
	ctx := testContext(t)
	psk := testPSK(t)
	certPath, keyPath, _ := testCert(t)

	peerAddr, _, closePeer := realisticWireGuardPeer(t)
	defer closePeer()

	serverTLSAddr := freeTCPAddr(t)
	go func() {
		_ = RunServerTLS(ctx, TLSConfig{
			PSKs:          [][32]byte{psk},
			LocalAddr:     peerAddr,
			ListenTLSAddr: serverTLSAddr,
			CertFile:      certPath,
			KeyFile:       keyPath,
		})
	}()
	time.Sleep(200 * time.Millisecond)

	for _, tc := range []struct {
		name    string
		send    string
		wantHdr string
	}{
		{"http request", "GET / HTTP/1.1\r\nHost: www.example.com\r\n\r\n", "HTTP/1.1 404"},
		{"random bytes", "\xff\xfe\xfd\xfc not http at all", "HTTP/1.1 400"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := tls.Dial("tcp", serverTLSAddr, &tls.Config{
				ServerName:         "test.local",
				MinVersion:         tls.VersionTLS13,
				InsecureSkipVerify: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()

			if _, err := conn.Write([]byte(tc.send)); err != nil {
				t.Fatal(err)
			}
			if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
				t.Fatal(err)
			}

			buf := make([]byte, 512)
			n, err := conn.Read(buf)
			if err != nil {
				t.Fatalf("probe got no response at all, which is itself a fingerprint: %v", err)
			}
			if got := string(buf[:n]); !strings.HasPrefix(got, tc.wantHdr) {
				t.Fatalf("probe got %q, want a response starting %q", got, tc.wantHdr)
			}
		})
	}
}

// TestFallbackProxiesToRealSite checks the stronger option: when a real
// web server is configured, unauthorized peers are handed to it, so the
// port is not merely plausible but actually serves the site behind it.
func TestFallbackProxiesToRealSite(t *testing.T) {
	ctx := testContext(t)
	psk := testPSK(t)
	certPath, keyPath, _ := testCert(t)

	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "a-real-website")
		_, _ = w.Write([]byte("hello from the cover site"))
	}))
	defer site.Close()
	siteAddr := strings.TrimPrefix(site.URL, "http://")

	peerAddr, _, closePeer := realisticWireGuardPeer(t)
	defer closePeer()

	serverTLSAddr := freeTCPAddr(t)
	go func() {
		_ = RunServerTLS(ctx, TLSConfig{
			PSKs:          [][32]byte{psk},
			LocalAddr:     peerAddr,
			ListenTLSAddr: serverTLSAddr,
			CertFile:      certPath,
			KeyFile:       keyPath,
			FallbackAddr:  siteAddr,
		})
	}()
	time.Sleep(200 * time.Millisecond)

	conn, err := tls.Dial("tcp", serverTLSAddr, &tls.Config{
		ServerName:         "test.local",
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("GET / HTTP/1.1\r\nHost: www.example.com\r\nConnection: close\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}

	body, err := io.ReadAll(conn)
	if err != nil && isTimeout(err) {
		t.Fatalf("fallback never answered: %v", err)
	}
	if !strings.Contains(string(body), "hello from the cover site") {
		t.Fatalf("probe did not reach the cover site; got %q", body)
	}
}

func TestTLSCertificateIsPinnedNotTrustedByCA(t *testing.T) {
	certPath, _, pin := testCert(t)

	// Sanity check on the property the client relies on: the pin is
	// derived from the certificate the server actually presents, and a
	// self-signed certificate would never pass ordinary CA validation —
	// which is exactly why pinning is mandatory rather than optional.
	filePin, err := tlscert.PinFromCertFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if filePin != pin {
		t.Fatal("pin is not reproducible from the certificate file")
	}
}

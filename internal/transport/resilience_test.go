package transport

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cryptoinvestccc-del/vpn/internal/obfuscator"
	"github.com/cryptoinvestccc-del/vpn/internal/tlscert"
)

// testPSK returns a random pre-shared key for tests.
func testPSK(t *testing.T) [32]byte {
	t.Helper()
	var psk [32]byte
	if _, err := rand.Read(psk[:]); err != nil {
		t.Fatal(err)
	}
	return psk
}

func testCert(t *testing.T) (certPath, keyPath, pin string) {
	t.Helper()
	dir := t.TempDir()
	certPath = filepath.Join(dir, "server.crt")
	keyPath = filepath.Join(dir, "server.key")
	if err := tlscert.Generate("test.local", certPath, keyPath); err != nil {
		t.Fatal(err)
	}
	pin, err := tlscert.PinFromCertFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath, pin
}

// exchange sends one payload through the tunnel entrance and returns the
// echoed reply, failing the test if none arrives in time.
func exchange(t *testing.T, entrance string, payload []byte, timeout time.Duration) []byte {
	t.Helper()
	conn, err := net.Dial("udp", entrance)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if _, err := conn.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 65535)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("no reply through tunnel: %v", err)
	}
	return buf[:n]
}

// TestTLSConcurrentClients is the TLS-mode counterpart of
// TestConcurrentClients: several clients sharing one server must each get
// their own traffic back.
func TestTLSConcurrentClients(t *testing.T) {
	ctx := testContext(t)
	psk := testPSK(t)
	certPath, keyPath, pin := testCert(t)

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
	time.Sleep(150 * time.Millisecond)

	const clients = 4
	entrances := make([]string, clients)
	for i := range entrances {
		entrances[i] = freeUDPAddr(t)
		go func(local string) {
			_ = RunClientTLS(ctx, TLSConfig{
				PSKs:             [][32]byte{psk},
				LocalAddr:        local,
				RemoteTLSAddr:    serverTLSAddr,
				ServerName:       "test.local",
				PinnedCertSHA256: pin,
			})
		}(entrances[i])
	}
	time.Sleep(400 * time.Millisecond)

	var wg sync.WaitGroup
	errs := make(chan error, clients)
	for i, entrance := range entrances {
		wg.Add(1)
		go func(id int, entrance string) {
			defer wg.Done()

			conn, err := net.Dial("udp", entrance)
			if err != nil {
				errs <- err
				return
			}
			defer conn.Close()

			payload := []byte(fmt.Sprintf("tls-client-%d-marker", id))
			for round := 0; round < 5; round++ {
				if _, err := conn.Write(payload); err != nil {
					errs <- err
					return
				}
				conn.SetReadDeadline(time.Now().Add(3 * time.Second))
				buf := make([]byte, 65535)
				n, err := conn.Read(buf)
				if err != nil {
					errs <- fmt.Errorf("tls client %d round %d: %w", id, round, err)
					return
				}
				if !bytes.Equal(buf[:n], payload) {
					errs <- fmt.Errorf("tls client %d received another client's data: %q", id, buf[:n])
					return
				}
			}
		}(i, entrance)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestTLSClientReconnectsAfterServerRestart is the failure mode a real
// deployment hits constantly: the server restarts (upgrade, reboot, blip)
// and the client must recover on its own rather than staying dead until
// someone restarts it.
func TestTLSClientReconnectsAfterServerRestart(t *testing.T) {
	ctx := testContext(t)
	psk := testPSK(t)
	certPath, keyPath, pin := testCert(t)

	peerAddr, _, closePeer := realisticWireGuardPeer(t)
	defer closePeer()

	serverTLSAddr := freeTCPAddr(t)
	clientLocalAddr := freeUDPAddr(t)

	serverCtx, stopServer := context.WithCancel(ctx)
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		_ = RunServerTLS(serverCtx, TLSConfig{
			PSKs:          [][32]byte{psk},
			LocalAddr:     peerAddr,
			ListenTLSAddr: serverTLSAddr,
			CertFile:      certPath,
			KeyFile:       keyPath,
		})
	}()
	time.Sleep(150 * time.Millisecond)

	go func() {
		_ = RunClientTLS(ctx, TLSConfig{
			PSKs:             [][32]byte{psk},
			LocalAddr:        clientLocalAddr,
			RemoteTLSAddr:    serverTLSAddr,
			ServerName:       "test.local",
			PinnedCertSHA256: pin,
		})
	}()
	time.Sleep(300 * time.Millisecond)

	before := exchange(t, clientLocalAddr, []byte("before restart"), 3*time.Second)
	if !bytes.Equal(before, []byte("before restart")) {
		t.Fatalf("unexpected reply before restart: %q", before)
	}

	stopServer()
	<-serverDone
	time.Sleep(200 * time.Millisecond)

	// Same address, fresh server process.
	go func() {
		_ = RunServerTLS(ctx, TLSConfig{
			PSKs:          [][32]byte{psk},
			LocalAddr:     peerAddr,
			ListenTLSAddr: serverTLSAddr,
			CertFile:      certPath,
			KeyFile:       keyPath,
		})
	}()

	// The client backs off between attempts, so give it room to notice
	// the server is back.
	deadline := time.Now().Add(15 * time.Second)
	for {
		conn, err := net.Dial("udp", clientLocalAddr)
		if err != nil {
			t.Fatal(err)
		}
		payload := []byte("after restart")
		_, _ = conn.Write(payload)
		conn.SetReadDeadline(time.Now().Add(time.Second))
		buf := make([]byte, 65535)
		n, err := conn.Read(buf)
		conn.Close()

		if err == nil && bytes.Equal(buf[:n], payload) {
			return // recovered
		}
		if time.Now().After(deadline) {
			t.Fatal("client never reconnected after the server came back")
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// TestTLSPinMismatchIsFatal makes sure a certificate that doesn't match
// the pin stops the client instead of sending it into an endless
// reconnect loop against a possible interceptor.
func TestTLSPinMismatchIsFatal(t *testing.T) {
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
	time.Sleep(150 * time.Millisecond)

	done := make(chan error, 1)
	go func() {
		done <- RunClientTLS(ctx, TLSConfig{
			PSKs:             [][32]byte{psk},
			LocalAddr:        freeUDPAddr(t),
			RemoteTLSAddr:    serverTLSAddr,
			ServerName:       "test.local",
			PinnedCertSHA256: "0000000000000000000000000000000000000000000000000000000000000000",
		})
	}()

	select {
	case err := <-done:
		if !errors.Is(err, ErrPinMismatch) {
			t.Fatalf("expected ErrPinMismatch, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("client kept retrying against an unpinned certificate instead of stopping")
	}
}

// TestServerReapsIdleSessions verifies the session table doesn't grow
// without bound as peers come and go.
func TestServerReapsIdleSessions(t *testing.T) {
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

	table := newSessionTable(wireConn, localAddr, obf)
	defer table.closeAll()

	for i := 0; i < 3; i++ {
		addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40000 + i}
		if _, err := table.get(addr); err != nil {
			t.Fatal(err)
		}
	}
	if got := table.count(); got != 3 {
		t.Fatalf("expected 3 sessions, got %d", got)
	}

	// Re-fetching a known peer must reuse its session rather than
	// opening a second socket for it.
	if _, err := table.get(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40000}); err != nil {
		t.Fatal(err)
	}
	if got := table.count(); got != 3 {
		t.Fatalf("re-fetching a peer created a duplicate session: %d sessions", got)
	}

	table.closeAll()
	// closeAll ends each pump goroutine, which removes its session.
	deadline := time.Now().Add(2 * time.Second)
	for table.count() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("sessions were not cleaned up: %d remain", table.count())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestGracefulShutdown checks that cancelling the context stops the
// proxies promptly and reports cancellation rather than a socket error,
// so systemd sees a clean exit on SIGTERM.
func TestGracefulShutdown(t *testing.T) {
	psk := testPSK(t)

	peerAddr, _, closePeer := realisticWireGuardPeer(t)
	defer closePeer()

	ctx, cancel := context.WithCancel(context.Background())
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- RunServer(ctx, Config{
			PSKs:           [][32]byte{psk},
			LocalAddr:      peerAddr,
			ListenWireAddr: freeUDPAddr(t),
		})
	}()
	time.Sleep(150 * time.Millisecond)

	cancel()
	select {
	case err := <-serverErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled on shutdown, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not shut down when its context was cancelled")
	}
}

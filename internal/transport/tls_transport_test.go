package transport

import (
	"bytes"
	"crypto/rand"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/cryptoinvestccc-del/vpn/internal/tlscert"
)

func freeTCPAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func TestTLSClientServerRoundTrip(t *testing.T) {
	var psk [32]byte
	if _, err := rand.Read(psk[:]); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")
	if err := tlscert.Generate("test.local", certPath, keyPath); err != nil {
		t.Fatal(err)
	}
	pin, err := tlscert.PinFromCertFile(certPath)
	if err != nil {
		t.Fatal(err)
	}

	peerAddr, closePeer := fakeWireGuardPeer(t)
	defer closePeer()

	serverTLSAddr := freeTCPAddr(t)
	clientLocalAddr := freeUDPAddr(t)

	go func() {
		if err := RunServerTLS(TLSConfig{
			PSKs:          [][32]byte{psk},
			LocalAddr:     peerAddr,
			ListenTLSAddr: serverTLSAddr,
			CertFile:      certPath,
			KeyFile:       keyPath,
		}); err != nil {
			t.Logf("tls server exited: %v", err)
		}
	}()
	time.Sleep(150 * time.Millisecond)

	go func() {
		if err := RunClientTLS(TLSConfig{
			PSKs:             [][32]byte{psk},
			LocalAddr:        clientLocalAddr,
			RemoteTLSAddr:    serverTLSAddr,
			ServerName:       "test.local",
			PinnedCertSHA256: pin,
		}); err != nil {
			t.Logf("tls client exited: %v", err)
		}
	}()
	time.Sleep(150 * time.Millisecond)

	appConn, err := net.Dial("udp", clientLocalAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer appConn.Close()

	msg := []byte("simulated wireguard handshake init over tls")
	if _, err := appConn.Write(msg); err != nil {
		t.Fatal(err)
	}

	appConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 2048)
	n, err := appConn.Read(buf)
	if err != nil {
		t.Fatalf("did not receive round-tripped reply: %v", err)
	}

	want := append([]byte("echo:"), msg...)
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("got %q want %q", buf[:n], want)
	}
}

// TestTLSAutoDerivedKey covers the no-PSK path: neither side configures a
// psk, so both must derive the same obfuscation key from the TLS session
// via RFC 5705 keying material export and still round-trip successfully.
func TestTLSAutoDerivedKey(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")
	if err := tlscert.Generate("test.local", certPath, keyPath); err != nil {
		t.Fatal(err)
	}
	pin, err := tlscert.PinFromCertFile(certPath)
	if err != nil {
		t.Fatal(err)
	}

	peerAddr, closePeer := fakeWireGuardPeer(t)
	defer closePeer()

	serverTLSAddr := freeTCPAddr(t)
	clientLocalAddr := freeUDPAddr(t)

	go func() {
		if err := RunServerTLS(TLSConfig{
			// PSKs intentionally left empty.
			LocalAddr:     peerAddr,
			ListenTLSAddr: serverTLSAddr,
			CertFile:      certPath,
			KeyFile:       keyPath,
		}); err != nil {
			t.Logf("tls server exited: %v", err)
		}
	}()
	time.Sleep(150 * time.Millisecond)

	go func() {
		if err := RunClientTLS(TLSConfig{
			// PSKs intentionally left empty.
			LocalAddr:        clientLocalAddr,
			RemoteTLSAddr:    serverTLSAddr,
			ServerName:       "test.local",
			PinnedCertSHA256: pin,
		}); err != nil {
			t.Logf("tls client exited: %v", err)
		}
	}()
	time.Sleep(150 * time.Millisecond)

	appConn, err := net.Dial("udp", clientLocalAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer appConn.Close()

	msg := []byte("packet secured by an auto-derived key")
	if _, err := appConn.Write(msg); err != nil {
		t.Fatal(err)
	}

	appConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 2048)
	n, err := appConn.Read(buf)
	if err != nil {
		t.Fatalf("did not receive round-tripped reply: %v", err)
	}

	want := append([]byte("echo:"), msg...)
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("got %q want %q", buf[:n], want)
	}
}

func TestTLSClientRejectsWrongPin(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")
	if err := tlscert.Generate("test.local", certPath, keyPath); err != nil {
		t.Fatal(err)
	}

	var psk [32]byte
	if _, err := rand.Read(psk[:]); err != nil {
		t.Fatal(err)
	}

	peerAddr, closePeer := fakeWireGuardPeer(t)
	defer closePeer()

	serverTLSAddr := freeTCPAddr(t)
	clientLocalAddr := freeUDPAddr(t)

	go func() {
		_ = RunServerTLS(TLSConfig{
			PSKs:          [][32]byte{psk},
			LocalAddr:     peerAddr,
			ListenTLSAddr: serverTLSAddr,
			CertFile:      certPath,
			KeyFile:       keyPath,
		})
	}()
	time.Sleep(150 * time.Millisecond)

	err := RunClientTLS(TLSConfig{
		PSKs:             [][32]byte{psk},
		LocalAddr:        clientLocalAddr,
		RemoteTLSAddr:    serverTLSAddr,
		ServerName:       "test.local",
		PinnedCertSHA256: "0000000000000000000000000000000000000000000000000000000000000000",
	})
	if err == nil {
		t.Fatal("expected error dialing with wrong pinned cert")
	}
}

package transport

import (
	"bytes"
	"crypto/rand"
	"net"
	"testing"
	"time"
)

// fakeWireGuardPeer is a bare UDP echo responder standing in for a real
// WireGuard endpoint, so we can test the obfuscated proxy pair end-to-end
// without depending on an actual WireGuard installation.
func fakeWireGuardPeer(t *testing.T) (addr string, close func()) {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			reply := append([]byte("echo:"), buf[:n]...)
			_, _ = conn.WriteTo(reply, from)
		}
	}()
	return conn.LocalAddr().String(), func() { conn.Close() }
}

func freeUDPAddr(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := conn.LocalAddr().String()
	conn.Close()
	return addr
}

func TestClientServerRoundTrip(t *testing.T) {
	var psk [32]byte
	if _, err := rand.Read(psk[:]); err != nil {
		t.Fatal(err)
	}

	peerAddr, closePeer := fakeWireGuardPeer(t)
	defer closePeer()

	serverWireAddr := freeUDPAddr(t)
	clientLocalAddr := freeUDPAddr(t)

	serverCfg := Config{
		PSK:            psk,
		LocalAddr:      peerAddr,
		ListenWireAddr: serverWireAddr,
	}
	clientCfg := Config{
		PSK:            psk,
		LocalAddr:      clientLocalAddr,
		RemoteWireAddr: serverWireAddr,
	}

	go func() {
		if err := RunServer(serverCfg); err != nil {
			t.Logf("server exited: %v", err)
		}
	}()
	go func() {
		if err := RunClient(clientCfg); err != nil {
			t.Logf("client exited: %v", err)
		}
	}()

	// Give listeners a moment to bind.
	time.Sleep(100 * time.Millisecond)

	appConn, err := net.Dial("udp", clientLocalAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer appConn.Close()

	msg := []byte("simulated wireguard handshake init")
	if _, err := appConn.Write(msg); err != nil {
		t.Fatal(err)
	}

	appConn.SetReadDeadline(time.Now().Add(3 * time.Second))
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

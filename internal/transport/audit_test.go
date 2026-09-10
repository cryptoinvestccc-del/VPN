package transport

import (
	"crypto/rand"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
)

// realisticWireGuardPeer echoes back a payload of the same size it
// received, the way a real WireGuard endpoint responds with same-order-of-
// magnitude packets, and reports every distinct source it saw so tests can
// tell whether client identities survived the tunnel.
func realisticWireGuardPeer(t *testing.T) (addr string, sources func() int, close func()) {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	seen := map[string]bool{}

	go func() {
		buf := make([]byte, 65535)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			mu.Lock()
			seen[from.String()] = true
			mu.Unlock()

			reply := make([]byte, n)
			copy(reply, buf[:n])
			_, _ = conn.WriteTo(reply, from)
		}
	}()

	return conn.LocalAddr().String(), func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(seen)
	}, func() { conn.Close() }
}

// TestFullSizeWireGuardPacket feeds the tunnel a packet the size a real
// WireGuard data packet reaches at the default MTU of 1420: 16 bytes of
// WireGuard header + 1420 payload + 16 byte Poly1305 tag = 1452 bytes.
// If the tunnel silently drops these, every large transfer through the
// VPN stalls while small packets appear to work.
func TestFullSizeWireGuardPacket(t *testing.T) {
	ctx := testContext(t)
	var psk [32]byte
	if _, err := rand.Read(psk[:]); err != nil {
		t.Fatal(err)
	}

	peerAddr, _, closePeer := realisticWireGuardPeer(t)
	defer closePeer()

	serverWireAddr := freeUDPAddr(t)
	clientLocalAddr := freeUDPAddr(t)

	go func() {
		_ = RunServer(ctx, Config{
			PSKs:           [][32]byte{psk},
			LocalAddr:      peerAddr,
			ListenWireAddr: serverWireAddr,
		})
	}()
	go func() {
		_ = RunClient(ctx, Config{
			PSKs:           [][32]byte{psk},
			LocalAddr:      clientLocalAddr,
			RemoteWireAddr: serverWireAddr,
		})
	}()
	time.Sleep(150 * time.Millisecond)

	appConn, err := net.Dial("udp", clientLocalAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer appConn.Close()

	// Sizes a real WireGuard interface emits at MTU 1420.
	for _, size := range []int{1452, 1420, 1280, 148, 92, 32} {
		packet := make([]byte, size)
		if _, err := rand.Read(packet); err != nil {
			t.Fatal(err)
		}

		if _, err := appConn.Write(packet); err != nil {
			t.Fatal(err)
		}

		appConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 65535)
		n, err := appConn.Read(buf)
		if err != nil {
			t.Fatalf("packet of %d bytes never came back: %v", size, err)
		}
		if n != size {
			t.Fatalf("packet of %d bytes came back as %d bytes", size, n)
		}
	}
}

// TestConcurrentClients runs several independent clients against one
// server, the normal case for any VPN serving more than one device. Each
// client must get its own replies back — not another client's.
func TestConcurrentClients(t *testing.T) {
	ctx := testContext(t)
	var psk [32]byte
	if _, err := rand.Read(psk[:]); err != nil {
		t.Fatal(err)
	}

	peerAddr, _, closePeer := realisticWireGuardPeer(t)
	defer closePeer()

	serverWireAddr := freeUDPAddr(t)
	go func() {
		_ = RunServer(ctx, Config{
			PSKs:           [][32]byte{psk},
			LocalAddr:      peerAddr,
			ListenWireAddr: serverWireAddr,
		})
	}()
	time.Sleep(100 * time.Millisecond)

	const clients = 4
	clientAddrs := make([]string, clients)
	for i := range clientAddrs {
		clientAddrs[i] = freeUDPAddr(t)
		go func(local string) {
			_ = RunClient(ctx, Config{
				PSKs:           [][32]byte{psk},
				LocalAddr:      local,
				RemoteWireAddr: serverWireAddr,
			})
		}(clientAddrs[i])
	}
	time.Sleep(200 * time.Millisecond)

	var wg sync.WaitGroup
	errs := make(chan error, clients)

	for i, addr := range clientAddrs {
		wg.Add(1)
		go func(clientID int, local string) {
			defer wg.Done()

			conn, err := net.Dial("udp", local)
			if err != nil {
				errs <- err
				return
			}
			defer conn.Close()

			// A payload unique to this client, so a reply that
			// belongs to someone else is detectable.
			payload := []byte(fmt.Sprintf("client-%d-payload-marker", clientID))

			for round := 0; round < 5; round++ {
				if _, err := conn.Write(payload); err != nil {
					errs <- fmt.Errorf("client %d write: %w", clientID, err)
					return
				}
				conn.SetReadDeadline(time.Now().Add(3 * time.Second))
				buf := make([]byte, 65535)
				n, err := conn.Read(buf)
				if err != nil {
					errs <- fmt.Errorf("client %d round %d got no reply: %w", clientID, round, err)
					return
				}
				if string(buf[:n]) != string(payload) {
					errs <- fmt.Errorf("client %d got another client's data: %q", clientID, buf[:n])
					return
				}
			}
		}(i, addr)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestSustainedBidirectionalLoad pushes continuous traffic through the
// tunnel to surface data races and dropped packets that a single
// request/response test never reaches.
func TestSustainedBidirectionalLoad(t *testing.T) {
	ctx := testContext(t)
	var psk [32]byte
	if _, err := rand.Read(psk[:]); err != nil {
		t.Fatal(err)
	}

	peerAddr, _, closePeer := realisticWireGuardPeer(t)
	defer closePeer()

	serverWireAddr := freeUDPAddr(t)
	clientLocalAddr := freeUDPAddr(t)

	go func() {
		_ = RunServer(ctx, Config{
			PSKs:           [][32]byte{psk},
			LocalAddr:      peerAddr,
			ListenWireAddr: serverWireAddr,
		})
	}()
	go func() {
		_ = RunClient(ctx, Config{
			PSKs:           [][32]byte{psk},
			LocalAddr:      clientLocalAddr,
			RemoteWireAddr: serverWireAddr,
		})
	}()
	time.Sleep(150 * time.Millisecond)

	conn, err := net.Dial("udp", clientLocalAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	const packets = 200
	received := 0
	for i := 0; i < packets; i++ {
		payload := make([]byte, 1200)
		if _, err := rand.Read(payload); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write(payload); err != nil {
			t.Fatal(err)
		}

		conn.SetReadDeadline(time.Now().Add(time.Second))
		buf := make([]byte, 65535)
		n, err := conn.Read(buf)
		if err != nil {
			continue // count it as a loss, report at the end
		}
		if n == len(payload) {
			received++
		}
	}

	// UDP may legitimately drop a packet, but the tunnel losing a
	// meaningful share of traffic on loopback means something is wrong.
	if received < packets*95/100 {
		t.Fatalf("only %d/%d packets survived the tunnel", received, packets)
	}
}

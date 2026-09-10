// Package wireguard_test runs the obfuscating tunnel against a real
// WireGuard implementation rather than a stand-in.
//
// Every other test in this repository talks to a UDP echo responder
// standing in for WireGuard. That is enough to check packet plumbing, and
// it is what caught the multi-client and MTU defects — but it proves
// nothing about whether real WireGuard survives the tunnel. Real
// WireGuard does things an echo server does not: a Noise IK handshake
// whose messages have fixed sizes, monotonic counters with an anti-replay
// window, and a rekey every couple of minutes.
//
// wireguard-go's netstack backend runs the genuine protocol entirely in
// userspace — no TUN device, no root, no kernel module — so the real
// implementation can be placed at both ends of the tunnel here.
package wireguard_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/curve25519"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"

	"github.com/cryptoinvestccc-del/vpn/internal/transport"
)

// wgKeypair is a Curve25519 keypair in the hex form WireGuard's
// configuration interface expects.
type wgKeypair struct {
	privateHex string
	publicHex  string
}

func generateKeypair(t *testing.T) wgKeypair {
	t.Helper()

	var private [32]byte
	if _, err := rand.Read(private[:]); err != nil {
		t.Fatal(err)
	}
	// Curve25519 clamping, as WireGuard does when generating a key.
	private[0] &= 248
	private[31] &= 127
	private[31] |= 64

	public, err := curve25519.X25519(private[:], curve25519.Basepoint)
	if err != nil {
		t.Fatal(err)
	}
	return wgKeypair{
		privateHex: hex.EncodeToString(private[:]),
		publicHex:  hex.EncodeToString(public),
	}
}

// wgPeer is one end of the WireGuard tunnel: a real protocol
// implementation with its own userspace IP stack.
type wgPeer struct {
	net    *netstack.Net
	device *device.Device
}

// startWireGuard brings up a real WireGuard peer in userspace.
func startWireGuard(t *testing.T, addr netip.Addr, mtu int, config string) *wgPeer {
	t.Helper()

	tunDev, tnet, err := netstack.CreateNetTUN(
		[]netip.Addr{addr},
		[]netip.Addr{netip.MustParseAddr("127.0.0.1")}, // unused; no DNS in these tests
		mtu,
	)
	if err != nil {
		t.Fatal(err)
	}

	dev := device.NewDevice(tunDev, conn.NewDefaultBind(), device.NewLogger(device.LogLevelSilent, ""))
	if err := dev.IpcSet(config); err != nil {
		t.Fatalf("configuring WireGuard: %v", err)
	}
	if err := dev.Up(); err != nil {
		t.Fatalf("bringing WireGuard up: %v", err)
	}
	t.Cleanup(dev.Close)

	return &wgPeer{net: tnet, device: dev}
}

var (
	issuedPortsMu sync.Mutex
	issuedPorts   = map[string]bool{}
)

// reserveAddr returns a loopback address the operating system is offering
// and never returns the same one twice within this process.
//
// The obvious implementation — bind port 0, read the address back, close —
// can hand the same port to two consecutive callers, because the port is
// free again the instant it is read. This test builds a topology out of
// four separate addresses; if two of them collide, WireGuard's packets go
// somewhere unintended and the failure appears much later as a handshake
// that never completes. That was an observed flake, not a hypothetical.
func reserveAddr(t *testing.T, network string) string {
	t.Helper()

	for attempt := 0; attempt < 100; attempt++ {
		var addr string
		switch network {
		case "udp":
			c, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			addr = c.LocalAddr().String()
			c.Close()
		case "tcp":
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			addr = ln.Addr().String()
			ln.Close()
		default:
			t.Fatalf("unsupported network %q", network)
		}

		issuedPortsMu.Lock()
		fresh := !issuedPorts[addr]
		issuedPorts[addr] = true
		issuedPortsMu.Unlock()

		if fresh {
			return addr
		}
	}

	t.Fatalf("could not find an unused %s port after 100 attempts", network)
	return ""
}

func freeUDPPort(t *testing.T) int {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", reserveAddr(t, "udp"))
	if err != nil {
		t.Fatal(err)
	}
	return addr.Port
}

func freeTCPAddr(t *testing.T) string { return reserveAddr(t, "tcp") }

func testPSK(t *testing.T) [32]byte {
	t.Helper()
	var psk [32]byte
	if _, err := rand.Read(psk[:]); err != nil {
		t.Fatal(err)
	}
	return psk
}

// lastHandshake reads WireGuard's own record of when the current session
// was established, so a test can prove a rekey happened rather than
// assuming one did because enough time passed.
func lastHandshake(t *testing.T, peer *wgPeer) string {
	t.Helper()

	state, err := peer.device.IpcGet()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(state, "\n") {
		if strings.HasPrefix(line, "last_handshake_time_sec=") ||
			strings.HasPrefix(line, "last_handshake_time_nsec=") {
			return line
		}
	}
	t.Fatal("WireGuard reported no handshake time")
	return ""
}

const (
	// wireGuardDefaultMTU is what WireGuard uses when nothing is set —
	// correct for running over plain IP, too large inside this tunnel.
	wireGuardDefaultMTU = 1420

	serverTunnelIP = "10.55.0.1"
	clientTunnelIP = "10.55.0.2"
	servicePort    = 8080
)

// buildTunnel wires a real WireGuard peer at each end of the obfuscated
// tunnel and returns the client's view of the tunnel network:
//
//	[WG client] --UDP--> [obfsclient] ==obfuscated==> [obfsserver] --UDP--> [WG server]
//
// startProxies is called with the addresses the two proxies must use, so
// the same topology serves both the UDP and the TLS transport.
// buildTunnel builds the topology at WireGuard's default MTU.
func buildTunnel(t *testing.T, startProxies func(t *testing.T, wgServerPort int, clientEntry string)) *wgPeer {
	t.Helper()
	return buildTunnelMTU(t, wireGuardDefaultMTU, startProxies)
}

// buildTunnelMTU is buildTunnel with the WireGuard interface MTU under the
// test's control, so a test can check what a given MTU puts on the wire.
func buildTunnelMTU(t *testing.T, mtu int, startProxies func(t *testing.T, wgServerPort int, clientEntry string)) *wgPeer {
	t.Helper()

	serverKeys := generateKeypair(t)
	clientKeys := generateKeypair(t)

	wgServerPort := freeUDPPort(t)
	wgClientPort := freeUDPPort(t)
	clientEntryPort := freeUDPPort(t)
	clientEntry := fmt.Sprintf("127.0.0.1:%d", clientEntryPort)

	// The server-side peer learns the client's endpoint from the
	// handshake, exactly as a deployed server does.
	serverPeer := startWireGuard(t, netip.MustParseAddr(serverTunnelIP), mtu, fmt.Sprintf(
		"private_key=%s\nlisten_port=%d\npublic_key=%s\nallowed_ip=%s/32\n",
		serverKeys.privateHex, wgServerPort, clientKeys.publicHex, clientTunnelIP,
	))

	startProxies(t, wgServerPort, clientEntry)

	// The client's endpoint is the local obfsclient, not the server: as
	// far as WireGuard knows it is talking to a peer on localhost.
	clientPeer := startWireGuard(t, netip.MustParseAddr(clientTunnelIP), mtu, fmt.Sprintf(
		"private_key=%s\nlisten_port=%d\npublic_key=%s\nallowed_ip=%s/32\nendpoint=%s\npersistent_keepalive_interval=5\n",
		clientKeys.privateHex, wgClientPort, serverKeys.publicHex, serverTunnelIP, clientEntry,
	))

	serveOverTunnel(t, serverPeer)
	return clientPeer
}

// serveOverTunnel runs a TCP service reachable only through the WireGuard
// tunnel, so a successful read proves the whole path carried real
// encrypted traffic.
func serveOverTunnel(t *testing.T, peer *wgPeer) {
	t.Helper()

	listener, err := peer.net.ListenTCP(&net.TCPAddr{Port: servicePort})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })

	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				// Echo, so the test can push payloads of any size
				// and check they arrive intact.
				_, _ = io.Copy(c, c)
			}(c)
		}
	}()
}

// dialThroughTunnel opens a TCP connection to the in-tunnel service,
// retrying while the WireGuard handshake completes.
func dialThroughTunnel(t *testing.T, tnet *netstack.Net, timeout time.Duration) net.Conn {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		c, err := tnet.DialContextTCP(ctx, &net.TCPAddr{
			IP:   net.ParseIP(serverTunnelIP),
			Port: servicePort,
		})
		cancel()
		if err == nil {
			return c
		}
		if time.Now().After(deadline) {
			t.Fatalf("no connection through the WireGuard tunnel within %s: %v", timeout, err)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// exchange sends a payload over the tunnelled connection and requires it
// back unchanged.
func exchange(t *testing.T, c net.Conn, size int) {
	t.Helper()

	payload := make([]byte, size)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	if err := c.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := c.Write(payload)
		errCh <- err
	}()

	got := make([]byte, size)
	if _, err := io.ReadFull(c, got); err != nil {
		t.Fatalf("reading %d bytes back through the tunnel: %v", size, err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("writing %d bytes through the tunnel: %v", size, err)
	}
	if string(got) != string(payload) {
		t.Fatalf("%d bytes came back altered", size)
	}
}

// TestRealWireGuardOverUDPTransport carries a genuine WireGuard session
// through the obfuscated UDP transport: a real Noise IK handshake, real
// transport packets with counters, real anti-replay state.
func TestRealWireGuardOverUDPTransport(t *testing.T) {
	psk := testPSK(t)

	peer := buildTunnel(t, func(t *testing.T, wgServerPort int, clientEntry string) {
		ctx := testContext(t)
		wireAddr := fmt.Sprintf("127.0.0.1:%d", freeUDPPort(t))

		go func() {
			_ = transport.RunServer(ctx, transport.Config{
				PSKs:           [][32]byte{psk},
				LocalAddr:      fmt.Sprintf("127.0.0.1:%d", wgServerPort),
				ListenWireAddr: wireAddr,
			})
		}()
		go func() {
			_ = transport.RunClient(ctx, transport.Config{
				PSKs:           [][32]byte{psk},
				LocalAddr:      clientEntry,
				RemoteWireAddr: wireAddr,
				JunkPackets:    3,
			})
		}()
		time.Sleep(200 * time.Millisecond)
	})

	c := dialThroughTunnel(t, peer.net, 30*time.Second)
	defer c.Close()

	// Sizes that exercise the padding budget: small packets get padded,
	// large ones fill the MTU and must not be padded into fragmentation.
	for _, size := range []int{1, 100, 1300, 64 << 10} {
		exchange(t, c, size)
	}
}

// TestRealWireGuardOverTLSTransport does the same through the TLS
// transport, where packets are framed on a stream rather than sent as
// datagrams.
func TestRealWireGuardOverTLSTransport(t *testing.T) {
	psk := testPSK(t)
	certPath, keyPath, pin := testCert(t)

	peer := buildTunnel(t, func(t *testing.T, wgServerPort int, clientEntry string) {
		ctx := testContext(t)
		tlsAddr := freeTCPAddr(t)

		go func() {
			_ = transport.RunServerTLS(ctx, transport.TLSConfig{
				PSKs:          [][32]byte{psk},
				LocalAddr:     fmt.Sprintf("127.0.0.1:%d", wgServerPort),
				ListenTLSAddr: tlsAddr,
				CertFile:      certPath,
				KeyFile:       keyPath,
			})
		}()
		time.Sleep(200 * time.Millisecond)

		go func() {
			_ = transport.RunClientTLS(ctx, transport.TLSConfig{
				PSKs:             [][32]byte{psk},
				LocalAddr:        clientEntry,
				RemoteTLSAddr:    tlsAddr,
				ServerName:       "test.local",
				PinnedCertSHA256: pin,
			})
		}()
		time.Sleep(300 * time.Millisecond)
	})

	c := dialThroughTunnel(t, peer.net, 30*time.Second)
	defer c.Close()

	for _, size := range []int{1, 100, 1300, 64 << 10} {
		exchange(t, c, size)
	}
}

// TestRealWireGuardSurvivesRekey covers the thing an echo responder can
// never exercise: WireGuard renegotiates its session periodically, and
// the tunnel must carry the new handshake — and keep the peer's identity
// straight in the session table — while traffic is in flight.
func TestRealWireGuardSurvivesRekey(t *testing.T) {
	if testing.Short() {
		t.Skip("rekey takes longer than -short allows")
	}

	psk := testPSK(t)

	peer := buildTunnel(t, func(t *testing.T, wgServerPort int, clientEntry string) {
		ctx := testContext(t)
		wireAddr := fmt.Sprintf("127.0.0.1:%d", freeUDPPort(t))

		go func() {
			_ = transport.RunServer(ctx, transport.Config{
				PSKs:           [][32]byte{psk},
				LocalAddr:      fmt.Sprintf("127.0.0.1:%d", wgServerPort),
				ListenWireAddr: wireAddr,
			})
		}()
		go func() {
			_ = transport.RunClient(ctx, transport.Config{
				PSKs:           [][32]byte{psk},
				LocalAddr:      clientEntry,
				RemoteWireAddr: wireAddr,
			})
		}()
		time.Sleep(200 * time.Millisecond)
	})

	c := dialThroughTunnel(t, peer.net, 30*time.Second)
	defer c.Close()

	// WireGuard rekeys after 120 seconds of use; keep traffic flowing
	// across that boundary and require every exchange to succeed.
	firstHandshake := lastHandshake(t, peer)

	deadline := time.Now().Add(150 * time.Second)
	exchanges := 0
	for time.Now().Before(deadline) {
		exchange(t, c, 512)
		exchanges++
		time.Sleep(2 * time.Second)
	}

	if exchanges < 30 {
		t.Fatalf("only %d exchanges completed; the tunnel stalled", exchanges)
	}

	// Without this the test would pass on a tunnel that simply never
	// rekeyed, proving nothing about the case it exists to cover.
	if got := lastHandshake(t, peer); got == firstHandshake {
		t.Fatalf("no rekey occurred during the run (handshake still %s); "+
			"the test did not exercise what it claims to", got)
	}
	t.Logf("%d exchanges completed, session rekeyed mid-run", exchanges)
}

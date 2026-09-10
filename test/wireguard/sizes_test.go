package wireguard_test

import (
	"fmt"
	"net"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/cryptoinvestccc-del/vpn/internal/obfuscator"
	"github.com/cryptoinvestccc-del/vpn/internal/transport"
)

// packetTap is a single-peer UDP relay placed between the tunnel and the
// WireGuard server, so a test can observe the plaintext WireGuard packets
// the tunnel actually delivers. One peer is enough here: the point is to
// measure sizes, not to route traffic for several clients.
type packetTap struct {
	addr string

	mu    sync.Mutex
	sizes []int
}

func startPacketTap(t *testing.T, forwardTo int) *packetTap {
	t.Helper()

	front, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { front.Close() })

	target, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", forwardTo))
	if err != nil {
		t.Fatal(err)
	}
	back, err := net.DialUDP("udp", nil, target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { back.Close() })

	tap := &packetTap{addr: front.LocalAddr().String()}

	var peerMu sync.Mutex
	var peer net.Addr

	go func() {
		buf := make([]byte, 65535)
		for {
			n, from, err := front.ReadFrom(buf)
			if err != nil {
				return
			}
			peerMu.Lock()
			peer = from
			peerMu.Unlock()

			tap.record(n)
			if _, err := back.Write(buf[:n]); err != nil {
				return
			}
		}
	}()

	go func() {
		buf := make([]byte, 65535)
		for {
			n, err := back.Read(buf)
			if err != nil {
				return
			}
			tap.record(n)

			peerMu.Lock()
			to := peer
			peerMu.Unlock()
			if to == nil {
				continue
			}
			if _, err := front.WriteTo(buf[:n], to); err != nil {
				return
			}
		}
	}()

	return tap
}

func (tp *packetTap) record(size int) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	tp.sizes = append(tp.sizes, size)
}

func (tp *packetTap) observed() []int {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	out := append([]int(nil), tp.sizes...)
	sort.Ints(out)
	return out
}

// TestObservedWireGuardPacketSizes measures the packets a real WireGuard
// implementation actually emits, rather than trusting the numbers the
// padding budget was designed around.
//
// Two constants depend on these sizes being right. SafeWireSize assumes a
// full-size data packet is 1452 bytes at the default MTU of 1420, and the
// padding logic assumes handshake messages are small enough to have
// headroom for randomization. If either assumption were wrong, padding
// would either fragment real traffic or fail to disguise the handshake —
// and both failures are invisible until deployed against a censor.
func TestObservedWireGuardPacketSizes(t *testing.T) {
	psk := testPSK(t)
	var tap *packetTap

	peer := buildTunnel(t, func(t *testing.T, wgServerPort int, clientEntry string) {
		ctx := testContext(t)
		wireAddr := fmt.Sprintf("127.0.0.1:%d", freeUDPPort(t))
		tap = startPacketTap(t, wgServerPort)

		go func() {
			_ = transport.RunServer(ctx, transport.Config{
				PSKs:           [][32]byte{psk},
				LocalAddr:      tap.addr,
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

	// Enough traffic to produce full-size data packets.
	exchange(t, c, 256<<10)
	time.Sleep(300 * time.Millisecond)

	sizes := tap.observed()
	if len(sizes) == 0 {
		t.Fatal("the tap saw no WireGuard packets at all")
	}

	seen := map[int]bool{}
	for _, s := range sizes {
		seen[s] = true
	}

	// WireGuard's handshake messages have fixed, documented sizes. Seeing
	// them here confirms the tunnel carried a genuine handshake — and
	// that these are the lengths our padding has to disguise.
	const (
		handshakeInitiation = 148
		handshakeResponse   = 92
	)
	if !seen[handshakeInitiation] {
		t.Errorf("never saw a %d-byte handshake initiation; sizes observed: %v",
			handshakeInitiation, sizes)
	}
	if !seen[handshakeResponse] {
		t.Errorf("never saw a %d-byte handshake response; sizes observed: %v",
			handshakeResponse, sizes)
	}

	largest := sizes[len(sizes)-1]
	t.Logf("observed %d packets, sizes %d..%d", len(sizes), sizes[0], largest)

	// The assumption SafeWireSize is built on: a full-size data packet at
	// the default MTU fits in the budget with room for the wrapper.
	if largest > obfuscator.SafeWireSize {
		t.Errorf("real WireGuard emitted a %d-byte packet, above SafeWireSize (%d): "+
			"wrapping it would exceed the path MTU", largest, obfuscator.SafeWireSize)
	}
	if largest+obfuscator.Overhead > obfuscator.SafeWireSize {
		t.Logf("note: the largest packet (%d) plus wrapper overhead (%d) reaches %d, "+
			"so packets this size are sent unpadded by design",
			largest, obfuscator.Overhead, largest+obfuscator.Overhead)
	}

	// And the property padding exists to provide: the handshake sizes
	// must not survive wrapping unchanged.
	obf, err := obfuscator.New(psk)
	if err != nil {
		t.Fatal(err)
	}
	wrappedSizes := map[int]bool{}
	for i := 0; i < 100; i++ {
		wrapped, err := obf.Wrap(make([]byte, handshakeInitiation))
		if err != nil {
			t.Fatal(err)
		}
		wrappedSizes[len(wrapped)] = true
	}
	if len(wrappedSizes) < 20 {
		t.Errorf("a %d-byte handshake wrapped into only %d distinct sizes; "+
			"its fixed length would still identify WireGuard",
			handshakeInitiation, len(wrappedSizes))
	}
}

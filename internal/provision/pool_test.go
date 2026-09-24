package provision

import (
	"net/netip"
	"testing"
	"time"
)

func peerAt(key, addr string) Peer {
	return Peer{PublicKey: key, Addresses: []netip.Prefix{netip.MustParsePrefix(addr)}}
}

func mustPool(t *testing.T, subnet, server string) *Pool {
	t.Helper()
	p, err := NewPool(netip.MustParsePrefix(subnet), netip.MustParseAddr(server))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAllocateSkipsWhatIsInUse(t *testing.T) {
	pool := mustPool(t, "10.8.0.0/24", "10.8.0.1")
	peers := []Peer{
		peerAt("a", "10.8.0.2/32"),
		peerAt("b", "10.8.0.3/32"),
	}

	addr, err := pool.Allocate(peers)
	if err != nil {
		t.Fatal(err)
	}
	if addr.String() != "10.8.0.4" {
		t.Errorf("allocated %s, expected the lowest free address 10.8.0.4", addr)
	}
}

// TestAllocateNeverRepeats is the property that matters most here. The
// server routes a tunnel address to exactly one peer, so handing the
// same address to two clients does not degrade a tunnel — it delivers
// one person's traffic to another person's device.
func TestAllocateNeverRepeats(t *testing.T) {
	pool := mustPool(t, "10.8.0.0/24", "10.8.0.1")

	var peers []Peer
	seen := map[netip.Addr]bool{}

	for i := 0; i < 250; i++ {
		addr, err := pool.Allocate(peers)
		if err != nil {
			t.Fatalf("allocation %d failed early: %v", i, err)
		}
		if seen[addr] {
			t.Fatalf("address %s was handed out twice", addr)
		}
		seen[addr] = true
		peers = append(peers, peerAt("peer", addr.String()+"/32"))
	}
}

func TestAllocateReusesAFreedAddress(t *testing.T) {
	// The whole reason for lowest-free: a free service churns through
	// installs, and without reuse a /24 is exhausted in an afternoon.
	pool := mustPool(t, "10.8.0.0/24", "10.8.0.1")
	peers := []Peer{
		peerAt("a", "10.8.0.2/32"),
		peerAt("c", "10.8.0.4/32"),
	}

	addr, err := pool.Allocate(peers)
	if err != nil {
		t.Fatal(err)
	}
	if addr.String() != "10.8.0.3" {
		t.Errorf("allocated %s, expected the gap at 10.8.0.3 to be reused", addr)
	}
}

func TestAllocateRefusesReservedAddresses(t *testing.T) {
	pool := mustPool(t, "10.8.0.0/24", "10.8.0.1")

	addr, err := pool.Allocate(nil)
	if err != nil {
		t.Fatal(err)
	}
	switch addr.String() {
	case "10.8.0.0":
		t.Error("handed out the network address")
	case "10.8.0.1":
		t.Error("handed out the server's own address")
	case "10.8.0.255":
		t.Error("handed out the broadcast address")
	}
	if addr.String() != "10.8.0.2" {
		t.Errorf("allocated %s, expected 10.8.0.2", addr)
	}
}

func TestPoolFullIsReportedNotWrappedAround(t *testing.T) {
	// A pool that quietly starts again from the bottom when it fills is
	// exactly the duplicate-address fault, arriving later and harder to
	// spot. Running out has to be an error.
	pool := mustPool(t, "10.8.0.0/29", "10.8.0.1")

	var peers []Peer
	for {
		addr, err := pool.Allocate(peers)
		if err == ErrPoolFull {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range peers {
			if p.Addresses[0].Addr() == addr {
				t.Fatalf("pool wrapped around and reissued %s", addr)
			}
		}
		peers = append(peers, peerAt("p", addr.String()+"/32"))
	}

	// /29 is eight addresses: network, server, broadcast, five clients.
	if len(peers) != 5 {
		t.Errorf("a /29 yielded %d client addresses, expected 5", len(peers))
	}
}

func TestPoolRejectsASubnetWithNoRoom(t *testing.T) {
	for _, subnet := range []string{"10.8.0.0/31", "10.8.0.0/32"} {
		if _, err := NewPool(netip.MustParsePrefix(subnet), netip.MustParseAddr("10.8.0.1")); err == nil {
			t.Errorf("%s was accepted as a client pool", subnet)
		}
	}
}

func TestAllocateIPv6(t *testing.T) {
	pool, err := NewPool(netip.MustParsePrefix("fd00:8::/120"), netip.MustParseAddr("fd00:8::1"))
	if err != nil {
		t.Fatal(err)
	}
	addr, err := pool.Allocate([]Peer{peerAt("a", "fd00:8::2/128")})
	if err != nil {
		t.Fatal(err)
	}
	if addr.String() != "fd00:8::3" {
		t.Errorf("allocated %s, expected fd00:8::3", addr)
	}
}

func TestPeerUsed(t *testing.T) {
	if (Peer{}).Used() {
		t.Error("a peer that never handshook is reported as used")
	}
	if !(Peer{LastHandshake: time.Now()}).Used() {
		t.Error("a peer with a handshake is reported as unused")
	}
}

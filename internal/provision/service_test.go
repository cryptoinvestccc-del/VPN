package provision

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"testing"
)

// fakeDevice stands in for the running interface. It is deliberately
// strict: adding a peer that already exists, or two peers on one
// address, is a fault in the caller and shows up here rather than on a
// server nobody can see.
type fakeDevice struct {
	mu    sync.Mutex
	peers []Peer

	// slow, when set, runs between reading the peer list and returning
	// it, so a test can interleave two callers at the worst moment.
	slow func()

	readErr, addErr error
}

func (d *fakeDevice) Peers(context.Context) ([]Peer, error) {
	if d.readErr != nil {
		return nil, d.readErr
	}
	d.mu.Lock()
	out := append([]Peer(nil), d.peers...)
	d.mu.Unlock()

	if d.slow != nil {
		d.slow()
	}
	return out, nil
}

func (d *fakeDevice) AddPeer(_ context.Context, key string, addr netip.Prefix) error {
	if d.addErr != nil {
		return d.addErr
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, p := range d.peers {
		if p.PublicKey == key {
			return fmt.Errorf("peer %s already exists", key)
		}
		for _, a := range p.Addresses {
			if a.Addr() == addr.Addr() {
				return fmt.Errorf("address %s is already routed to %s", addr, p.PublicKey)
			}
		}
	}
	d.peers = append(d.peers, Peer{PublicKey: key, Addresses: []netip.Prefix{addr}})
	return nil
}

func (d *fakeDevice) RemovePeer(_ context.Context, key string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, p := range d.peers {
		if p.PublicKey == key {
			d.peers = append(d.peers[:i], d.peers[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("no peer %s", key)
}

func (d *fakeDevice) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.peers)
}

func testKey(n int) string {
	raw := make([]byte, 32)
	raw[0] = byte(n)
	raw[1] = byte(n >> 8)
	raw[2] = 0xAA
	return base64.StdEncoding.EncodeToString(raw)
}

func testService(t *testing.T, device Device) *Service {
	t.Helper()
	s, err := NewService(device, mustPool(t, "10.8.0.0/24", "10.8.0.1"), Settings{
		Endpoint:        "198.51.100.9:51820",
		ServerPublicKey: testKey(999),
		AllowedIPs:      "0.0.0.0/0, ::/0",
		MTU:             1280,
		Keepalive:       25,
		Params:          Params{Jc: 4, Jmin: 40, Jmax: 70, S1: 86, S2: 574, H1: "1", H2: "2", H3: "3", H4: "4"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestIssueGivesEachDeviceItsOwnAddress(t *testing.T) {
	device := &fakeDevice{}
	svc := testService(t, device)

	seen := map[netip.Addr]bool{}
	for i := 0; i < 20; i++ {
		cfg, err := svc.Issue(context.Background(), testKey(i))
		if err != nil {
			t.Fatal(err)
		}
		if seen[cfg.Address.Addr()] {
			t.Fatalf("address %s was issued twice", cfg.Address)
		}
		seen[cfg.Address.Addr()] = true
	}
	if device.count() != 20 {
		t.Errorf("the interface has %d peers after 20 issues", device.count())
	}
}

// TestIssueIsIdempotent: a reply lost on the way back to a phone is an
// ordinary event. Without this, every retry burns another address and
// leaves behind a peer nobody uses.
func TestIssueIsIdempotent(t *testing.T) {
	device := &fakeDevice{}
	svc := testService(t, device)
	key := testKey(1)

	first, err := svc.Issue(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		again, err := svc.Issue(context.Background(), key)
		if err != nil {
			t.Fatalf("retry %d: %v", i, err)
		}
		if again.Address != first.Address {
			t.Fatalf("retry %d moved the client from %s to %s", i, first.Address, again.Address)
		}
	}
	if device.count() != 1 {
		t.Errorf("retries created %d peers", device.count())
	}
}

// TestConcurrentIssuesNeverShareAnAddress is the fault this design
// exists to prevent. Two requests that read the peer list before either
// writes would pick the same free address, and the server then routes
// that address to whichever peer was added last — one person's traffic
// arriving on another person's device.
//
// The fake blocks inside Peers to force exactly that interleaving.
func TestConcurrentIssuesNeverShareAnAddress(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	device := &fakeDevice{slow: func() {
		// Hold the first reader open long enough for the others to
		// arrive, if the service were to let them.
		once.Do(func() { close(release) })
		<-release
	}}
	svc := testService(t, device)

	const n = 16
	var wg sync.WaitGroup
	addrs := make([]netip.Prefix, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cfg, err := svc.Issue(context.Background(), testKey(i))
			addrs[i], errs[i] = cfg.Address, err
		}(i)
	}
	wg.Wait()

	seen := map[netip.Addr]int{}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("issue %d failed: %v", i, err)
		}
		if prev, ok := seen[addrs[i].Addr()]; ok {
			t.Fatalf("clients %d and %d were both given %s", prev, i, addrs[i])
		}
		seen[addrs[i].Addr()] = i
	}
}

func TestIssueRejectsBadKeys(t *testing.T) {
	svc := testService(t, &fakeDevice{})

	cases := map[string]string{
		"empty":        "",
		"not base64":   "!!!!not base64!!!!",
		"wrong length": base64.StdEncoding.EncodeToString(make([]byte, 16)),
		"all zeroes":   base64.StdEncoding.EncodeToString(make([]byte, 32)),
	}
	for name, key := range cases {
		if _, err := svc.Issue(context.Background(), key); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("%s: got %v, want ErrInvalidKey", name, err)
		}
	}
}

// TestPeerCeilingIsEnforced: a handshake is matched against the peer
// list, so the cost of an unauthenticated packet grows with the number
// of peers. A free app that issues per install and never takes one back
// walks into finding 22 without anybody attacking anything.
func TestPeerCeilingIsEnforced(t *testing.T) {
	device := &fakeDevice{}
	svc := testService(t, device)
	svc.SetMaxPeers(5)

	for i := 0; i < 5; i++ {
		if _, err := svc.Issue(context.Background(), testKey(i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Issue(context.Background(), testKey(99)); !errors.Is(err, ErrTooManyPeers) {
		t.Fatalf("the sixth issue returned %v, want ErrTooManyPeers", err)
	}

	// An existing client must still be served once the ceiling is hit:
	// turning away the people already using the service, to protect the
	// service they are using, is the wrong way round.
	if _, err := svc.Issue(context.Background(), testKey(0)); err != nil {
		t.Errorf("an existing client was refused at the ceiling: %v", err)
	}
}

func TestIssueReportsDeviceFailures(t *testing.T) {
	boom := errors.New("awg is not running")

	if _, err := testService(t, &fakeDevice{readErr: boom}).Issue(context.Background(), testKey(1)); err == nil {
		t.Error("a failure reading the interface was swallowed")
	}
	if _, err := testService(t, &fakeDevice{addErr: boom}).Issue(context.Background(), testKey(1)); err == nil {
		t.Error("a failure adding the peer was swallowed")
	}
}

func TestPoolExhaustionIsReported(t *testing.T) {
	device := &fakeDevice{}
	pool, err := NewPool(netip.MustParsePrefix("10.8.0.0/29"), netip.MustParseAddr("10.8.0.1"))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(device, pool, Settings{Endpoint: "a:1", ServerPublicKey: testKey(9)})
	if err != nil {
		t.Fatal(err)
	}

	var lastErr error
	for i := 0; i < 20; i++ {
		if _, lastErr = svc.Issue(context.Background(), testKey(i)); lastErr != nil {
			break
		}
	}
	if !errors.Is(lastErr, ErrPoolFull) {
		t.Fatalf("running out of addresses reported %v, want ErrPoolFull", lastErr)
	}
}

func TestNewServiceRequiresWhatClientsNeed(t *testing.T) {
	pool := mustPool(t, "10.8.0.0/24", "10.8.0.1")
	cases := map[string]Settings{
		"no endpoint":          {ServerPublicKey: testKey(1)},
		"no server public key": {Endpoint: "a:1"},
	}
	for name, settings := range cases {
		if _, err := NewService(&fakeDevice{}, pool, settings); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := NewService(nil, pool, Settings{Endpoint: "a:1", ServerPublicKey: testKey(1)}); err == nil {
		t.Error("a nil device was accepted")
	}
}

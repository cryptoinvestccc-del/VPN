package provision

import (
	"context"
	"net/netip"
	"testing"
	"time"
)

func usedPeer(key string, ago time.Duration, now time.Time) Peer {
	return Peer{
		PublicKey:     key,
		Addresses:     []netip.Prefix{netip.MustParsePrefix("10.8.0.2/32")},
		LastHandshake: now.Add(-ago),
	}
}

func unusedPeer(key string) Peer {
	return Peer{PublicKey: key, Addresses: []netip.Prefix{netip.MustParsePrefix("10.8.0.3/32")}}
}

// ownsAll is a registry that claims every peer, for the tests below
// that are about timing rather than ownership.
type ownsAll struct{ removed []string }

func (o *ownsAll) Owns(string) bool                  { return true }
func (o *ownsAll) Add(context.Context, string) error { return nil }
func (o *ownsAll) Remove(_ context.Context, k string) error {
	o.removed = append(o.removed, k)
	return nil
}

// memRegistry owns exactly the keys it was given.
type memRegistry map[string]bool

func (m memRegistry) Owns(k string) bool                       { return m[k] }
func (m memRegistry) Add(_ context.Context, k string) error    { m[k] = true; return nil }
func (m memRegistry) Remove(_ context.Context, k string) error { delete(m, k); return nil }

func frozenReaper(device Device, ttl, grace time.Duration) (*Reaper, *time.Time) {
	now := time.Now()
	r := NewReaper(device, &ownsAll{}, ttl, grace)
	r.nowFn = func() time.Time { return now }
	return r, &now
}

func TestExpiredTakesStaleCredentials(t *testing.T) {
	r, now := frozenReaper(&fakeDevice{}, 30*24*time.Hour, time.Hour)

	peers := []Peer{
		usedPeer("fresh", time.Hour, *now),
		usedPeer("stale", 40*24*time.Hour, *now),
	}

	expired := r.Expired(peers)
	if len(expired) != 1 || expired[0].PublicKey != "stale" {
		t.Fatalf("expired: %+v, expected only the stale one", expired)
	}
}

// TestUnusedCredentialsNeedTwoSightings: the interface records only the
// last handshake, so a credential that has never had one carries no date
// at all. The reaper dates it by observing, which means it cannot be
// collected the first time it is seen — and must not be, or a phone
// would lose the credential it was issued seconds ago.
func TestUnusedCredentialsNeedTwoSightings(t *testing.T) {
	r, now := frozenReaper(&fakeDevice{}, time.Hour, time.Hour)
	peers := []Peer{unusedPeer("brand-new")}

	if got := r.Expired(peers); len(got) != 0 {
		t.Fatalf("a credential was collected the first time it was seen: %+v", got)
	}

	// Still inside the grace period.
	*now = now.Add(30 * time.Minute)
	if got := r.Expired(peers); len(got) != 0 {
		t.Fatalf("collected inside the grace period: %+v", got)
	}

	*now = now.Add(31 * time.Minute)
	if got := r.Expired(peers); len(got) != 1 {
		t.Fatalf("an unused credential outlived its grace period: %+v", got)
	}
}

// TestUsingACredentialClearsItsUnusedDate: a phone that provisions, sits
// on a dead connection for most of the grace period and then connects
// must not be collected on the next sweep.
func TestUsingACredentialClearsItsUnusedDate(t *testing.T) {
	r, now := frozenReaper(&fakeDevice{}, 30*24*time.Hour, time.Hour)

	r.Expired([]Peer{unusedPeer("late")}) // first sighting, unused
	*now = now.Add(59 * time.Minute)

	if got := r.Expired([]Peer{usedPeer("late", time.Minute, *now)}); len(got) != 0 {
		t.Fatalf("a credential that started being used was still collected: %+v", got)
	}

	// And from here it lives by the handshake clock, not the old one.
	*now = now.Add(2 * time.Hour)
	if got := r.Expired([]Peer{usedPeer("late", time.Hour, *now)}); len(got) != 0 {
		t.Fatalf("a recently used credential was collected: %+v", got)
	}
}

// TestFirstSeenDoesNotGrowForever: the reaper's own bookkeeping must not
// become the leak it exists to prevent.
func TestFirstSeenDoesNotGrowForever(t *testing.T) {
	r, _ := frozenReaper(&fakeDevice{}, time.Hour, time.Hour)

	for i := 0; i < 500; i++ {
		r.Expired([]Peer{unusedPeer(string(rune('a'+i%26)) + string(rune(i)))})
	}

	r.mu.Lock()
	tracked := len(r.firstSeen)
	r.mu.Unlock()

	if tracked > 1 {
		t.Errorf("the reaper is tracking %d peers the interface no longer has", tracked)
	}
}

func TestSweepWithdrawsAndReportsCount(t *testing.T) {
	now := time.Now()
	device := &fakeDevice{peers: []Peer{
		usedPeer("fresh", time.Minute, now),
		usedPeer("stale-1", 40*24*time.Hour, now),
		usedPeer("stale-2", 90*24*time.Hour, now),
	}}
	r := NewReaper(device, &ownsAll{}, 30*24*time.Hour, time.Hour)

	removed, err := r.Sweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Errorf("withdrew %d credentials, expected 2", removed)
	}
	if device.count() != 1 {
		t.Errorf("the interface has %d peers left, expected 1", device.count())
	}
}

// TestSweepFreesTheAddressForReuse is what ties the reaper to the pool:
// collecting a credential is only useful if the address comes back.
func TestSweepFreesTheAddressForReuse(t *testing.T) {
	now := time.Now()
	device := &fakeDevice{}
	svc := testService(t, device)

	first, err := svc.Issue(context.Background(), testKey(1))
	if err != nil {
		t.Fatal(err)
	}

	// Age it past the TTL and sweep.
	device.mu.Lock()
	device.peers[0].LastHandshake = now.Add(-90 * 24 * time.Hour)
	device.mu.Unlock()

	if _, err := NewReaper(device, &ownsAll{}, 30*24*time.Hour, time.Hour).Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}

	second, err := svc.Issue(context.Background(), testKey(2))
	if err != nil {
		t.Fatal(err)
	}
	if second.Address != first.Address {
		t.Errorf("the freed address %s was not reused; got %s", first.Address, second.Address)
	}
}

func TestSweepSurvivesAStubbornPeer(t *testing.T) {
	now := time.Now()
	device := &stubbornDevice{fakeDevice: fakeDevice{peers: []Peer{
		usedPeer("wont-go", 90*24*time.Hour, now),
		usedPeer("will-go", 90*24*time.Hour, now),
	}}, refuse: "wont-go"}

	removed, err := NewReaper(device, &ownsAll{}, 30*24*time.Hour, time.Hour).Sweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("one peer refusing to go stopped the sweep: removed %d", removed)
	}
}

type stubbornDevice struct {
	fakeDevice
	refuse string
}

func (d *stubbornDevice) RemovePeer(ctx context.Context, key string) error {
	if key == d.refuse {
		return context.DeadlineExceeded
	}
	return d.fakeDevice.RemovePeer(ctx, key)
}

// TestNothingIsWithdrawnThatThisServiceDidNotIssue is the production
// server as it stands: Amnezia's own clients share the interface and the
// subnet, several of them handed out and not yet used. The first version
// would have withdrawn those a day after starting.
func TestNothingIsWithdrawnThatThisServiceDidNotIssue(t *testing.T) {
	now := time.Now()
	peers := []Peer{
		unusedPeer("AMNEZIA-NEVER-CONNECTED"),
		usedPeer("AMNEZIA-LONG-IDLE", 400*24*time.Hour, now),
		unusedPeer("BESY-UNUSED"),
		usedPeer("BESY-IDLE", 40*24*time.Hour, now),
	}
	registry := memRegistry{"BESY-UNUSED": true, "BESY-IDLE": true}
	r := NewReaper(&fakeDevice{}, registry, 30*24*time.Hour, time.Hour)
	clock := now
	r.nowFn = func() time.Time { return clock }

	r.Expired(peers) // first sighting of the unused ones
	clock = clock.Add(48 * time.Hour)
	got := map[string]bool{}
	for _, p := range r.Expired(peers) {
		got[p.PublicKey] = true
	}

	for _, theirs := range []string{"AMNEZIA-NEVER-CONNECTED", "AMNEZIA-LONG-IDLE"} {
		if got[theirs] {
			t.Errorf("%s is Amnezia's, and would have been withdrawn", theirs)
		}
	}
	for _, ours := range []string{"BESY-UNUSED", "BESY-IDLE"} {
		if !got[ours] {
			t.Errorf("%s is ours and past its time, and was kept", ours)
		}
	}
}

func TestWithoutARegistryNothingIsWithdrawn(t *testing.T) {
	r := NewReaper(&fakeDevice{}, nil, time.Hour, time.Hour)
	now := time.Now()
	r.nowFn = func() time.Time { return now }
	peers := []Peer{unusedPeer("A"), usedPeer("B", 1000*time.Hour, now)}
	r.Expired(peers)
	now = now.Add(100 * time.Hour)
	if got := r.Expired(peers); len(got) != 0 {
		t.Errorf("a reaper with no record of what it issued withdrew %d peers", len(got))
	}
}

package provision

import (
	"context"
	"log"
	"sync"
	"time"
)

// A free app that issues a credential per install and never takes one
// back accumulates peers forever. That is not a storage problem: a
// handshake is matched against the peer list, so the work an
// unauthenticated packet costs the server grows with the number of
// peers. It is finding 22 in docs/AUDIT.md arriving by a different road
// — there an attacker had to flood the port, here ordinary users
// installing and forgetting the app get to the same place on their own.
//
// So credentials expire. Not to punish anybody: a device that has not
// connected in weeks is not using the one it has, and will be issued
// another the moment it asks.
const (
	// DefaultTTL is how long a peer may go without a handshake before
	// it is withdrawn. Long enough to cover a holiday.
	DefaultTTL = 30 * 24 * time.Hour

	// DefaultGrace is how long a credential that has never been used at
	// all is kept. An app that installs, provisions and connects does so
	// within seconds; an hour is generous for a phone that provisioned
	// on a dying connection and retried later.
	DefaultGrace = 24 * time.Hour

	// DefaultSweep is how often the list is examined.
	DefaultSweep = time.Hour
)

// Reaper withdraws credentials that have fallen out of use.
type Reaper struct {
	device Device
	ttl    time.Duration
	grace  time.Duration

	// firstSeen dates the peers that have never completed a handshake.
	//
	// The interface cannot help here: it records the last handshake and
	// nothing else, so a peer that has never had one carries no date at
	// all. Rather than keep a database — which would have to survive
	// crashes, and would be one more thing able to disagree with the
	// interface — the dates are held in memory and rebuilt by observing.
	//
	// The cost of that choice, stated plainly: a restart forgets them,
	// so an unused credential can live one extra grace period. That is
	// the whole consequence, and it is self-correcting.
	mu        sync.Mutex
	firstSeen map[string]time.Time
	registry  Registry
	tally     *Tally

	// removing serialises the hourly sweep and an operator's cleanup, so
	// the two never try to withdraw the same peer at once.
	removing sync.Mutex

	nowFn func() time.Time
}

// NewReaper prepares a reaper. Zero durations take the defaults.
// NewReaper sweeps only the peers registry says this service issued.
//
// The registry is not optional. The first version swept every peer on
// the interface, and the interface is shared: Amnezia's own clients sit
// on it too, in the same subnet, with nothing on the wire to tell them
// apart from ours. A client Amnezia had handed to somebody who had not
// yet connected would have been withdrawn after a day. Without a record
// of what this service created, it touches nothing.
func NewReaper(device Device, registry Registry, ttl, grace time.Duration) *Reaper {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if grace <= 0 {
		grace = DefaultGrace
	}
	return &Reaper{
		device:    device,
		registry:  registry,
		ttl:       ttl,
		grace:     grace,
		firstSeen: map[string]time.Time{},
		nowFn:     time.Now,
	}
}

// Run sweeps until the context ends.
func (r *Reaper) Run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = DefaultSweep
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		// Sweep on entry as well as on the tick, so a server that is
		// restarted often still collects.
		if removed, err := r.Sweep(ctx); err != nil {
			log.Printf("provision: sweep failed: %v", err)
		} else if removed > 0 {
			log.Printf("provision: withdrew %d unused credentials", removed)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Sweep withdraws expired credentials once and reports how many.
func (r *Reaper) Sweep(ctx context.Context) (int, error) {
	r.removing.Lock()
	defer r.removing.Unlock()

	peers, err := r.device.Peers(ctx)
	if err != nil {
		return 0, err
	}
	return r.withdraw(ctx, r.Expired(peers)), nil
}

// Idle lists this service's credentials that have been used but not in
// the last idle: the ones Cleanup would withdraw. Credentials never used
// at all are left to the sweep — one may belong to a phone that is
// connecting for the first time this very second.
func (r *Reaper) Idle(ctx context.Context, idle time.Duration) ([]Peer, error) {
	peers, err := r.device.Peers(ctx)
	if err != nil {
		return nil, err
	}
	if r.registry == nil {
		return nil, nil
	}
	now := r.nowFn()
	var out []Peer
	for _, p := range peers {
		if r.registry.Owns(p.PublicKey) && p.Used() && now.Sub(p.LastHandshake) >= idle {
			out = append(out, p)
		}
	}
	return out, nil
}

// Cleanup withdraws, now, every credential of ours not used in the last
// idle, and reports how many went. It is the operator's early sweep:
// nothing is lost by it, because a device whose credential was withdrawn
// is issued one again, transparently, the next time it connects.
func (r *Reaper) Cleanup(ctx context.Context, idle time.Duration) (int, error) {
	r.removing.Lock()
	defer r.removing.Unlock()

	peers, err := r.Idle(ctx, idle)
	if err != nil {
		return 0, err
	}
	return r.withdraw(ctx, peers), nil
}

func (r *Reaper) withdraw(ctx context.Context, peers []Peer) int {
	var removed int
	for _, peer := range peers {
		if err := r.device.RemovePeer(ctx, peer.PublicKey); err != nil {
			// One stubborn peer must not stop the rest being collected.
			log.Printf("provision: could not withdraw a credential: %v", err)
			continue
		}
		removed++
		r.mu.Lock()
		r.tally.addWithdrawn()
		r.mu.Unlock()
		if err := r.registry.Remove(ctx, peer.PublicKey); err != nil {
			// The peer is gone either way; a stale entry only means
			// a key that no longer exists is remembered.
			log.Printf("provision: could not forget a withdrawn credential: %v", err)
		}

		r.mu.Lock()
		delete(r.firstSeen, peer.PublicKey)
		r.mu.Unlock()
	}
	return removed
}

// Expired selects the peers that should be withdrawn, and records the
// first sighting of those that have never been used.
//
// Separated from Sweep so the decision can be tested without a device
// and without waiting a month.
func (r *Reaper) Expired(peers []Peer) []Peer {
	now := r.nowFn()

	r.mu.Lock()
	defer r.mu.Unlock()

	// Forget peers that are gone, so the map cannot grow without bound
	// on a server that churns.
	// Only what this service issued is ever considered. Everything else
	// on the interface belongs to somebody else.
	if r.registry == nil {
		return nil
	}
	owned := peers[:0:0]
	for _, peer := range peers {
		if r.registry.Owns(peer.PublicKey) {
			owned = append(owned, peer)
		}
	}
	peers = owned

	present := make(map[string]bool, len(peers))
	for _, peer := range peers {
		present[peer.PublicKey] = true
	}
	for key := range r.firstSeen {
		if !present[key] {
			delete(r.firstSeen, key)
		}
	}

	var expired []Peer
	for _, peer := range peers {
		if peer.Used() {
			delete(r.firstSeen, peer.PublicKey)
			if now.Sub(peer.LastHandshake) > r.ttl {
				expired = append(expired, peer)
			}
			continue
		}

		seen, ok := r.firstSeen[peer.PublicKey]
		if !ok {
			r.firstSeen[peer.PublicKey] = now
			continue
		}
		if now.Sub(seen) > r.grace {
			expired = append(expired, peer)
		}
	}
	return expired
}

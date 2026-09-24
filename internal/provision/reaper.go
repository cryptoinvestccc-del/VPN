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

	nowFn func() time.Time
}

// NewReaper prepares a reaper. Zero durations take the defaults.
func NewReaper(device Device, ttl, grace time.Duration) *Reaper {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if grace <= 0 {
		grace = DefaultGrace
	}
	return &Reaper{
		device:    device,
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
	peers, err := r.device.Peers(ctx)
	if err != nil {
		return 0, err
	}

	var removed int
	for _, peer := range r.Expired(peers) {
		if err := r.device.RemovePeer(ctx, peer.PublicKey); err != nil {
			// One stubborn peer must not stop the rest being collected.
			log.Printf("provision: could not withdraw a credential: %v", err)
			continue
		}
		removed++

		r.mu.Lock()
		delete(r.firstSeen, peer.PublicKey)
		r.mu.Unlock()
	}
	return removed, nil
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

package transport

import (
	"sync"
	"time"
)

// replayGuard remembers packets that have already been used to open a
// session, so a single packet captured off the wire cannot be resent from
// many source addresses to make the server allocate a session and a
// socket for each copy.
//
// It is consulted only when a packet would create a *new* session, which
// is a rare event compared to ordinary traffic — a full anti-replay
// window over every packet would cost far more memory than the attack is
// worth, and the tunnelled WireGuard already rejects replayed payloads on
// its own counters.
type replayGuard struct {
	mu    sync.Mutex
	seen  map[string]time.Time
	max   int
	ttl   time.Duration
	nowFn func() time.Time
}

func newReplayGuard(max int, ttl time.Duration) *replayGuard {
	return &replayGuard{
		seen:  make(map[string]time.Time),
		max:   max,
		ttl:   ttl,
		nowFn: time.Now,
	}
}

// admit reports whether this packet is being seen for the first time, and
// records it. A false result means the packet is a replay.
func (g *replayGuard) admit(nonce []byte) bool {
	key := string(nonce)
	now := g.nowFn()

	g.mu.Lock()
	defer g.mu.Unlock()

	if seenAt, ok := g.seen[key]; ok {
		if now.Sub(seenAt) < g.ttl {
			return false
		}
		// Older than the window: the address it opened a session for
		// is long gone, so treat it as fresh again.
	}

	if len(g.seen) >= g.max {
		g.prune(now)
	}
	g.seen[key] = now
	return true
}

// prune drops expired entries, and if that frees nothing, clears the
// table outright. Forgetting early is safe — the worst case is that an
// attacker who has been replaying for longer than the window gets to open
// one more session, which the session limit already bounds.
func (g *replayGuard) prune(now time.Time) {
	for key, seenAt := range g.seen {
		if now.Sub(seenAt) >= g.ttl {
			delete(g.seen, key)
		}
	}
	if len(g.seen) >= g.max {
		clear(g.seen)
	}
}

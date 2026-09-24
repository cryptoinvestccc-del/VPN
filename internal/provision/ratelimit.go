package provision

import (
	"sync"
	"time"
)

// The issuing endpoint is reachable by anyone who downloads the app —
// that is the point of it — so it is also reachable by anyone who wants
// to fill the peer list. Each accepted request costs an address out of a
// finite pool and adds a peer that every later handshake is matched
// against, so a caller left unlimited can exhaust the service for
// everybody in a couple of minutes.
//
// The limit is per source address and deliberately loose. A phone
// provisions once, and then only again after a reinstall; anything
// asking repeatedly is not a phone.
const (
	// DefaultIssueRate is sustained requests per second from one source.
	DefaultIssueRate = 0.05 // three a minute

	// DefaultIssueBurst lets a household behind one address, or a lab
	// full of test devices, provision together without being turned
	// away.
	DefaultIssueBurst = 10

	// limiterIdle is how long a source is remembered after its last
	// request. The table is what an attacker would grow instead, so it
	// forgets.
	limiterIdle = time.Hour

	// maxTrackedSources caps the table outright. Past this, the oldest
	// entries go: a limiter that can be made to consume memory without
	// bound is a denial of service with extra steps.
	maxTrackedSources = 50_000
)

// Limiter counts requests per source address.
type Limiter struct {
	// TrustForwardedFor says the service sits behind a proxy that sets
	// X-Forwarded-For. Leave it off when the service is exposed
	// directly: a header anybody can set is not an identity, and
	// trusting it would let one caller present a new address per
	// request.
	TrustForwardedFor bool

	rate  float64
	burst float64

	mu      sync.Mutex
	sources map[string]*bucket
	nowFn   func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewLimiter prepares a limiter. Zero values take the defaults.
func NewLimiter(rate, burst float64) *Limiter {
	if rate <= 0 {
		rate = DefaultIssueRate
	}
	if burst <= 0 {
		burst = DefaultIssueBurst
	}
	return &Limiter{
		rate:    rate,
		burst:   burst,
		sources: map[string]*bucket{},
		nowFn:   time.Now,
	}
}

// Allow reports whether a request from this source may proceed.
func (l *Limiter) Allow(source string) bool {
	now := l.nowFn()

	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.sources[source]
	if !ok {
		if len(l.sources) >= maxTrackedSources {
			l.evictLocked(now)
		}
		b = &bucket{tokens: l.burst, last: now}
		l.sources[source] = b
	}

	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens += elapsed * l.rate
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
		b.last = now
	}

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// evictLocked drops sources that have gone quiet, and if that frees
// nothing, drops the least recently seen.
//
// Never refuse instead: a full table would otherwise lock out every new
// caller, which is exactly the outcome whoever filled it wanted.
func (l *Limiter) evictLocked(now time.Time) {
	for key, b := range l.sources {
		if now.Sub(b.last) > limiterIdle {
			delete(l.sources, key)
		}
	}
	if len(l.sources) < maxTrackedSources {
		return
	}

	var oldestKey string
	var oldest time.Time
	for key, b := range l.sources {
		if oldestKey == "" || b.last.Before(oldest) {
			oldestKey, oldest = key, b.last
		}
	}
	delete(l.sources, oldestKey)
}

// Sweep forgets sources that have gone quiet. Run it periodically so a
// long-lived service does not hold a table of addresses that stopped
// calling weeks ago.
func (l *Limiter) Sweep() int {
	now := l.nowFn()

	l.mu.Lock()
	defer l.mu.Unlock()

	var dropped int
	for key, b := range l.sources {
		if now.Sub(b.last) > limiterIdle {
			delete(l.sources, key)
			dropped++
		}
	}
	return dropped
}

// Tracked reports how many sources are remembered.
func (l *Limiter) Tracked() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.sources)
}

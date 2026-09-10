package transport

import (
	"sync"
	"time"
)

// Identifying an unknown peer costs one decryption attempt per configured
// credential, so the work an attacker can provoke with a single junk
// packet grows with the number of clients. Measured on a modest server:
// roughly 0.54µs per credential, which at a thousand clients is 540µs a
// packet — about 1850 packets a second, well under a megabit, to saturate
// a core.
//
// The usual answer, a cookie challenge like WireGuard's, is unavailable
// here: replying to an unauthenticated packet is exactly what would let a
// prober distinguish this port from silence, which is the property the
// whole design rests on. So the work is capped instead.
//
// Only first contact is limited. An established session decrypts under
// the single key it authenticated with and never reaches this path, so a
// flood cannot slow down traffic that is already flowing — it can only
// delay new peers, and only while it lasts.
const (
	// trialCPUBudget is the fraction of one core that trial
	// authentication may consume. The rest belongs to carrying traffic.
	trialCPUBudget = 0.05

	// trialCostPerCredential is the measured cost of one decryption
	// attempt (see BenchmarkAuthenticateGarbage). Deriving the rate from
	// a measurement rather than a guessed constant is what keeps the
	// budget meaningful as the credential list grows.
	trialCostPerCredential = 600 * time.Nanosecond

	// minTrialRate keeps a very large deployment usable: even at the
	// point where the budget would allow almost nothing, new peers still
	// get in, just more slowly.
	minTrialRate = 20.0

	// maxTrialRate applies when there are few credentials, where each
	// attempt is cheap enough that the limit is not the interesting
	// constraint.
	maxTrialRate = 5000.0

	// trialBurst lets a cluster of genuine reconnections — a server
	// restart, a network coming back — proceed at once instead of being
	// spread out by the steady rate.
	trialBurst = 200.0
)

// trialLimiter is a token bucket over authentication attempts for peers
// that are not yet known.
type trialLimiter struct {
	mu       sync.Mutex
	tokens   float64
	rate     float64
	burst    float64
	lastFill time.Time
	nowFn    func() time.Time
}

// newTrialLimiter sizes the bucket for a given number of credentials, so
// the CPU spent identifying strangers stays bounded however long the
// client list gets.
func newTrialLimiter(credentialCount int) *trialLimiter {
	rate := rateForCredentials(credentialCount)
	return &trialLimiter{
		tokens:   trialBurst,
		rate:     rate,
		burst:    trialBurst,
		lastFill: time.Now(),
		nowFn:    time.Now,
	}
}

func rateForCredentials(count int) float64 {
	if count <= 0 {
		return maxTrialRate
	}
	perAttempt := float64(count) * trialCostPerCredential.Seconds()
	rate := trialCPUBudget / perAttempt

	if rate > maxTrialRate {
		return maxTrialRate
	}
	if rate < minTrialRate {
		return minTrialRate
	}
	return rate
}

// allow reports whether another unknown peer may be put through the
// credential search right now.
func (l *trialLimiter) allow() bool {
	now := l.nowFn()

	l.mu.Lock()
	defer l.mu.Unlock()

	elapsed := now.Sub(l.lastFill).Seconds()
	if elapsed > 0 {
		l.tokens += elapsed * l.rate
		if l.tokens > l.burst {
			l.tokens = l.burst
		}
		l.lastFill = now
	}

	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}

// resize adjusts the rate after the credential list changes, so adding
// clients tightens the budget rather than quietly widening the exposure.
func (l *trialLimiter) resize(credentialCount int) {
	rate := rateForCredentials(credentialCount)

	l.mu.Lock()
	defer l.mu.Unlock()
	l.rate = rate
}

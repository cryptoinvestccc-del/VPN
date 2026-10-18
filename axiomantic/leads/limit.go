package main

import (
	"sync"
	"time"
)

// Limiter allows a few leads per address and a ceiling for everyone
// together, both over a sliding window. A person sends one or two forms;
// the global ceiling is what keeps a botnet from flooding the chat.
type Limiter struct {
	mu        sync.Mutex
	window    time.Duration
	perIP     int
	total     int
	byIP      map[string][]time.Time
	all       []time.Time
	lastSweep time.Time
}

func NewLimiter(window time.Duration, perIP, total int) *Limiter {
	return &Limiter{window: window, perIP: perIP, total: total, byIP: map[string][]time.Time{}}
}

func recent(ts []time.Time, since time.Time) []time.Time {
	i := 0
	for i < len(ts) && !ts[i].After(since) {
		i++
	}
	return ts[i:]
}

// Allow records an attempt and says whether it may go ahead; when it may
// not, it says how long until the oldest attempt leaves the window.
func (l *Limiter) Allow(ip string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	since := now.Add(-l.window)
	if now.Sub(l.lastSweep) > l.window {
		for k, ts := range l.byIP {
			if ts = recent(ts, since); len(ts) == 0 {
				delete(l.byIP, k)
			} else {
				l.byIP[k] = ts
			}
		}
		l.lastSweep = now
	}
	l.all = recent(l.all, since)
	mine := recent(l.byIP[ip], since)
	// A refused address keeps only what it already had: no entry is
	// made for an address that never got a slot, so refusals cannot grow
	// the map.
	keep := func() {
		if len(mine) > 0 {
			l.byIP[ip] = mine
		} else {
			delete(l.byIP, ip)
		}
	}
	switch {
	case len(mine) >= l.perIP:
		keep()
		return false, mine[0].Sub(since)
	case len(l.all) >= l.total:
		keep()
		return false, l.all[0].Sub(since)
	}
	l.byIP[ip] = append(mine, now)
	l.all = append(l.all, now)
	return true, 0
}

// Over says whether a key has used up its allowance, without counting
// this look as an attempt. With Hit it counts only what should count —
// for sign-in, failures — so nobody can spend another person's allowance
// by merely knocking.
func (l *Limiter) Over(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	since := now.Add(-l.window)
	mine := recent(l.byIP[key], since)
	l.byIP[key] = mine
	if len(mine) == 0 {
		delete(l.byIP, key)
	}
	if len(mine) >= l.perIP {
		return true, mine[0].Sub(since)
	}
	return false, 0
}

// Hit counts one attempt for a key, sweeping old keys now and then.
func (l *Limiter) Hit(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	since := now.Add(-l.window)
	if now.Sub(l.lastSweep) > l.window {
		for k, ts := range l.byIP {
			if ts = recent(ts, since); len(ts) == 0 {
				delete(l.byIP, k)
			} else {
				l.byIP[k] = ts
			}
		}
		l.lastSweep = now
	}
	l.byIP[key] = append(recent(l.byIP[key], since), now)
}

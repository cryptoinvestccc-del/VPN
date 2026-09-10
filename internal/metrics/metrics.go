// Package metrics exposes what an operator needs to run this service:
// how many clients are connected, how much traffic is moving, and how
// often unauthenticated peers are knocking.
//
// The exposition format is Prometheus text, written by hand. A metrics
// client library would be a large dependency for a few counters, and this
// project deliberately keeps the supply chain of a VPN small.
//
// A note on what is deliberately absent. Counting bytes per client is a
// record of who used the service and when — a log, in everything but
// name, and at odds with the no-logs position a VPN rests on. Aggregate
// counters are therefore the default, and per-client breakdowns are
// something the operator has to switch on knowingly.
package metrics

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Registry collects the counters the transport reports into.
//
// The zero value is usable and does nothing, so code paths that have no
// metrics configured need no nil checks.
type Registry struct {
	// Aggregate counters, always collected. None of them say anything
	// about an individual client.
	sessionsOpened   atomic.Int64
	sessionsClosed   atomic.Int64
	sessionsActive   atomic.Int64
	bytesIn          atomic.Int64
	bytesOut         atomic.Int64
	packetsIn        atomic.Int64
	packetsOut       atomic.Int64
	authFailures     atomic.Int64
	replaysRejected  atomic.Int64
	revocationsFired atomic.Int64
	fallbackServed   atomic.Int64

	// perClient is populated only when the operator opts in.
	perClientEnabled bool
	mu               sync.Mutex
	perClient        map[string]*clientCounters
}

type clientCounters struct {
	sessionsOpened int64
	bytesIn        int64
	bytesOut       int64
}

// New returns a registry. Set perClient only where the operator has
// accepted that the server will keep per-device usage figures.
func New(perClient bool) *Registry {
	return &Registry{
		perClientEnabled: perClient,
		perClient:        make(map[string]*clientCounters),
	}
}

// The recording methods are all safe on a nil Registry, so the transport
// can call them unconditionally.

func (r *Registry) SessionOpened(clientID string) {
	if r == nil {
		return
	}
	r.sessionsOpened.Add(1)
	r.sessionsActive.Add(1)
	r.withClient(clientID, func(c *clientCounters) { c.sessionsOpened++ })
}

func (r *Registry) SessionClosed() {
	if r == nil {
		return
	}
	r.sessionsClosed.Add(1)
	r.sessionsActive.Add(-1)
}

func (r *Registry) PacketIn(clientID string, bytes int) {
	if r == nil {
		return
	}
	r.packetsIn.Add(1)
	r.bytesIn.Add(int64(bytes))
	r.withClient(clientID, func(c *clientCounters) { c.bytesIn += int64(bytes) })
}

func (r *Registry) PacketOut(clientID string, bytes int) {
	if r == nil {
		return
	}
	r.packetsOut.Add(1)
	r.bytesOut.Add(int64(bytes))
	r.withClient(clientID, func(c *clientCounters) { c.bytesOut += int64(bytes) })
}

// AuthFailure counts a peer that could not prove it holds a credential.
// On a public port this is mostly internet background noise and censor
// probing, so a sudden change in its rate is worth an operator's
// attention.
func (r *Registry) AuthFailure() {
	if r == nil {
		return
	}
	r.authFailures.Add(1)
}

func (r *Registry) ReplayRejected() {
	if r == nil {
		return
	}
	r.replaysRejected.Add(1)
}

func (r *Registry) RevocationEnforced(n int) {
	if r == nil || n == 0 {
		return
	}
	r.revocationsFired.Add(int64(n))
}

func (r *Registry) FallbackServed() {
	if r == nil {
		return
	}
	r.fallbackServed.Add(1)
}

func (r *Registry) withClient(clientID string, update func(*clientCounters)) {
	if !r.perClientEnabled || clientID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	c, ok := r.perClient[clientID]
	if !ok {
		c = &clientCounters{}
		r.perClient[clientID] = c
	}
	update(c)
}

// Expose renders the current values in Prometheus text format.
func (r *Registry) Expose() string {
	if r == nil {
		return ""
	}

	var b strings.Builder
	counter := func(name, help string, value int64) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s counter\n%s %d\n", name, help, name, name, value)
	}
	gauge := func(name, help string, value int64) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n%s %d\n", name, help, name, name, value)
	}

	gauge("obfsvpn_sessions_active", "Client sessions currently established.", r.sessionsActive.Load())
	counter("obfsvpn_sessions_opened_total", "Client sessions established since start.", r.sessionsOpened.Load())
	counter("obfsvpn_sessions_closed_total", "Client sessions ended since start.", r.sessionsClosed.Load())
	counter("obfsvpn_bytes_received_total", "Bytes received from clients.", r.bytesIn.Load())
	counter("obfsvpn_bytes_sent_total", "Bytes sent to clients.", r.bytesOut.Load())
	counter("obfsvpn_packets_received_total", "Packets received from clients.", r.packetsIn.Load())
	counter("obfsvpn_packets_sent_total", "Packets sent to clients.", r.packetsOut.Load())
	counter("obfsvpn_auth_failures_total",
		"Peers that failed to authenticate. On a public port this is mostly scanning and probing.",
		r.authFailures.Load())
	counter("obfsvpn_replays_rejected_total",
		"Packets refused because they had already opened a session.", r.replaysRejected.Load())
	counter("obfsvpn_revocations_enforced_total",
		"Sessions closed because their client's credential was withdrawn.", r.revocationsFired.Load())
	counter("obfsvpn_fallback_served_total",
		"Unauthorized connections handed to the web-server fallback.", r.fallbackServed.Load())

	if !r.perClientEnabled {
		return b.String()
	}

	r.mu.Lock()
	ids := make([]string, 0, len(r.perClient))
	for id := range r.perClient {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	if len(ids) > 0 {
		b.WriteString("# HELP obfsvpn_client_bytes_received_total Bytes received from one client.\n")
		b.WriteString("# TYPE obfsvpn_client_bytes_received_total counter\n")
		for _, id := range ids {
			fmt.Fprintf(&b, "obfsvpn_client_bytes_received_total{client=%q} %d\n", id, r.perClient[id].bytesIn)
		}
		b.WriteString("# HELP obfsvpn_client_bytes_sent_total Bytes sent to one client.\n")
		b.WriteString("# TYPE obfsvpn_client_bytes_sent_total counter\n")
		for _, id := range ids {
			fmt.Fprintf(&b, "obfsvpn_client_bytes_sent_total{client=%q} %d\n", id, r.perClient[id].bytesOut)
		}
		b.WriteString("# HELP obfsvpn_client_sessions_opened_total Sessions opened by one client.\n")
		b.WriteString("# TYPE obfsvpn_client_sessions_opened_total counter\n")
		for _, id := range ids {
			fmt.Fprintf(&b, "obfsvpn_client_sessions_opened_total{client=%q} %d\n", id, r.perClient[id].sessionsOpened)
		}
	}
	r.mu.Unlock()

	return b.String()
}

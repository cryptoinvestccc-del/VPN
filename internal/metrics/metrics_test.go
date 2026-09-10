package metrics

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNilRegistryIsSafe(t *testing.T) {
	// The transport records unconditionally, so a server with metrics
	// switched off must not need a nil check at every call site.
	var r *Registry
	r.SessionOpened("someone")
	r.SessionClosed()
	r.PacketIn("someone", 100)
	r.PacketOut("someone", 100)
	r.AuthFailure()
	r.ReplayRejected()
	r.RevocationEnforced(3)
	r.FallbackServed()

	if got := r.Expose(); got != "" {
		t.Fatalf("a nil registry produced output: %q", got)
	}
}

func TestAggregateCounters(t *testing.T) {
	r := New(false)

	r.SessionOpened("laptop")
	r.SessionOpened("phone")
	r.SessionClosed()
	r.PacketIn("laptop", 1400)
	r.PacketOut("laptop", 600)
	r.AuthFailure()
	r.AuthFailure()
	r.ReplayRejected()
	r.RevocationEnforced(2)
	r.FallbackServed()

	out := r.Expose()
	for _, want := range []string{
		"obfsvpn_sessions_active 1",
		"obfsvpn_sessions_opened_total 2",
		"obfsvpn_sessions_closed_total 1",
		"obfsvpn_bytes_received_total 1400",
		"obfsvpn_bytes_sent_total 600",
		"obfsvpn_packets_received_total 1",
		"obfsvpn_packets_sent_total 1",
		"obfsvpn_auth_failures_total 2",
		"obfsvpn_replays_rejected_total 1",
		"obfsvpn_revocations_enforced_total 2",
		"obfsvpn_fallback_served_total 1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// TestPerClientCountersAreOptOut is the privacy property. Counting bytes
// per client records who used the service and when — a usage log in all
// but name — so a server must not produce one unless the operator asked
// for it.
func TestPerClientCountersAreOptOut(t *testing.T) {
	off := New(false)
	off.SessionOpened("laptop")
	off.PacketIn("laptop", 1000)
	off.PacketOut("laptop", 500)

	out := off.Expose()
	if strings.Contains(out, "laptop") {
		t.Fatalf("a client id appeared in metrics that were not opted in:\n%s", out)
	}
	if strings.Contains(out, "obfsvpn_client_") {
		t.Fatalf("per-client series were produced without opting in:\n%s", out)
	}

	on := New(true)
	on.SessionOpened("laptop")
	on.PacketIn("laptop", 1000)
	on.PacketOut("laptop", 500)

	out = on.Expose()
	for _, want := range []string{
		`obfsvpn_client_bytes_received_total{client="laptop"} 1000`,
		`obfsvpn_client_bytes_sent_total{client="laptop"} 500`,
		`obfsvpn_client_sessions_opened_total{client="laptop"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestConcurrentRecording(t *testing.T) {
	r := New(true)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "client-" + string(rune('a'+i%5))
			for j := 0; j < 100; j++ {
				r.PacketIn(id, 10)
				r.PacketOut(id, 20)
			}
		}(i)
	}
	wg.Wait()

	out := r.Expose()
	if !strings.Contains(out, "obfsvpn_bytes_received_total 50000") {
		t.Errorf("aggregate byte count is wrong under concurrency:\n%s", out)
	}
	if !strings.Contains(out, "obfsvpn_bytes_sent_total 100000") {
		t.Errorf("aggregate byte count is wrong under concurrency:\n%s", out)
	}
}

func TestExposeIsValidPrometheusText(t *testing.T) {
	r := New(true)
	r.SessionOpened("a")
	r.PacketIn("a", 1)

	for _, line := range strings.Split(strings.TrimSpace(r.Expose()), "\n") {
		if strings.HasPrefix(line, "#") {
			if !strings.HasPrefix(line, "# HELP ") && !strings.HasPrefix(line, "# TYPE ") {
				t.Fatalf("unexpected comment line: %q", line)
			}
			continue
		}
		// A sample line is "name value" or "name{labels} value".
		if len(strings.Fields(line)) != 2 {
			t.Fatalf("malformed sample line: %q", line)
		}
	}
}

func TestServeExposesMetrics(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	r := New(false)
	r.SessionOpened("x")

	go func() {
		if err := Serve(ctx, addr, r); err != nil {
			t.Logf("metrics server stopped: %v", err)
		}
	}()

	deadline := time.Now().Add(5 * time.Second)
	var body string
	for {
		resp, err := http.Get("http://" + addr + "/metrics")
		if err == nil {
			data, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			body = string(data)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("metrics endpoint never came up: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !strings.Contains(body, "obfsvpn_sessions_opened_total 1") {
		t.Fatalf("metrics endpoint served unexpected content:\n%s", body)
	}

	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz returned %d", resp.StatusCode)
	}
}

func TestServeDisabledWhenNoAddress(t *testing.T) {
	// An empty address means "no endpoint", and must return immediately
	// rather than binding something unexpected.
	done := make(chan error, 1)
	go func() { done <- Serve(context.Background(), "", New(false)) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("an empty metrics address should be a no-op, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve blocked on an empty address instead of returning")
	}
}

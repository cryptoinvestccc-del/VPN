package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cryptoinvestccc-del/vpn/internal/serverstat"
)

func TestSampleServerScrollsSecondBySecond(t *testing.T) {
	a := sampleServer(fixedNow())
	b := sampleServer(fixedNow().Add(time.Second))

	if !a.Mock || !a.Reachable {
		t.Fatalf("sample must be marked mock and reachable: %+v", a)
	}
	if len(a.History) != serverstat.HistoryLen {
		t.Fatalf("history = %d points, want %d", len(a.History), serverstat.HistoryLen)
	}
	// One second later the line has moved left by one point, not been
	// redrawn: the page would otherwise flicker instead of scroll.
	for i := 1; i < len(a.History); i++ {
		if a.History[i] != b.History[i-1] {
			t.Fatalf("point %d changed between consecutive seconds", i)
		}
	}
	if a.ThroughputMbps != a.History[len(a.History)-1].V {
		t.Error("headline speed must be the last point on the chart")
	}
}

func TestServerRouteIsNeverCached(t *testing.T) {
	rec := get(t, "/api/v1/server")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	var s Server
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	if !s.Mock {
		t.Error("SampleSource must say its server figures are a sample")
	}
}

func TestAgentSourceProxiesAndThrottles(t *testing.T) {
	var calls atomic.Int32
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer s3cret" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(serverstat.Snapshot{ClientsOnline: 9, ThroughputMbps: 12.5})
	}))
	defer agent.Close()

	src := &AgentSource{Source: SampleSource{}, URL: agent.URL, Token: "s3cret"}
	now := fixedNow()

	s := src.Server(now)
	if !s.Reachable || s.Mock || s.ClientsOnline != 9 || s.ThroughputMbps != 12.5 {
		t.Fatalf("got %+v", s)
	}
	// Many visitors inside the same second cost the agent one request.
	for i := 0; i < 20; i++ {
		src.Server(now.Add(500 * time.Millisecond))
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("agent asked %d times within a second, want 1", n)
	}
	src.Server(now.Add(1100 * time.Millisecond))
	if n := calls.Load(); n != 2 {
		t.Errorf("agent asked %d times after a second, want 2", n)
	}
}

func TestAgentSourceReportsUnreachable(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no fresh sample", http.StatusServiceUnavailable)
	}))
	defer agent.Close()

	s := (&AgentSource{Source: SampleSource{}, URL: agent.URL}).Server(fixedNow())
	if s.Reachable || s.Mock {
		t.Errorf("a failing agent must read as unreachable, not as a sample: %+v", s)
	}
	body, _ := json.Marshal(s)
	if !strings.Contains(string(body), `"history":[]`) {
		t.Errorf("history must be an empty list, not null: %s", body)
	}
	if strings.Contains(string(body), "503") || strings.Contains(string(body), "sample") {
		t.Errorf("the agent's error must not reach the public page: %s", body)
	}
}

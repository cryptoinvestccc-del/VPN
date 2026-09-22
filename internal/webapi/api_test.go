package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fixedNow() time.Time {
	return time.Date(2026, 9, 22, 10, 30, 0, 0, time.UTC)
}

func newHandler() http.Handler {
	return Handler(SampleSource{}, fixedNow)
}

func get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	newHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestStatusServesJSON(t *testing.T) {
	rec := get(t, "/api/v1/status")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store: a cached node count is worse than a slow one", cc)
	}

	var got Status
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Mock {
		t.Error("mock = false, but these are the built-in sample figures; the page captions them on this field")
	}
	if len(got.Tiles) == 0 || len(got.Throughput.Points) < 2 {
		t.Fatalf("tiles = %d, points = %d, want a populated response", len(got.Tiles), len(got.Throughput.Points))
	}
	if got.NodesOnline > got.NodesTotal {
		t.Errorf("nodes_online = %d exceeds nodes_total = %d", got.NodesOnline, got.NodesTotal)
	}
}

// The sample figures must not jitter between requests: a number that
// changes by tens of percent on every refresh reads as a fault rather
// than as live data.
func TestSampleIsStableForTheSameInstant(t *testing.T) {
	first := get(t, "/api/v1/status").Body.String()
	second := get(t, "/api/v1/status").Body.String()

	if first != second {
		t.Error("two requests for the same instant returned different bodies")
	}
}

func TestSampleTimestampsFollowTheClock(t *testing.T) {
	later := fixedNow().Add(time.Hour)
	rec := httptest.NewRecorder()
	Handler(SampleSource{}, func() time.Time { return later }).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))

	var got Status
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.GeneratedAt != later.Format(time.RFC3339) {
		t.Errorf("generated_at = %q, want %q", got.GeneratedAt, later.Format(time.RFC3339))
	}

	last := got.Throughput.Points[len(got.Throughput.Points)-1]
	if last.T != later.Format(time.RFC3339) {
		t.Errorf("last point at %q, want the current instant %q", last.T, later.Format(time.RFC3339))
	}
}

// This endpoint is public, unlike the Prometheus one, which is loopback
// only on purpose. Nothing it serves may be per-client: that would put on
// the open internet exactly the record internal/metrics makes the
// operator opt into.
func TestPublicResponsesCarryNoPerClientData(t *testing.T) {
	for _, path := range []string{"/api/v1/status", "/api/v1/locations", "/api/v1/plans"} {
		body := strings.ToLower(get(t, path).Body.String())
		for _, banned := range []string{`"client`, `"peer`, `"psk`, `"key"`, `"addr`, `"host`, `"ip"`} {
			if strings.Contains(body, banned) {
				t.Errorf("%s exposes %s in a public response", path, banned)
			}
		}
	}
}

// Node addresses are the other thing this must not publish: a list of
// them is a blocklist someone else does not have to assemble.
func TestLocationsOmitAddresses(t *testing.T) {
	rec := get(t, "/api/v1/locations")

	var got []Location
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("no locations")
	}
	for _, l := range got {
		if l.City == "" || l.Country == "" {
			t.Errorf("location %q is missing its place", l.ID)
		}
		if l.Mode != "tls" && l.Mode != "udp" {
			t.Errorf("location %q has mode %q, want tls or udp", l.ID, l.Mode)
		}
		if l.LoadPct < 0 || l.LoadPct > 100 {
			t.Errorf("location %q reports load %d%%", l.ID, l.LoadPct)
		}
	}
}

func TestPlansAreRenderable(t *testing.T) {
	rec := get(t, "/api/v1/plans")

	var got []Plan
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	featured := 0
	for _, p := range got {
		if p.Name == "" || p.CTA == "" || len(p.Features) == 0 {
			t.Errorf("plan %q is missing something the card renders", p.ID)
		}
		if p.Featured {
			featured++
		}
	}
	if featured > 1 {
		t.Errorf("%d plans marked featured; the layout has one highlighted slot", featured)
	}
}

func TestWriteMethodsAreRejected(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := httptest.NewRecorder()
		newHandler().ServeHTTP(rec, httptest.NewRequest(method, "/api/v1/status", nil))

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /api/v1/status = %d, want 405: this API has no write path", method, rec.Code)
		}
	}
}

func TestHeadReturnsHeadersWithoutABody(t *testing.T) {
	rec := httptest.NewRecorder()
	newHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/api/v1/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD status = %d, want 200", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("HEAD returned %d bytes of body", rec.Body.Len())
	}
}

func TestUnknownEndpointIsJSON(t *testing.T) {
	rec := get(t, "/api/v1/nope")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want JSON: a client that asked for JSON should not get HTML", ct)
	}
}

func TestTileDeltasCarryTheirDirection(t *testing.T) {
	var got Status
	if err := json.Unmarshal(get(t, "/api/v1/status").Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	for _, tile := range got.Tiles {
		if tile.Delta != nil && tile.DeltaWindow == "" {
			t.Errorf("tile %q has a delta with no window; a percentage with no period is not a fact", tile.ID)
		}
		if len(tile.Series) < 2 {
			t.Errorf("tile %q has %d points, too few to draw a sparkline", tile.ID, len(tile.Series))
		}
	}

	// Rejected probes falling is good news, and the page colours the
	// delta on this flag rather than on its sign.
	for _, tile := range got.Tiles {
		if tile.ID == "probes" && tile.GoodWhenUp {
			t.Error("rejected probes are marked good_when_up; a rise in scanning is not good news")
		}
	}
}

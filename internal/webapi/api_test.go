package webapi

import (
	"encoding/json"
	"fmt"
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

// The landing page and the dashboard describe one network. They used to
// carry two hand-written sets of figures, and the page ended up claiming
// eleven of twelve nodes over a list of six cities while the dashboard
// said five of six. Both now read the same fleet, and this is what keeps
// them doing so.
func TestLandingAndDashboardDescribeOneNetwork(t *testing.T) {
	var status Status
	if err := json.Unmarshal(get(t, "/api/v1/status").Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	var dash Dashboard
	if err := json.Unmarshal(get(t, "/api/v1/dashboard").Body.Bytes(), &dash); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	var locations []Location
	if err := json.Unmarshal(get(t, "/api/v1/locations").Body.Bytes(), &locations); err != nil {
		t.Fatalf("decode locations: %v", err)
	}

	if status.NodesTotal != len(locations) {
		t.Errorf("status says %d nodes, the location list names %d",
			status.NodesTotal, len(locations))
	}

	stats := map[string]Stat{}
	for _, s := range dash.Stats {
		stats[s.ID] = s
	}

	if want := fmt.Sprintf("из %d", status.NodesTotal); stats["nodes"].Unit != want {
		t.Errorf("dashboard node count reads %q, status says %q", stats["nodes"].Unit, want)
	}
	if got := int(stats["nodes"].Value); got != status.NodesOnline {
		t.Errorf("dashboard says %d nodes online, status says %d", got, status.NodesOnline)
	}
	if got := int(stats["sessions"].Value); got != status.SessionsActive {
		t.Errorf("dashboard says %d sessions, status says %d", got, status.SessionsActive)
	}
	if got := stats["throughput"].Value; got != status.ThroughputMbps {
		t.Errorf("dashboard says %v Mbit/s, status says %v", got, status.ThroughputMbps)
	}

	// The headline figure is the last point of the chart under it, not a
	// number of its own beside it.
	if last := status.Throughput.Points[len(status.Throughput.Points)-1]; last.V != status.ThroughputMbps {
		t.Errorf("headline throughput %v does not match the chart's last point %v",
			status.ThroughputMbps, last.V)
	}
	for _, tile := range status.Tiles {
		if tile.ID == "throughput" && tile.Value != status.ThroughputMbps {
			t.Errorf("throughput tile reads %v beside a headline of %v", tile.Value, status.ThroughputMbps)
		}
		if tile.ID == "sessions" && int(tile.Value) != status.SessionsActive {
			t.Errorf("sessions tile reads %v beside a headline of %d", tile.Value, status.SessionsActive)
		}
	}
}

// A node under maintenance is not online, and the caption under the count
// names it. Both are read off the fleet, so neither can outlive it.
func TestNodeCountFollowsTheFleet(t *testing.T) {
	online, total := fleetStatus()
	if total != len(nodes) {
		t.Errorf("total = %d, want %d", total, len(nodes))
	}

	down := 0
	for _, n := range nodes {
		if n.status == "maintenance" {
			down++
		}
	}
	if online != total-down {
		t.Errorf("online = %d, want %d (%d under maintenance)", online, total-down, down)
	}

	note := maintenanceNote()
	for _, n := range nodes {
		if n.status == "maintenance" && !strings.Contains(note, n.id) {
			t.Errorf("caption %q does not name %s, which is under maintenance", note, n.id)
		}
	}
}

// A delta's caption names the period it was measured over. These used to
// be written by hand — "за сутки" over a series that spanned six hours —
// and a percentage attached to the wrong period is not a smaller mistake
// than a wrong percentage.
func TestDeltaCaptionsNameThePeriodMeasured(t *testing.T) {
	var got Status
	if err := json.Unmarshal(get(t, "/api/v1/status").Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	windows := map[string]string{}
	for _, tile := range got.Tiles {
		if tile.Delta == nil {
			continue
		}
		if tile.DeltaWindow == "" {
			t.Errorf("tile %q reports a delta with no period", tile.ID)
		}
		windows[tile.ID] = tile.DeltaWindow
	}

	// The three fleet tiles read the same window, so they must caption it
	// the same way.
	if windows["sessions"] != windows["throughput"] || windows["throughput"] != windows["probes"] {
		t.Errorf("fleet tiles disagree about their period: %v", windows)
	}

	// Availability is a monthly figure and is measured over days, not
	// over the six hours the fleet tiles cover.
	if !strings.Contains(windows["uptime"], "дн") {
		t.Errorf("uptime delta is captioned %q; a monthly figure is not compared over minutes", windows["uptime"])
	}
	if windows["uptime"] == windows["sessions"] {
		t.Errorf("uptime and the fleet tiles claim the same period %q", windows["uptime"])
	}
}

// The endings Russian needs, which a Sprintf with a fixed word gets
// wrong for most numbers.
func TestDurationsReadAsRussian(t *testing.T) {
	for _, c := range []struct {
		d    time.Duration
		want string
	}{
		{36 * time.Minute, "36 минут"},
		{21 * time.Minute, "21 минуту"},
		{22 * time.Minute, "22 минуты"},
		{11 * time.Minute, "11 минут"},
		{2 * time.Hour, "2 часа"},
		{5 * time.Hour, "5 часов"},
		{24 * time.Hour, "1 день"},
		{3 * 24 * time.Hour, "3 дня"},
		{14 * 24 * time.Hour, "14 дней"},
	} {
		if got := ruDuration(c.d); got != c.want {
			t.Errorf("ruDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestDashboardServesEveryPanel(t *testing.T) {
	rec := get(t, "/api/v1/dashboard")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var got Dashboard
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Mock {
		t.Error("mock = false, but these are the built-in sample figures")
	}
	if len(got.Stats) == 0 || len(got.Gauges) == 0 || len(got.TimeSeries) == 0 || len(got.Nodes) == 0 {
		t.Fatalf("empty dashboard: %d stats, %d gauges, %d panels, %d nodes",
			len(got.Stats), len(got.Gauges), len(got.TimeSeries), len(got.Nodes))
	}

	for _, panel := range got.TimeSeries {
		if len(panel.Series) == 0 {
			t.Errorf("panel %q has no series", panel.ID)
		}
		for _, s := range panel.Series {
			if len(s.Points) != got.Range.Points {
				t.Errorf("panel %q series %q has %d points, want %d — panels must share one axis",
					panel.ID, s.Name, len(s.Points), got.Range.Points)
			}
		}
	}
}

// Four is where a categorical palette stops being reliably
// distinguishable; a fifth line would have to be a generated hue, and a
// generated hue is indistinguishable from an existing one under CVD.
func TestNoPanelExceedsThePaletteLength(t *testing.T) {
	var got Dashboard
	if err := json.Unmarshal(get(t, "/api/v1/dashboard").Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	for _, panel := range got.TimeSeries {
		if len(panel.Series) > len(colors) {
			t.Errorf("panel %q has %d series for a %d-colour palette", panel.ID, len(panel.Series), len(colors))
		}
		seen := map[string]bool{}
		for _, s := range panel.Series {
			if seen[s.Color] {
				t.Errorf("panel %q reuses colour %q; two series would be one line to the reader", panel.ID, s.Color)
			}
			seen[s.Color] = true
		}
	}
}

// A dashboard that reshuffles its history on every refresh is unreadable:
// the operator cannot tell a real change from a redraw. The same wall
// clock instant must always produce the same window.
func TestDashboardIsStableAcrossRequests(t *testing.T) {
	first := get(t, "/api/v1/dashboard?range=6h").Body.String()
	second := get(t, "/api/v1/dashboard?range=6h").Body.String()

	if first != second {
		t.Error("two requests for the same instant returned different dashboards")
	}
}

// As the window slides forward by one step, the history already on screen
// has to survive: point i+1 of the old window is point i of the new one.
func TestHistoryScrollsInsteadOfBeingRedrawn(t *testing.T) {
	src := SampleSource{}
	before := src.Dashboard("6h", fixedNow())
	after := src.Dashboard("6h", fixedNow().Add(time.Duration(before.Range.StepSeconds)*time.Second))

	oldPoints := before.TimeSeries[0].Series[0].Points
	newPoints := after.TimeSeries[0].Series[0].Points

	if len(oldPoints) != len(newPoints) {
		t.Fatalf("window changed length: %d then %d", len(oldPoints), len(newPoints))
	}
	for i := 1; i < len(oldPoints); i++ {
		if oldPoints[i] != newPoints[i-1] {
			t.Fatalf("point %d changed when the window moved: %v then %v at %d",
				i, oldPoints[i], newPoints[i-1], i-1)
		}
	}
}

func TestEveryRangeInThePickerResolves(t *testing.T) {
	var got Dashboard
	if err := json.Unmarshal(get(t, "/api/v1/dashboard").Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.RangeOptions) == 0 {
		t.Fatal("the picker has no options")
	}

	for _, opt := range got.RangeOptions {
		var d Dashboard
		body := get(t, "/api/v1/dashboard?range="+opt.ID).Body.Bytes()
		if err := json.Unmarshal(body, &d); err != nil {
			t.Fatalf("range %s: decode: %v", opt.ID, err)
		}
		if d.Range.ID != opt.ID {
			t.Errorf("asked for range %q, got %q", opt.ID, d.Range.ID)
		}
		if d.Range.Points < 10 || d.Range.StepSeconds <= 0 {
			t.Errorf("range %s resolved to %d points at step %ds", opt.ID, d.Range.Points, d.Range.StepSeconds)
		}
	}
}

// An unrecognised range must not break the page — a bookmarked URL
// outlives a picker entry. It falls back, and says in the response which
// window it actually used.
func TestUnknownRangeFallsBackAndSaysSo(t *testing.T) {
	rec := get(t, "/api/v1/dashboard?range=нет-такого")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got Dashboard
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Range.ID != DefaultRange {
		t.Errorf("range = %q, want the default %q", got.Range.ID, DefaultRange)
	}
}

func TestGaugesAndBarsStayInsideTheirScale(t *testing.T) {
	var got Dashboard
	if err := json.Unmarshal(get(t, "/api/v1/dashboard").Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	for _, g := range got.Gauges {
		if g.Max <= g.Min {
			t.Errorf("gauge %q has max %v <= min %v", g.ID, g.Max, g.Min)
		}
		if g.Value < g.Min || g.Value > g.Max {
			t.Errorf("gauge %q reads %v outside [%v, %v]", g.ID, g.Value, g.Min, g.Max)
		}
	}
	for _, row := range got.BarGauge.Rows {
		if row.Value < got.BarGauge.Min || row.Value > got.BarGauge.Max {
			t.Errorf("bar %q reads %v outside [%v, %v]",
				row.Label, row.Value, got.BarGauge.Min, got.BarGauge.Max)
		}
	}
}

// The headline totals skip a drained node, so the table must not show it
// carrying traffic — two numbers on one screen that contradict each other
// are worse than either one alone.
func TestDrainedNodeCarriesNoTraffic(t *testing.T) {
	var got Dashboard
	if err := json.Unmarshal(get(t, "/api/v1/dashboard").Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	found := false
	for _, n := range got.Nodes {
		if n.Status != "maintenance" {
			continue
		}
		found = true
		if n.Sessions != 0 || n.Throughput != 0 {
			t.Errorf("node %q is in maintenance but reports %d sessions and %v Mbit/s",
				n.ID, n.Sessions, n.Throughput)
		}
	}
	if !found {
		t.Skip("no node is in maintenance in the sample fleet")
	}
}

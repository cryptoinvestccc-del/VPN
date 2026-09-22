// Package webapi serves the read-only JSON the marketing site renders:
// the network's current state, the list of nodes, and the price list.
//
// Two decisions shape everything here. The first is that this endpoint is
// public, unlike the Prometheus endpoint in internal/metrics, which is
// loopback-only on purpose. A public endpoint must therefore never expose
// anything the private one keeps back: no per-client figures, no client
// identifiers, no operational detail that would help someone probing the
// service. What it serves is the handful of aggregates a visitor would
// see on the page anyway.
//
// The second is that the figures shipped here are samples, not
// measurements, and every response says so in a `mock` field. A landing
// page that presents invented numbers as live telemetry is lying to its
// visitors; one that labels them is a mockup, which is what this is.
package webapi

// Point is one sample on a time series.
type Point struct {
	T string  `json:"t"`
	V float64 `json:"v"`
}

// Tile is a headline number with enough context to read it: what it
// measures, how it moved, and over what window.
type Tile struct {
	ID    string  `json:"id"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`

	// Delta is the signed percentage change over DeltaWindow. It is a
	// pointer because "no comparable previous period" and "no change"
	// are different statements, and the page colours them differently.
	Delta       *float64 `json:"delta,omitempty"`
	DeltaWindow string   `json:"delta_window,omitempty"`

	// GoodWhenUp decides the delta's colour. Rejected probes falling is
	// good news; sessions falling is not, and the tile cannot know which
	// it is without being told.
	GoodWhenUp bool `json:"good_when_up"`

	Series []float64 `json:"series"`
	Note   string    `json:"note"`
}

// PointSeries is a labelled run of timestamped points, as the landing
// page's single chart reads them. The dashboard's Series is a different
// shape: it carries bare values against a shared time range.
type PointSeries struct {
	Unit   string  `json:"unit"`
	Window string  `json:"window"`
	Points []Point `json:"points"`
}

// Status is what the console panel on the landing page renders.
type Status struct {
	GeneratedAt string `json:"generated_at"`

	// Mock is true while these are the built-in sample figures. The page
	// prints a different caption depending on it, so switching this
	// package onto real counters later changes the caption by itself.
	Mock bool `json:"mock"`

	SessionsActive int     `json:"sessions_active"`
	ThroughputMbps float64 `json:"throughput_mbps"`
	NodesOnline    int     `json:"nodes_online"`
	NodesTotal     int     `json:"nodes_total"`

	Tiles      []Tile      `json:"tiles"`
	Throughput PointSeries `json:"throughput"`
}

// Location is one node, as a prospective customer sees it. Deliberately
// absent: the node's address. The page needs a city and a latency figure;
// publishing a list of this operator's server addresses would hand a
// blocklist to anyone who wants to build one.
type Location struct {
	ID      string `json:"id"`
	City    string `json:"city"`
	Country string `json:"country"`
	Mode    string `json:"mode"`
	RTTMs   int    `json:"rtt_ms"`
	LoadPct int    `json:"load_pct"`
	Status  string `json:"status"`
}

// Plan is one entry in the price list.
type Plan struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Price    int      `json:"price"`
	Currency string   `json:"currency"`
	Period   string   `json:"period"`
	Note     string   `json:"note"`
	Featured bool     `json:"featured"`
	Features []string `json:"features"`
	CTA      string   `json:"cta"`
}

package webapi

import "time"

// The dashboard model. It is shaped after what an operator actually
// watches — load, throughput, latency, failed authentications — rather
// than after what is easy to draw, and it is deliberately the same shape
// a real collector would fill in.

// Threshold colours a value by severity. The name is a role, not a
// colour: the page owns the palette, and a threshold that shipped "#ff0000"
// from the server would be unreadable the moment the theme changed.
type Threshold struct {
	From  float64 `json:"from"`
	Level string  `json:"level"` // ok | warn | crit
}

// Series is one line on a panel. Color names a slot in the page's fixed
// categorical order, never a hex value, for the same reason.
type Series struct {
	Name   string    `json:"name"`
	Color  string    `json:"color"`
	Points []float64 `json:"points"`
}

// TimeSeries is a panel with one or more lines sharing an axis. There is
// never a second y-axis: two measures of different scale get two panels.
type TimeSeries struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Unit        string      `json:"unit"`
	Fill        bool        `json:"fill"`
	Stacked     bool        `json:"stacked"`
	SoftMax     *float64    `json:"soft_max,omitempty"`
	Series      []Series    `json:"series"`
	Thresholds  []Threshold `json:"thresholds,omitempty"`
	// Span is the panel's width in the 12-column grid.
	Span int `json:"span"`
}

// Stat is a single headline number with its recent shape behind it.
type Stat struct {
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	Value      float64     `json:"value"`
	Unit       string      `json:"unit"`
	Decimals   int         `json:"decimals"`
	Sparkline  []float64   `json:"sparkline"`
	Thresholds []Threshold `json:"thresholds,omitempty"`
	Delta      *float64    `json:"delta,omitempty"`
	GoodWhenUp bool        `json:"good_when_up"`
	Note       string      `json:"note"`
}

// Gauge is a value against a known ceiling — the one case where a dial
// beats a number, because the ceiling is part of the reading.
type Gauge struct {
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	Value      float64     `json:"value"`
	Min        float64     `json:"min"`
	Max        float64     `json:"max"`
	Unit       string      `json:"unit"`
	Decimals   int         `json:"decimals"`
	Thresholds []Threshold `json:"thresholds"`
}

// BarRow is one bar in a bar-gauge panel.
type BarRow struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

// BarGauge compares the same measure across nodes. Bars, not lines: the
// question is "which node is hot right now", not "how did it get there".
type BarGauge struct {
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	Unit       string      `json:"unit"`
	Min        float64     `json:"min"`
	Max        float64     `json:"max"`
	Rows       []BarRow    `json:"rows"`
	Thresholds []Threshold `json:"thresholds"`
}

// NodeRow is one line of the node table.
type NodeRow struct {
	ID         string    `json:"id"`
	Node       string    `json:"node"`
	Country    string    `json:"country"`
	Mode       string    `json:"mode"`
	Status     string    `json:"status"`
	CPUPct     float64   `json:"cpu_pct"`
	MemPct     float64   `json:"mem_pct"`
	Load       float64   `json:"load"`
	RTTMs      float64   `json:"rtt_ms"`
	Sessions   int       `json:"sessions"`
	Throughput float64   `json:"throughput_mbps"`
	Spark      []float64 `json:"spark"`
}

// TimeRange is the window every panel on the dashboard shares.
type TimeRange struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	From        string `json:"from"`
	To          string `json:"to"`
	StepSeconds int    `json:"step_seconds"`
	Points      int    `json:"points"`
}

// RangeOption is one entry in the time picker.
type RangeOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Dashboard is one request's worth of everything on the page.
//
// Range is the window actually used, which is not always the one asked
// for: an unrecognised range falls back to the default rather than
// failing, and the client reads back what it got instead of assuming.
type Dashboard struct {
	GeneratedAt  string        `json:"generated_at"`
	Mock         bool          `json:"mock"`
	Range        TimeRange     `json:"range"`
	RangeOptions []RangeOption `json:"range_options"`
	Stats        []Stat        `json:"stats"`
	Gauges       []Gauge       `json:"gauges"`
	TimeSeries   []TimeSeries  `json:"time_series"`
	BarGauge     BarGauge      `json:"bar_gauge"`
	Nodes        []NodeRow     `json:"nodes"`
}

// Ranges are the windows the picker offers, in the order it shows them.
var Ranges = []struct {
	ID    string
	Label string
	Span  time.Duration
}{
	{"15m", "Последние 15 минут", 15 * time.Minute},
	{"1h", "Последний час", time.Hour},
	{"3h", "Последние 3 часа", 3 * time.Hour},
	{"6h", "Последние 6 часов", 6 * time.Hour},
	{"12h", "Последние 12 часов", 12 * time.Hour},
	{"24h", "Последние 24 часа", 24 * time.Hour},
	{"7d", "Последние 7 дней", 7 * 24 * time.Hour},
}

// DefaultRange is what the dashboard opens on.
const DefaultRange = "6h"

// RangeOptions lists the picker's entries in display order.
func RangeOptions() []RangeOption {
	out := make([]RangeOption, 0, len(Ranges))
	for _, r := range Ranges {
		out = append(out, RangeOption{ID: r.ID, Label: r.Label})
	}
	return out
}

// targetPoints keeps every window at a similar density: enough detail to
// see a spike, few enough that the line is not a solid block of ink.
const targetPoints = 120

func resolveRange(id string, now time.Time) TimeRange {
	span := 6 * time.Hour
	label := "Последние 6 часов"
	chosen := DefaultRange
	for _, r := range Ranges {
		if r.ID == id {
			span, label, chosen = r.Span, r.Label, r.ID
			break
		}
	}

	step := span / targetPoints
	if step < time.Minute {
		step = step.Round(time.Second)
		if step < time.Second {
			step = time.Second
		}
	} else {
		step = step.Round(time.Minute)
	}
	points := int(span / step)

	// Anchoring the window to whole steps is what makes an auto-refresh
	// scroll the curve instead of reshuffling it: the same wall-clock
	// bucket always carries the same value.
	to := now.Truncate(step)
	from := to.Add(-span)

	return TimeRange{
		ID:          chosen,
		Label:       label,
		From:        from.UTC().Format(time.RFC3339),
		To:          to.UTC().Format(time.RFC3339),
		StepSeconds: int(step / time.Second),
		Points:      points,
	}
}

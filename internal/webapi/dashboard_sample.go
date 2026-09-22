package webapi

import (
	"math"
	"time"
)

// node is one server in the sample fleet. Only the first four appear as
// lines on a time-series panel: four is where a categorical palette stops
// being reliably distinguishable, and past it the honest move is a table
// or a facet, not a fifth hue. The bar gauge and the table cover all six.
type node struct {
	id      string
	name    string
	country string
	mode    string
	status  string
	// base shifts this node's curves so the fleet does not move as one.
	base float64
}

var nodes = []node{
	{"fra-1", "fra-1 · Франкфурт", "Германия", "TLS", "online", 0.00},
	{"ams-1", "ams-1 · Амстердам", "Нидерланды", "TLS", "online", 0.06},
	{"hel-1", "hel-1 · Хельсинки", "Финляндия", "UDP", "online", -0.05},
	{"waw-1", "waw-1 · Варшава", "Польша", "TLS", "degraded", 0.19},
	{"ist-1", "ist-1 · Стамбул", "Турция", "TLS", "online", 0.03},
	{"sin-1", "sin-1 · Сингапур", "Сингапур", "UDP", "maintenance", -0.22},
}

// colors is the fixed categorical order. It is assigned by position and
// never cycled: node 1 is always the first slot, whatever else is on the
// panel, so a filter that drops a node cannot repaint the survivors.
var colors = []string{"green", "blue", "orange", "purple"}

var loadThresholds = []Threshold{{From: 0, Level: "ok"}, {From: 70, Level: "warn"}, {From: 88, Level: "crit"}}

// Dashboard builds one request's worth of panels for the window `rangeID`.
func (SampleSource) Dashboard(rangeID string, now time.Time) Dashboard {
	r := resolveRange(rangeID, now)

	return Dashboard{
		GeneratedAt:  now.UTC().Format(time.RFC3339),
		Mock:         true,
		Range:        r,
		RangeOptions: RangeOptions(),
		Stats:        sampleStats(r),
		Gauges:       sampleGauges(r),
		TimeSeries:   sampleTimeSeries(r),
		BarGauge:     sampleBarGauge(r),
		Nodes:        sampleNodes(r),
	}
}

func sampleStats(r TimeRange) []Stat {
	sessions := fleetSum(r, "sessions", 120, 460)
	throughput := fleetSum(r, "throughput", 60, 240)
	failures := seriesFor("auth-failures", r, 0, 900, burst)

	return []Stat{
		{
			ID: "sessions", Title: "Активные сессии", Unit: "", Decimals: 0,
			Value: last(sessions), Sparkline: sessions,
			Delta: deltaPct(sessions), GoodWhenUp: true,
			Note: "Сумма по шести узлам",
		},
		{
			ID: "throughput", Title: "Трафик через туннель", Unit: "Мбит/с", Decimals: 0,
			Value: last(throughput), Sparkline: throughput,
			Delta: deltaPct(throughput), GoodWhenUp: true,
			Note: "Входящий и исходящий вместе",
		},
		{
			ID: "auth-failures", Title: "Неудачные аутентификации", Unit: "/мин", Decimals: 0,
			Value: last(failures), Sparkline: failures,
			Delta: deltaPct(failures), GoodWhenUp: false,
			Thresholds: []Threshold{{From: 0, Level: "ok"}, {From: 300, Level: "warn"}, {From: 650, Level: "crit"}},
			Note:       "Сканеры и пробы DPI на публичном порту",
		},
		{
			ID: "nodes", Title: "Узлы онлайн", Unit: "из 6", Decimals: 0,
			Value: 5, Sparkline: seriesFor("nodes-online", r, 4.6, 6, wave),
			GoodWhenUp: true,
			Thresholds: []Threshold{{From: 0, Level: "crit"}, {From: 4, Level: "warn"}, {From: 6, Level: "ok"}},
			Note:       "sin-1 на обслуживании",
		},
	}
}

func sampleGauges(r TimeRange) []Gauge {
	return []Gauge{
		{
			ID: "cpu", Title: "CPU, среднее по флоту", Unit: "%", Decimals: 1,
			Value: last(seriesFor("fleet-cpu", r, 18, 74, wave)),
			Min:   0, Max: 100, Thresholds: loadThresholds,
		},
		{
			ID: "memory", Title: "Память", Unit: "%", Decimals: 1,
			Value: last(seriesFor("fleet-mem", r, 34, 68, wave)),
			Min:   0, Max: 100, Thresholds: loadThresholds,
		},
		{
			ID: "uplink", Title: "Загрузка канала", Unit: "%", Decimals: 1,
			Value: last(seriesFor("fleet-uplink", r, 22, 91, wave)),
			Min:   0, Max: 100, Thresholds: loadThresholds,
		},
	}
}

func sampleTimeSeries(r TimeRange) []TimeSeries {
	lineNodes := nodes[:len(colors)]

	loadLines := make([]Series, 0, len(lineNodes))
	cpuLines := make([]Series, 0, len(lineNodes))
	sessionLines := make([]Series, 0, len(lineNodes))
	for i, n := range lineNodes {
		loadLines = append(loadLines, Series{
			Name: n.id, Color: colors[i],
			Points: shifted(seriesFor(n.id+"-load", r, 0.4, 3.4, wave), n.base*1.8),
		})
		cpuLines = append(cpuLines, Series{
			Name: n.id, Color: colors[i],
			Points: shifted(seriesFor(n.id+"-cpu", r, 14, 78, wave), n.base*40),
		})
		sessionLines = append(sessionLines, Series{
			Name: n.id, Color: colors[i],
			Points: shifted(seriesFor(n.id+"-sessions", r, 90, 320, wave), n.base*80),
		})
	}

	softMax100 := 100.0

	return []TimeSeries{
		{
			ID: "load", Title: "Нагрузка на серверы", Span: 8,
			Description: "Load average за минуту, по узлам. Выше числа ядер — очередь на процессор.",
			Unit:        "", Series: loadLines,
			Thresholds: []Threshold{{From: 2.6, Level: "warn"}},
		},
		{
			ID: "throughput", Title: "Пропускная способность", Span: 4,
			Description: "Трафик через туннель в обе стороны.",
			Unit:        "Мбит/с", Fill: true,
			Series: []Series{
				{Name: "входящий", Color: "green", Points: seriesFor("tp-in", r, 140, 640, wave)},
				{Name: "исходящий", Color: "blue", Points: seriesFor("tp-out", r, 90, 410, wave)},
			},
		},
		{
			ID: "cpu", Title: "CPU по узлам", Span: 6,
			Description: "Доля процессорного времени. Обфускация стоит одного AEAD-прохода на пакет.",
			Unit:        "%", SoftMax: &softMax100, Series: cpuLines,
			Thresholds: []Threshold{{From: 70, Level: "warn"}, {From: 88, Level: "crit"}},
		},
		{
			ID: "latency", Title: "Задержка обфускации", Span: 6,
			Description: "Время между приходом WG-пакета и отправкой обёрнутого.",
			Unit:        "мс",
			Series: []Series{
				{Name: "p50", Color: "green", Points: seriesFor("lat-p50", r, 0.4, 1.1, wave)},
				{Name: "p95", Color: "blue", Points: seriesFor("lat-p95", r, 1.2, 3.6, wave)},
				{Name: "p99", Color: "orange", Points: seriesFor("lat-p99", r, 2.8, 9.4, wave)},
			},
		},
		{
			ID: "sessions", Title: "Сессии по узлам", Span: 6,
			Description: "Открытые сессии, с накоплением.",
			Unit:        "", Fill: true, Stacked: true, Series: sessionLines,
		},
		{
			ID: "rejected", Title: "Отклонённые пакеты", Span: 6,
			Description: "То, что не прошло аутентификацию или проверку на повтор. На публичном порту это фон.",
			Unit:        "/мин", Fill: true,
			Series: []Series{
				{Name: "неверный ключ", Color: "orange", Points: seriesFor("auth-failures", r, 0, 900, burst)},
				{Name: "повтор (replay)", Color: "purple", Points: seriesFor("replays", r, 0, 140, burst)},
			},
		},
	}
}

func sampleBarGauge(r TimeRange) BarGauge {
	rows := make([]BarRow, 0, len(nodes))
	for _, n := range nodes {
		rows = append(rows, BarRow{
			Label: n.id,
			Value: clampRange(last(seriesFor(n.id+"-cpu", r, 14, 78, wave))+n.base*40, 0, 100),
		})
	}
	return BarGauge{
		ID: "node-load", Title: "Загрузка узлов сейчас", Unit: "%",
		Min: 0, Max: 100, Rows: rows, Thresholds: loadThresholds,
	}
}

func sampleNodes(r TimeRange) []NodeRow {
	out := make([]NodeRow, 0, len(nodes))
	for _, n := range nodes {
		cpu := seriesFor(n.id+"-cpu", r, 14, 78, wave)
		sessions := seriesFor(n.id+"-sessions", r, 90, 320, wave)

		// A drained node carries no traffic. The fleet totals already
		// skip it, and a table that showed it serving two hundred
		// sessions would contradict the number at the top of the page.
		sessionCount := int(math.Round(last(sessions) + n.base*80))
		throughput := round2(last(seriesFor(n.id+"-tp", r, 30, 190, wave)))
		if n.status == "maintenance" {
			sessionCount, throughput = 0, 0
		}

		out = append(out, NodeRow{
			ID:         n.id,
			Node:       n.name,
			Country:    n.country,
			Mode:       n.mode,
			Status:     n.status,
			CPUPct:     clampRange(last(cpu)+n.base*40, 0, 100),
			MemPct:     clampRange(last(seriesFor(n.id+"-mem", r, 28, 71, wave))+n.base*22, 0, 100),
			Load:       round2(clampRange(last(seriesFor(n.id+"-load", r, 0.4, 3.4, wave))+n.base*1.8, 0, 8)),
			RTTMs:      round2(last(seriesFor(n.id+"-rtt", r, 18, 130, wave))),
			Sessions:   sessionCount,
			Throughput: throughput,
			Spark:      tail(cpu, 24),
		})
	}
	return out
}

// -- helpers --------------------------------------------------------------

func seriesFor(name string, r TimeRange, lo, hi float64, gen func(uint64, int64) float64) []float64 {
	return samples(name, r, lo, hi, gen)
}

// fleetSum adds up one metric across the fleet, so the headline number
// and the per-node panel below it cannot drift apart.
func fleetSum(r TimeRange, metric string, lo, hi float64) []float64 {
	total := make([]float64, r.Points)
	for _, n := range nodes {
		if n.status == "maintenance" {
			continue
		}
		s := seriesFor(n.id+"-"+metric, r, lo, hi, wave)
		for i := range total {
			total[i] += s[i]
		}
	}
	for i := range total {
		total[i] = math.Round(total[i])
	}
	return total
}

func shifted(points []float64, by float64) []float64 {
	out := make([]float64, len(points))
	for i, v := range points {
		out[i] = round2(math.Max(0, v+by))
	}
	return out
}

func last(points []float64) float64 {
	if len(points) == 0 {
		return 0
	}
	return points[len(points)-1]
}

func tail(points []float64, n int) []float64 {
	if len(points) <= n {
		return points
	}
	return points[len(points)-n:]
}

// deltaPct compares the last value with the one a tenth of the window
// back. Against the very first point it would report the window's whole
// swing and call it "change", which is a different claim.
func deltaPct(points []float64) *float64 {
	if len(points) < 10 {
		return nil
	}
	prev := points[len(points)-1-len(points)/10]
	if prev == 0 {
		return nil
	}
	d := round2((last(points) - prev) / prev * 100)
	return &d
}

func clampRange(v, lo, hi float64) float64 {
	return round2(math.Min(hi, math.Max(lo, v)))
}

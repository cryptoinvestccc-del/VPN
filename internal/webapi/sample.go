package webapi

import (
	"math"
	"time"
)

// Source is where the API gets its figures. The landing page ships with
// SampleSource; wiring the site to a live server means implementing this
// against metrics.Registry and nothing else changes.
type Source interface {
	Status(now time.Time) Status
	Locations() []Location
	Plans() []Plan
	Dashboard(rangeID string, now time.Time) Dashboard
	Server(now time.Time) Server
}

// SampleSource serves invented but plausible figures, marked as such.
//
// The curve is generated from a fixed seed so that two requests a second
// apart do not disagree with each other: a number that jumps by 40% on
// every refresh reads as broken rather than as live. Only the timestamps
// advance with the clock.
type SampleSource struct{}

// Status is built from the same fleet the dashboard draws, over the
// dashboard's default window, rather than from a second set of
// hand-written numbers.
//
// It used to be its own set, and the two drifted apart exactly as you
// would expect: the page said eleven of twelve nodes were online while
// the node list under it named six cities and the dashboard said five of
// six, and the headline throughput disagreed with the tile beside it.
// A visitor who clicked through from the page to the dashboard was
// looking at a different service. One generator cannot do that.
func (SampleSource) Status(now time.Time) Status {
	r := resolveRange(DefaultRange, now)

	sessions := fleetSum(r, "sessions", 120, 460)
	throughput := fleetSum(r, "throughput", 60, 240)
	probes := seriesFor("landing-probes", r, 18000, 39000, wave)

	// Availability is a monthly statistic, so it gets a month of daily
	// points rather than a reading off the six-hour window beside it.
	month := monthly(r)
	uptime := seriesFor("landing-uptime", month, 99.86, 99.99, wave)

	online, total := fleetStatus()

	return Status{
		GeneratedAt:    now.UTC().Format(time.RFC3339),
		Mock:           true,
		SessionsActive: int(math.Round(last(sessions))),
		ThroughputMbps: last(throughput),
		NodesOnline:    online,
		NodesTotal:     total,
		Throughput: PointSeries{
			Unit:   "Мбит/с",
			Window: windowLabel(r),
			Points: stamp(r, throughput),
		},
		Tiles: []Tile{
			{
				ID:          "sessions",
				Label:       "Активные сессии",
				Value:       last(sessions),
				Delta:       deltaPct(sessions),
				DeltaWindow: deltaSpan(sessions, r),
				GoodWhenUp:  true,
				Series:      tail(sessions, 12),
				Note:        "Сумма по работающим узлам",
			},
			{
				ID:          "throughput",
				Label:       "Трафик через туннель",
				Value:       last(throughput),
				Unit:        "Мбит/с",
				Delta:       deltaPct(throughput),
				DeltaWindow: deltaSpan(throughput, r),
				GoodWhenUp:  true,
				Series:      tail(throughput, 12),
				Note:        "Агрегат, без разбивки по клиентам",
			},
			{
				ID:          "probes",
				Label:       "Отклонённые пробы",
				Value:       math.Round(last(probes)),
				Delta:       deltaPct(probes),
				DeltaWindow: deltaSpan(probes, r),
				GoodWhenUp:  false,
				Series:      tail(probes, 12),
				Note:        "Сканеры и активный DPI-пробинг",
			},
			{
				ID:          "uptime",
				Label:       "Доступность узлов",
				Value:       last(uptime),
				Unit:        "%",
				Delta:       deltaPct(uptime),
				DeltaWindow: deltaSpan(uptime, month),
				GoodWhenUp:  true,
				Series:      tail(uptime, 12),
				Note:        "По данным внешнего мониторинга",
			},
		},
	}
}

func (SampleSource) Server(now time.Time) Server { return sampleServer(now) }

func (SampleSource) Locations() []Location {
	return []Location{
		{ID: "fra-1", City: "Франкфурт", Country: "Германия", Mode: "tls", RTTMs: 28, LoadPct: 46, Status: "online"},
		{ID: "ams-1", City: "Амстердам", Country: "Нидерланды", Mode: "tls", RTTMs: 34, LoadPct: 38, Status: "online"},
		{ID: "hel-1", City: "Хельсинки", Country: "Финляндия", Mode: "udp", RTTMs: 22, LoadPct: 61, Status: "online"},
		{ID: "waw-1", City: "Варшава", Country: "Польша", Mode: "tls", RTTMs: 25, LoadPct: 72, Status: "degraded"},
		{ID: "ist-1", City: "Стамбул", Country: "Турция", Mode: "tls", RTTMs: 41, LoadPct: 54, Status: "online"},
		{ID: "sin-1", City: "Сингапур", Country: "Сингапур", Mode: "udp", RTTMs: 118, LoadPct: 29, Status: "maintenance"},
	}
}

func (SampleSource) Plans() []Plan {
	// Тарифы и реферальный бонус — те же, что выдаёт бот. Меняются здесь,
	// а не в разметке: страница читает их через /api/v1/plans.
	return []Plan{
		{
			ID: "month", Name: "1 месяц", Price: 100, Currency: "₽", Period: "за месяц",
			Note: "Чтобы попробовать", CTA: "Взять на месяц",
			Features: []string{
				"30 дней доступа",
				"Один ключ на одно устройство",
			},
		},
		{
			ID: "half-year", Name: "6 месяцев", Price: 500, Currency: "₽", Period: "за полгода",
			Note: "83 ₽ в месяц", Featured: true, CTA: "Взять на полгода",
			Features: []string{
				"Экономия 100 ₽",
				"Полгода без продлений",
			},
		},
		{
			ID: "year", Name: "12 месяцев", Price: 900, Currency: "₽", Period: "за год",
			Note: "75 ₽ в месяц, это 2,5 ₽ в день", CTA: "Взять на год",
			Features: []string{
				"Экономия 300 ₽",
				"Год без продлений",
			},
		},
	}
}

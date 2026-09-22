package webapi

import (
	"math"
	"math/rand"
	"time"
)

// Source is where the API gets its figures. The landing page ships with
// SampleSource; wiring the site to a live server means implementing this
// against metrics.Registry and nothing else changes.
type Source interface {
	Status(now time.Time) Status
	Locations() []Location
	Plans() []Plan
}

// SampleSource serves invented but plausible figures, marked as such.
//
// The curve is generated from a fixed seed so that two requests a second
// apart do not disagree with each other: a number that jumps by 40% on
// every refresh reads as broken rather than as live. Only the timestamps
// advance with the clock.
type SampleSource struct{}

const sampleSeed = 20240917

func (SampleSource) Status(now time.Time) Status {
	points := sampleThroughput(now, 48, 5*time.Minute)
	latest := points[len(points)-1].V

	return Status{
		GeneratedAt:    now.UTC().Format(time.RFC3339),
		Mock:           true,
		SessionsActive: 1284,
		ThroughputMbps: latest,
		NodesOnline:    11,
		NodesTotal:     12,
		Throughput: Series{
			Unit:   "Мбит/с",
			Window: "последние 4 часа",
			Points: points,
		},
		Tiles: []Tile{
			{
				ID:          "sessions",
				Label:       "Активные сессии",
				Value:       1284,
				Delta:       pct(8.4),
				DeltaWindow: "за сутки",
				GoodWhenUp:  true,
				Series:      []float64{980, 1010, 995, 1074, 1120, 1088, 1160, 1205, 1190, 1240, 1262, 1284},
				Note:        "Сумма по всем узлам",
			},
			{
				ID:          "throughput",
				Label:       "Трафик через туннель",
				Value:       742,
				Unit:        "Мбит/с",
				Delta:       pct(3.1),
				DeltaWindow: "за час",
				GoodWhenUp:  true,
				Series:      []float64{612, 648, 690, 665, 704, 688, 712, 735, 720, 758, 731, 742},
				Note:        "Агрегат, без разбивки по клиентам",
			},
			{
				ID:          "probes",
				Label:       "Отклонённые пробы",
				Value:       26400,
				Delta:       pct(-12.6),
				DeltaWindow: "за сутки",
				GoodWhenUp:  false,
				Series:      []float64{34200, 33100, 32400, 31800, 30900, 30100, 29600, 28800, 28200, 27500, 26900, 26400},
				Note:        "Сканеры и активный DPI-пробинг",
			},
			{
				ID:          "uptime",
				Label:       "Доступность узлов",
				Value:       99.94,
				Unit:        "%",
				Delta:       pct(0.02),
				DeltaWindow: "за 30 дней",
				GoodWhenUp:  true,
				Series:      []float64{99.9, 99.88, 99.93, 99.95, 99.91, 99.96, 99.94, 99.92, 99.95, 99.97, 99.93, 99.94},
				Note:        "По данным внешнего мониторинга",
			},
		},
	}
}

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
	return []Plan{
		{
			ID: "solo", Name: "Solo", Price: 290, Currency: "₽", Period: "в месяц",
			Note: "Одно устройство, один ключ", CTA: "Взять Solo",
			Features: []string{
				"1 устройство",
				"Все узлы, оба режима — UDP и TLS",
				"Профиль одним файлом + QR",
				"Отзыв и перевыпуск ключа в любой момент",
			},
		},
		{
			ID: "family", Name: "Family", Price: 690, Currency: "₽", Period: "в месяц",
			Note: "Отдельный ключ на каждое устройство", Featured: true, CTA: "Взять Family",
			Features: []string{
				"До 5 устройств",
				"Индивидуальные ключи — отзыв одного не роняет остальные",
				"Все узлы, оба режима",
				"Ротация PSK без даунтайма",
				"Приоритетная поддержка",
			},
		},
		{
			ID: "self", Name: "Self-hosted", Price: 0, Currency: "₽", Period: "навсегда",
			Note: "Ваш сервер, наш код", CTA: "Читать инструкцию",
			Features: []string{
				"Исходники целиком, ставятся одним скриптом",
				"Никакой телеметрии и внешних зависимостей",
				"Сколько угодно устройств",
				"Поддержка — через issues",
			},
		},
	}
}

// sampleThroughput draws a slow swell with a little noise on top. A flat
// line reads as a placeholder, and pure noise reads as a fault; traffic
// through a real tunnel looks like neither.
func sampleThroughput(now time.Time, count int, step time.Duration) []Point {
	rng := rand.New(rand.NewSource(sampleSeed))
	points := make([]Point, 0, count)
	level := 610.0

	for i := count - 1; i >= 0; i-- {
		swell := math.Sin(float64(count-i)/7) * 120
		level += (rng.Float64() - 0.48) * 70
		level = math.Min(980, math.Max(380, level))
		points = append(points, Point{
			T: now.Add(-time.Duration(i) * step).UTC().Format(time.RFC3339),
			V: math.Round(level + swell),
		})
	}
	return points
}

func pct(v float64) *float64 { return &v }

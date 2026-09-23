/*
  A static snapshot used when /api is unreachable — a landing page has to
  render for someone who lands on it while the API is down or who is
  served the built assets from a plain file server. The authoritative
  sample figures live in internal/webapi (Go); this is a copy, and both
  are clearly marked as samples rather than live measurements.
*/

import type { Location, Plan, Status } from './types'

/** Small deterministic PRNG so the sample curve is the same every load. */
function seeded(seed: number): () => number {
  let s = seed >>> 0
  return () => {
    s = (s * 1664525 + 1013904223) >>> 0
    return s / 0x100000000
  }
}

function sampleThroughput(count: number): Status['throughput']['points'] {
  const rand = seeded(20240917)
  const end = Date.now()
  const stepMs = 5 * 60 * 1000
  const points = []
  let level = 610
  for (let i = count - 1; i >= 0; i--) {
    // A slow daily swell plus a little noise: a flat line reads as fake.
    const swell = Math.sin((count - i) / 7) * 120
    level += (rand() - 0.48) * 70
    level = Math.min(980, Math.max(380, level))
    points.push({
      t: new Date(end - i * stepMs).toISOString(),
      v: Math.round(level + swell),
    })
  }
  return points
}

export const fallbackStatus: Status = {
  generated_at: new Date().toISOString(),
  mock: true,
  sessions_active: 1284,
  throughput_mbps: 742,
  nodes_online: 11,
  nodes_total: 12,
  throughput: {
    unit: 'Мбит/с',
    window: 'последние 4 часа',
    points: sampleThroughput(48),
  },
  tiles: [
    {
      id: 'sessions',
      label: 'Активные сессии',
      value: 1284,
      unit: '',
      delta: 8.4,
      delta_window: 'за сутки',
      good_when_up: true,
      series: [980, 1010, 995, 1074, 1120, 1088, 1160, 1205, 1190, 1240, 1262, 1284],
      note: 'Сумма по всем узлам',
    },
    {
      id: 'throughput',
      label: 'Трафик через туннель',
      value: 742,
      unit: 'Мбит/с',
      delta: 3.1,
      delta_window: 'за час',
      good_when_up: true,
      series: [612, 648, 690, 665, 704, 688, 712, 735, 720, 758, 731, 742],
      note: 'Агрегат, без разбивки по клиентам',
    },
    {
      id: 'probes',
      label: 'Отклонённые пробы',
      value: 26400,
      unit: '',
      delta: -12.6,
      delta_window: 'за сутки',
      good_when_up: false,
      series: [34200, 33100, 32400, 31800, 30900, 30100, 29600, 28800, 28200, 27500, 26900, 26400],
      note: 'Сканеры и активный DPI-пробинг',
    },
    {
      id: 'uptime',
      label: 'Доступность узлов',
      value: 99.94,
      unit: '%',
      delta: 0.02,
      delta_window: 'за 30 дней',
      good_when_up: true,
      series: [99.9, 99.88, 99.93, 99.95, 99.91, 99.96, 99.94, 99.92, 99.95, 99.97, 99.93, 99.94],
      note: 'По данным внешнего мониторинга',
    },
  ],
}

export const fallbackLocations: Location[] = [
  { id: 'fra-1', city: 'Франкфурт', country: 'Германия', mode: 'tls', rtt_ms: 28, load_pct: 46, status: 'online' },
  { id: 'ams-1', city: 'Амстердам', country: 'Нидерланды', mode: 'tls', rtt_ms: 34, load_pct: 38, status: 'online' },
  { id: 'hel-1', city: 'Хельсинки', country: 'Финляндия', mode: 'udp', rtt_ms: 22, load_pct: 61, status: 'online' },
  { id: 'waw-1', city: 'Варшава', country: 'Польша', mode: 'tls', rtt_ms: 25, load_pct: 72, status: 'degraded' },
  { id: 'ist-1', city: 'Стамбул', country: 'Турция', mode: 'tls', rtt_ms: 41, load_pct: 54, status: 'online' },
  { id: 'sin-1', city: 'Сингапур', country: 'Сингапур', mode: 'udp', rtt_ms: 118, load_pct: 29, status: 'maintenance' },
]

export const fallbackPlans: Plan[] = [
  {
    id: 'month',
    name: '1 месяц',
    price: 100,
    currency: '₽',
    period: 'за месяц',
    note: 'Попробовать без обязательств',
    featured: false,
    cta: 'Взять на месяц',
    features: [
      'Доступ к серверу на 30 дней',
      'Один ключ — одно устройство',
      'Протокол AmneziaWG',
    ],
  },
  {
    id: 'half-year',
    name: '6 месяцев',
    price: 500,
    currency: '₽',
    period: 'за полгода',
    note: '83 ₽ в месяц',
    featured: true,
    cta: 'Взять на полгода',
    features: [
      'Всё то же, что в месячном',
      'Не нужно продлевать каждый месяц',
    ],
  },
  {
    id: 'year',
    name: '12 месяцев',
    price: 900,
    currency: '₽',
    period: 'за год',
    note: '75 ₽ в месяц — самый дешёвый вариант',
    featured: false,
    cta: 'Взять на год',
    features: [
      'Всё то же, что в месячном',
      'Одна оплата на год вперёд',
    ],
  },
]

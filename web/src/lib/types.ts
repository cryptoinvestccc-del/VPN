/** Shapes returned by cmd/obfsweb. Keep in step with internal/webapi. */

export type Point = {
  /** RFC3339 timestamp. */
  t: string
  v: number
}

export type Tile = {
  id: string
  label: string
  value: number
  unit: string
  /** Signed change against `delta_window`. Absent when not comparable. */
  delta?: number
  delta_window?: string
  /** Whether a rise in this number is good news, for delta colouring. */
  good_when_up: boolean
  /** 12 points of recent history, oldest first. */
  series: number[]
  note: string
}

export type Status = {
  generated_at: string
  /** True while the server is serving the built-in sample figures. */
  mock: boolean
  sessions_active: number
  throughput_mbps: number
  nodes_online: number
  nodes_total: number
  tiles: Tile[]
  throughput: {
    unit: string
    window: string
    points: Point[]
  }
}

export type Location = {
  id: string
  city: string
  country: string
  /** "tls" or "udp" — the transport this node prefers. */
  mode: 'tls' | 'udp'
  rtt_ms: number
  load_pct: number
  status: 'online' | 'degraded' | 'maintenance'
}

export type Plan = {
  id: string
  name: string
  price: number
  currency: string
  period: string
  note: string
  featured: boolean
  features: string[]
  cta: string
}

/** One VPN server, measured once a second by besy-agent. */
export type Server = {
  generated_at: string
  /** True for the built-in sample; the card says so. */
  mock: boolean
  /** False when the site could not get a fresh reading from the server. */
  reachable: boolean
  clients_online: number
  throughput_mbps: number
  cpu_pct: number
  mem_pct: number
  uptime_s: number
  history: Point[]
}

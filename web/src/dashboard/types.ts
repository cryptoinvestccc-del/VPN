/** Shapes returned by /api/v1/dashboard. Mirrors internal/webapi. */

export type Level = 'ok' | 'warn' | 'crit'

export type Threshold = {
  from: number
  level: Level
}

export type Series = {
  name: string
  /** A slot in the page's fixed categorical order, not a colour value. */
  color: 'green' | 'blue' | 'orange' | 'purple'
  points: number[]
}

export type TimeSeriesPanel = {
  id: string
  title: string
  description: string
  unit: string
  fill: boolean
  stacked: boolean
  soft_max?: number
  series: Series[]
  thresholds?: Threshold[]
  /** Width in the 12-column grid. */
  span: number
}

export type StatPanel = {
  id: string
  title: string
  value: number
  unit: string
  decimals: number
  sparkline: number[]
  thresholds?: Threshold[]
  delta?: number
  good_when_up: boolean
  note: string
}

export type GaugePanel = {
  id: string
  title: string
  value: number
  min: number
  max: number
  unit: string
  decimals: number
  thresholds: Threshold[]
}

export type BarGaugePanel = {
  id: string
  title: string
  unit: string
  min: number
  max: number
  rows: { label: string; value: number }[]
  thresholds: Threshold[]
}

export type NodeRow = {
  id: string
  node: string
  country: string
  mode: string
  status: 'online' | 'degraded' | 'maintenance'
  cpu_pct: number
  mem_pct: number
  load: number
  rtt_ms: number
  sessions: number
  throughput_mbps: number
  spark: number[]
}

export type TimeRange = {
  id: string
  label: string
  from: string
  to: string
  step_seconds: number
  points: number
}

export type DashboardData = {
  generated_at: string
  mock: boolean
  range: TimeRange
  range_options: { id: string; label: string }[]
  stats: StatPanel[]
  gauges: GaugePanel[]
  time_series: TimeSeriesPanel[]
  bar_gauge: BarGaugePanel
  nodes: NodeRow[]
}

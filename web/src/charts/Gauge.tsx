import type { GaugePanel } from '../dashboard/types'
import { levelColor, levelFor, levelLabel } from './colors'
import { metric } from '../lib/format'

/**
 * A dial, for the one case that earns one: a value against a ceiling it
 * cannot exceed, where the remaining headroom is half the reading.
 *
 * The unfilled track is a light step of the same neutral rather than a
 * second hue, so the state reads across the whole arc; the number in the
 * middle is what actually gets read, and the arc is context for it.
 */
export function Gauge({ panel }: { panel: GaugePanel }) {
  const level = levelFor(panel.value, panel.thresholds)
  const color = level ? levelColor(level) : 'var(--series-2)'

  const size = 132
  const stroke = 11
  const r = (size - stroke) / 2 - 2
  const cx = size / 2
  const cy = size / 2 + 8

  // A 240° sweep with the gap at the bottom, which is what tells the
  // reader where the scale starts and ends. Angles here are clockwise
  // from twelve o'clock, so 240° is the lower left.
  const startAngle = 240
  const sweep = 240
  const ratio = clamp((panel.value - panel.min) / (panel.max - panel.min || 1))

  return (
    <div className="gauge">
      <svg
        width={size}
        height={size}
        viewBox={`0 0 ${size} ${size}`}
        role="img"
        aria-label={`${panel.title}: ${metric(panel.value, panel.decimals)} ${panel.unit} из ${panel.max}${
          level ? `, ${levelLabel[level]}` : ''
        }`}
      >
        <path
          d={arc(cx, cy, r, startAngle, startAngle + sweep)}
          fill="none"
          stroke="var(--chart-grid)"
          strokeWidth={stroke}
          strokeLinecap="round"
        />
        <path
          d={arc(cx, cy, r, startAngle, startAngle + sweep * ratio)}
          fill="none"
          stroke={color}
          strokeWidth={stroke}
          strokeLinecap="round"
        />
        {panel.thresholds
          .filter((t) => t.from > panel.min && t.from < panel.max)
          .map((t) => {
            const a = startAngle + sweep * clamp((t.from - panel.min) / (panel.max - panel.min || 1))
            const inner = polar(cx, cy, r - stroke / 2 - 1, a)
            const outer = polar(cx, cy, r + stroke / 2 + 1, a)
            return (
              <line
                key={t.from}
                x1={inner.x}
                y1={inner.y}
                x2={outer.x}
                y2={outer.y}
                stroke="var(--surface)"
                strokeWidth={2}
              />
            )
          })}
      </svg>

      <div className="gauge__readout" style={{ top: cy - 24 }}>
        <span className="gauge__value" style={{ color }}>
          {metric(panel.value, panel.decimals)}
          <span className="gauge__unit">{panel.unit}</span>
        </span>
        {/* The level is stated, not just coloured: colour alone is not an
            accessible way to say "critical". */}
        {level && <span className="gauge__level">{levelLabel[level]}</span>}
      </div>
    </div>
  )
}

function clamp(v: number): number {
  return Math.min(1, Math.max(0, v))
}

function polar(cx: number, cy: number, r: number, angleDeg: number) {
  const a = ((angleDeg - 90) * Math.PI) / 180
  return { x: cx + r * Math.cos(a), y: cy + r * Math.sin(a) }
}

function arc(cx: number, cy: number, r: number, from: number, to: number): string {
  if (to - from < 0.01) return ''
  const start = polar(cx, cy, r, from)
  const end = polar(cx, cy, r, to)
  const large = to - from > 180 ? 1 : 0
  return `M${start.x.toFixed(2)} ${start.y.toFixed(2)} A${r} ${r} 0 ${large} 1 ${end.x.toFixed(2)} ${end.y.toFixed(2)}`
}

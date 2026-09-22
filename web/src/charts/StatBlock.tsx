import type { StatPanel } from '../dashboard/types'
import { levelColor, levelFor } from './colors'
import { metricCompact, signed } from '../lib/format'

/**
 * The stat-tile contract: label, value, delta, trend, in that order.
 *
 * The sparkline sits behind the number rather than beside it, so a row of
 * four tiles reads as four numbers first and four shapes second. The
 * delta's colour is direction × whether up is good news — a fall in
 * failed authentications is green, and a fall in sessions would not be.
 */
export function StatBlock({ stat }: { stat: StatPanel }) {
  const level = levelFor(stat.value, stat.thresholds)
  const color = level ? levelColor(level) : 'var(--ink)'
  const delta = stat.delta
  const tone =
    delta === undefined || Math.abs(delta) < 0.05
      ? 'flat'
      : delta > 0 === stat.good_when_up
        ? 'good'
        : 'bad'

  return (
    <div className="stat-block">
      <Spark points={stat.sparkline} color={color} />
      <div className="stat-block__body">
        <p className="stat-block__title">{stat.title}</p>
        <div className="stat-block__value" style={{ color }}>
          {metricCompact(stat.value, stat.decimals)}
          {stat.unit && <span className="stat-block__unit">{stat.unit}</span>}
        </div>
        <div className="stat-block__foot">
          {delta !== undefined && (
            <span className={`delta delta--${tone}`}>
              {signed(delta, 1)}%
            </span>
          )}
          <span className="stat-block__note">{stat.note}</span>
        </div>
      </div>
    </div>
  )
}

/** A background sparkline: shape only, no axes, no labels, no hover. */
function Spark({ points, color }: { points: number[]; color: string }) {
  if (points.length < 2) return null

  const w = 300
  const h = 52
  const min = Math.min(...points)
  const max = Math.max(...points)
  const span = max - min || 1
  const step = w / (points.length - 1)

  const line = points
    .map((v, i) => `${i === 0 ? 'M' : 'L'}${(i * step).toFixed(2)} ${(h - ((v - min) / span) * (h - 4) - 2).toFixed(2)}`)
    .join(' ')

  return (
    <svg
      className="stat-block__spark"
      viewBox={`0 0 ${w} ${h}`}
      preserveAspectRatio="none"
      aria-hidden="true"
      focusable="false"
    >
      <path d={`${line} L${w} ${h} L0 ${h} Z`} fill={color} opacity={0.12} />
      {/* The stroke would be stretched with the viewBox; this keeps it 2px
          wide whatever the tile's width turns out to be. */}
      <path
        d={line}
        fill="none"
        stroke={color}
        strokeWidth={2}
        opacity={0.55}
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  )
}

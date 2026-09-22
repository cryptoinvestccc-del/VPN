import { useMemo, useState } from 'react'
import type { Point } from '../lib/types'
import { group, timeOfDay } from '../lib/format'
import { useMeasure } from '../lib/useMeasure'

type Props = {
  points: Point[]
  unit: string
  /** Names what is plotted; a single series needs no legend box. */
  title: string
  height?: number
}

const PAD = { top: 12, right: 52, bottom: 22, left: 0 }

/**
 * A single-series line with a hairline grid, a crosshair on hover and the
 * current value labelled at the end. One series means one colour and no
 * legend: the title above the chart already says what is plotted.
 *
 * Every value is also in the table below the chart, so the data is never
 * gated behind a hover no keyboard or screen reader can perform.
 */
export function LineChart({ points, unit, title, height = 190 }: Props) {
  const [wrapRef, width] = useMeasure<HTMLDivElement>()
  const [hover, setHover] = useState<number | null>(null)

  const geom = useMemo(() => {
    if (width <= 0 || points.length < 2) return null

    const values = points.map((p) => p.v)
    const rawMin = Math.min(...values)
    const rawMax = Math.max(...values)
    // Clean tick numbers beat a tight fit: the reader is comparing the
    // curve against round figures, not against its own extremes.
    const step = niceStep((rawMax - rawMin) / 3 || rawMax / 3 || 1)
    const min = Math.floor(rawMin / step) * step
    const max = Math.ceil(rawMax / step) * step
    const span = max - min || 1

    const plotW = Math.max(1, width - PAD.left - PAD.right)
    const plotH = Math.max(1, height - PAD.top - PAD.bottom)
    const x = (i: number) => PAD.left + (i / (points.length - 1)) * plotW
    const y = (v: number) => PAD.top + (1 - (v - min) / span) * plotH

    const coords = points.map((p, i) => ({ x: x(i), y: y(p.v) }))
    const line = coords
      .map((c, i) => `${i === 0 ? 'M' : 'L'}${c.x.toFixed(2)} ${c.y.toFixed(2)}`)
      .join(' ')
    const area = `${line} L${coords[coords.length - 1]!.x.toFixed(2)} ${(PAD.top + plotH).toFixed(
      2,
    )} L${coords[0]!.x.toFixed(2)} ${(PAD.top + plotH).toFixed(2)} Z`

    const ticks: number[] = []
    for (let v = min; v <= max + step / 2; v += step) ticks.push(round2(v))

    return { coords, line, area, ticks, y, plotH, plotW }
  }, [points, width, height])

  const active = hover !== null ? points[hover] : null

  function onMove(event: React.PointerEvent<HTMLDivElement>) {
    if (!geom) return
    const rect = event.currentTarget.getBoundingClientRect()
    const ratio = (event.clientX - rect.left - PAD.left) / geom.plotW
    const index = Math.round(ratio * (points.length - 1))
    setHover(Math.min(points.length - 1, Math.max(0, index)))
  }

  const lastCoord = geom?.coords[geom.coords.length - 1]
  const lastPoint = points[points.length - 1]

  return (
    <figure className="chart" style={{ margin: 0 }}>
      <div
        ref={wrapRef}
        onPointerMove={onMove}
        onPointerLeave={() => setHover(null)}
        style={{ position: 'relative', touchAction: 'pan-y' }}
      >
        {geom && (
          <svg
            width={width}
            height={height}
            viewBox={`0 0 ${width} ${height}`}
            role="img"
            aria-label={`${title}. Значения перечислены в таблице под графиком.`}
            focusable="false"
          >
            {geom.ticks.map((t) => (
              <g key={t}>
                <line
                  x1={0}
                  x2={width - PAD.right + 8}
                  y1={geom.y(t)}
                  y2={geom.y(t)}
                  stroke="var(--chart-grid)"
                  strokeWidth={1}
                  shapeRendering="crispEdges"
                />
                <text
                  x={width - PAD.right + 14}
                  y={geom.y(t)}
                  dominantBaseline="middle"
                  fill="var(--ink-muted)"
                  fontSize={11}
                  style={{ fontVariantNumeric: 'tabular-nums' }}
                >
                  {group(t)}
                </text>
              </g>
            ))}

            <path d={geom.area} fill="var(--chart-wash)" />
            <path
              d={geom.line}
              fill="none"
              stroke="var(--chart-ink)"
              strokeWidth={2}
              strokeLinecap="round"
              strokeLinejoin="round"
            />

            {active && hover !== null && geom.coords[hover] && (
              <g>
                <line
                  x1={geom.coords[hover]!.x}
                  x2={geom.coords[hover]!.x}
                  y1={PAD.top}
                  y2={PAD.top + geom.plotH}
                  stroke="var(--line-strong)"
                  strokeWidth={1}
                  shapeRendering="crispEdges"
                />
                <circle
                  cx={geom.coords[hover]!.x}
                  cy={geom.coords[hover]!.y}
                  r={4}
                  fill="var(--chart-ink)"
                  stroke="var(--surface)"
                  strokeWidth={2}
                />
              </g>
            )}

            {lastCoord && hover === null && (
              <circle
                cx={lastCoord.x}
                cy={lastCoord.y}
                r={4}
                fill="var(--chart-ink)"
                stroke="var(--surface)"
                strokeWidth={2}
              />
            )}

            <text
              x={0}
              y={height - 5}
              fill="var(--ink-muted)"
              fontSize={11}
              style={{ fontVariantNumeric: 'tabular-nums' }}
            >
              {timeOfDay(points[0]!.t)}
            </text>
            <text
              x={width - PAD.right}
              y={height - 5}
              textAnchor="end"
              fill="var(--ink-muted)"
              fontSize={11}
              style={{ fontVariantNumeric: 'tabular-nums' }}
            >
              {timeOfDay(points[points.length - 1]!.t)}
            </text>
          </svg>
        )}

        {!geom && <div className="skeleton" style={{ height }} />}

        {active && hover !== null && geom?.coords[hover] && (
          <div
            className="tooltip"
            style={{
              left: `${geom.coords[hover]!.x}px`,
              top: `${geom.coords[hover]!.y - 10}px`,
            }}
          >
            <div className="tooltip__label">{timeOfDay(active.t)}</div>
            <div className="tooltip__value">
              {group(active.v)} {unit}
            </div>
          </div>
        )}
      </div>

      <details className="chart__table">
        <summary>
          Значения таблицей
          {lastPoint ? ` — сейчас ${group(lastPoint.v)} ${unit}` : ''}
        </summary>
        <div className="table-wrap" style={{ marginTop: 10, maxHeight: 220, overflowY: 'auto' }}>
          <table className="data">
            <caption className="visually-hidden">{title}</caption>
            <thead>
              <tr>
                <th scope="col">Время</th>
                <th scope="col" className="num">
                  {unit}
                </th>
              </tr>
            </thead>
            <tbody>
              {points.map((p) => (
                <tr key={p.t}>
                  <td>{timeOfDay(p.t)}</td>
                  <td className="num">{group(p.v)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>
    </figure>
  )
}

/** 1, 2, 5 × 10ⁿ — the steps people read without doing arithmetic. */
function niceStep(raw: number): number {
  const magnitude = 10 ** Math.floor(Math.log10(Math.abs(raw) || 1))
  const scaled = raw / magnitude
  const nice = scaled <= 1 ? 1 : scaled <= 2 ? 2 : scaled <= 5 ? 5 : 10
  return nice * magnitude
}

function round2(n: number): number {
  return Math.round(n * 100) / 100
}

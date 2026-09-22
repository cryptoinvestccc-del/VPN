import { useMemo, useState } from 'react'
import type { TimeRange, TimeSeriesPanel } from '../dashboard/types'
import { axisFor, stack } from './scale'
import { levelColor, seriesColor } from './colors'
import { metric, metricCompact } from '../lib/format'
import { useMeasure } from '../lib/useMeasure'

type Props = {
  panel: TimeSeriesPanel
  range: TimeRange
  height?: number
}

const PAD = { top: 10, right: 10, bottom: 20, left: 48 }

/**
 * The workhorse panel: one to four lines sharing a single y-axis.
 *
 * There is deliberately no second axis. Two measures of different scale
 * on one chart make the crossing points look meaningful when they are an
 * artefact of the scaling, and it is the single most common way a
 * dashboard misleads the person reading it. Measures that do not share a
 * scale get their own panel.
 *
 * Hovering shows every series at the hovered instant at once, because the
 * question an operator asks is almost never about one line — it is "what
 * else moved when this moved".
 */
export function TimeSeriesChart({ panel, range, height = 190 }: Props) {
  const [wrapRef, width] = useMeasure<HTMLDivElement>()
  const [hover, setHover] = useState<number | null>(null)

  const geom = useMemo(() => {
    const count = panel.series[0]?.points.length ?? 0
    if (width <= 0 || count < 2) return null

    const raw = panel.series.map((s) => s.points)
    const plotted = panel.stacked ? stack(raw) : raw
    const axis = axisFor(plotted.flat(), 4, panel.soft_max)

    const plotW = Math.max(1, width - PAD.left - PAD.right)
    const plotH = Math.max(1, height - PAD.top - PAD.bottom)
    const x = (i: number) => PAD.left + (i / (count - 1)) * plotW
    const y = (v: number) =>
      PAD.top + (1 - (v - axis.min) / (axis.max - axis.min || 1)) * plotH

    const baseY = y(axis.min)
    const lines = plotted.map((points, si) => {
      const path = points
        .map((v, i) => `${i === 0 ? 'M' : 'L'}${x(i).toFixed(2)} ${y(v).toFixed(2)}`)
        .join(' ')

      // A stacked band is filled down to the series below it, not to the
      // baseline; otherwise the lower bands are painted over twice and
      // the wash stops meaning "this much".
      const floor = panel.stacked && si > 0 ? plotted[si - 1]! : null
      let area: string
      if (floor) {
        const back: string[] = []
        for (let i = floor.length - 1; i >= 0; i--) {
          back.push(`L${x(i).toFixed(2)} ${y(floor[i]!).toFixed(2)}`)
        }
        area = `${path} ${back.join(' ')} Z`
      } else {
        area =
          `${path} L${x(points.length - 1).toFixed(2)} ${baseY.toFixed(2)}` +
          ` L${x(0).toFixed(2)} ${baseY.toFixed(2)} Z`
      }

      return { path, area, points }
    })

    // Roughly one label every 90px, snapped to whole points.
    const tickEvery = Math.max(1, Math.round(count / Math.max(2, Math.floor(plotW / 90))))
    const xTicks: number[] = []
    for (let i = 0; i < count; i += tickEvery) xTicks.push(i)

    return { axis, x, y, lines, plotW, plotH, count, xTicks }
  }, [panel, width, height])

  const times = useMemo(() => {
    const start = new Date(range.from).getTime()
    return Array.from({ length: range.points }, (_, i) => start + i * range.step_seconds * 1000)
  }, [range])

  function onMove(event: React.PointerEvent<HTMLDivElement>) {
    if (!geom) return
    const rect = event.currentTarget.getBoundingClientRect()
    const ratio = (event.clientX - rect.left - PAD.left) / geom.plotW
    setHover(Math.min(geom.count - 1, Math.max(0, Math.round(ratio * (geom.count - 1)))))
  }

  const hoveredValues =
    hover === null
      ? null
      : panel.series
          .map((s, i) => ({ series: s, value: s.points[hover] ?? 0, order: i }))
          .sort((a, b) => b.value - a.value)

  return (
    <div className="ts">
      <div
        ref={wrapRef}
        className="ts__plot"
        onPointerMove={onMove}
        onPointerLeave={() => setHover(null)}
      >
        {!geom && <div className="skeleton" style={{ height }} />}
        {geom && (
          <svg
            width={width}
            height={height}
            viewBox={`0 0 ${width} ${height}`}
            role="img"
            aria-label={`${panel.title}. ${panel.description} Значения перечислены в легенде под графиком.`}
            focusable="false"
          >
            {geom.axis.ticks.map((t) => (
              <g key={`y${t}`}>
                <line
                  x1={PAD.left}
                  x2={width - PAD.right}
                  y1={geom.y(t)}
                  y2={geom.y(t)}
                  stroke="var(--chart-grid)"
                  strokeWidth={1}
                  shapeRendering="crispEdges"
                />
                <text
                  x={PAD.left - 8}
                  y={geom.y(t)}
                  textAnchor="end"
                  dominantBaseline="middle"
                  fill="var(--ink-muted)"
                  fontSize={10.5}
                  style={{ fontVariantNumeric: 'tabular-nums' }}
                >
                  {metricCompact(t)}
                </text>
              </g>
            ))}

            {geom.xTicks.map((i) => (
              <g key={`x${i}`}>
                <line
                  x1={geom.x(i)}
                  x2={geom.x(i)}
                  y1={PAD.top}
                  y2={PAD.top + geom.plotH}
                  stroke="var(--chart-grid)"
                  strokeWidth={1}
                  shapeRendering="crispEdges"
                />
                <text
                  x={geom.x(i)}
                  y={height - 5}
                  textAnchor={edgeAnchor(geom.x(i), PAD.left, width - PAD.right)}
                  fill="var(--ink-muted)"
                  fontSize={10.5}
                  style={{ fontVariantNumeric: 'tabular-nums' }}
                >
                  {clockLabel(times[i], range)}
                </text>
              </g>
            ))}

            {panel.thresholds?.map((t) => (
              <line
                key={`th${t.from}`}
                x1={PAD.left}
                x2={width - PAD.right}
                y1={geom.y(t.from)}
                y2={geom.y(t.from)}
                stroke={levelColor(t.level)}
                strokeWidth={1}
                strokeDasharray="4 4"
                opacity={0.75}
              />
            ))}

            {/* Painted back to front so the first slot ends up on top. */}
            {[...geom.lines].reverse().map((line, revIndex) => {
              const i = geom.lines.length - 1 - revIndex
              const color = seriesColor(panel.series[i]!.color)
              return (
                <g key={panel.series[i]!.name}>
                  {panel.fill && <path d={line.area} fill={color} opacity={0.14} />}
                  <path
                    d={line.path}
                    fill="none"
                    stroke={color}
                    strokeWidth={2}
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  />
                </g>
              )
            })}

            {hover !== null && (
              <g>
                <line
                  x1={geom.x(hover)}
                  x2={geom.x(hover)}
                  y1={PAD.top}
                  y2={PAD.top + geom.plotH}
                  stroke="var(--chart-axis)"
                  strokeWidth={1}
                  shapeRendering="crispEdges"
                />
                {geom.lines.map((line, i) => (
                  <circle
                    key={`dot${i}`}
                    cx={geom.x(hover)}
                    cy={geom.y(line.points[hover] ?? 0)}
                    r={4}
                    fill={seriesColor(panel.series[i]!.color)}
                    stroke="var(--surface)"
                    strokeWidth={2}
                  />
                ))}
              </g>
            )}
          </svg>
        )}

        {hover !== null && geom && hoveredValues && (
          <div
            className="ts__tooltip"
            style={{
              left: `${geom.x(hover)}px`,
              // Flip to the other side near the right edge so the tooltip
              // never hangs off the panel.
              transform:
                geom.x(hover) > width * 0.6 ? 'translate(-100%, 0) translateX(-12px)' : 'translateX(12px)',
            }}
          >
            <div className="ts__tooltip-time">{fullClock(times[hover])}</div>
            {hoveredValues.map(({ series, value }) => (
              <div className="ts__tooltip-row" key={series.name}>
                <span className="ts__key" style={{ background: seriesColor(series.color) }} />
                <span className="ts__tooltip-name">{series.name}</span>
                <span className="ts__tooltip-value">
                  {metric(value)} {panel.unit}
                </span>
              </div>
            ))}
          </div>
        )}
      </div>

      <Legend panel={panel} />
    </div>
  )
}

/**
 * The legend doubles as the summary table. It is always present: for two
 * or more series, colour alone is never the only way to tell them apart,
 * and the min/max/mean/last columns answer most questions without the
 * reader having to hover anything at all.
 */
function Legend({ panel }: { panel: TimeSeriesPanel }) {
  return (
    <div className="ts__legend">
      <table>
        <thead>
          <tr>
            <th scope="col">Серия</th>
            <th scope="col" className="num">
              Мин
            </th>
            <th scope="col" className="num">
              Макс
            </th>
            <th scope="col" className="num">
              Сред
            </th>
            <th scope="col" className="num">
              Посл
            </th>
          </tr>
        </thead>
        <tbody>
          {panel.series.map((s) => {
            const points = s.points
            const min = Math.min(...points)
            const max = Math.max(...points)
            const mean = points.reduce((a, b) => a + b, 0) / (points.length || 1)
            return (
              <tr key={s.name}>
                <th scope="row">
                  <span className="ts__key" style={{ background: seriesColor(s.color) }} />
                  {s.name}
                </th>
                <td className="num">{metric(min)}</td>
                <td className="num">{metric(max)}</td>
                <td className="num">{metric(mean)}</td>
                <td className="num">{metric(points[points.length - 1] ?? 0)}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

/** Keeps the first and last time labels inside the plot instead of
    letting them hang off the panel. */
function edgeAnchor(x: number, left: number, right: number): 'start' | 'middle' | 'end' {
  if (x - left < 22) return 'start'
  if (right - x < 22) return 'end'
  return 'middle'
}

function clockLabel(ms: number | undefined, range: TimeRange): string {
  if (ms === undefined) return ''
  const d = new Date(ms)
  // Past a day, the hour alone is ambiguous; the date is what the reader
  // needs to place the point.
  if (range.step_seconds >= 3600) {
    return `${pad(d.getDate())}.${pad(d.getMonth() + 1)} ${pad(d.getHours())}:00`
  }
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function fullClock(ms: number | undefined): string {
  if (ms === undefined) return ''
  const d = new Date(ms)
  return `${pad(d.getDate())}.${pad(d.getMonth() + 1)} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function pad(n: number): string {
  return String(n).padStart(2, '0')
}

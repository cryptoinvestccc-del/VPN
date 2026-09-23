import type { NodeRow } from '../dashboard/types'
import { levelColor, levelFor } from './colors'
import { metric } from '../lib/format'

const statusLabel: Record<NodeRow['status'], string> = {
  online: 'онлайн',
  degraded: 'нагружен',
  maintenance: 'обслуживание',
}

const loadThresholds = [
  { from: 0, level: 'ok' as const },
  { from: 70, level: 'warn' as const },
  { from: 88, level: 'crit' as const },
]

/**
 * More than about seven things that all carry meaning is a table, not
 * more colours. Nine columns across six nodes is exactly that case.
 */
export function NodeTable({ rows }: { rows: NodeRow[] }) {
  // The table scrolls sideways on a narrow screen, so it has to be a
  // region a keyboard can reach and a screen reader can name — the same
  // treatment the product page's tables get.
  return (
    <div className="node-table" tabIndex={0} role="region" aria-label="Состояние узлов сети">
      <table className="data">
        <caption className="visually-hidden">Состояние узлов сети</caption>
        <thead>
          <tr>
            <th scope="col">Узел</th>
            <th scope="col">Режим</th>
            <th scope="col">Состояние</th>
            <th scope="col" className="num">CPU</th>
            <th scope="col" className="num">Память</th>
            <th scope="col" className="num">Load</th>
            <th scope="col" className="num">RTT</th>
            <th scope="col" className="num">Сессии</th>
            <th scope="col" className="num">Мбит/с</th>
            <th scope="col">CPU за окно</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => {
            const level = levelFor(row.cpu_pct, loadThresholds)
            return (
              <tr key={row.id}>
                <th scope="row">
                  <span className="node-table__name">{row.node}</span>
                  <span className="node-table__country">{row.country}</span>
                </th>
                <td>{row.mode}</td>
                <td>
                  <span className={`status status--${row.status}`}>
                    <span className="dot" />
                    {statusLabel[row.status]}
                  </span>
                </td>
                <td className="num">
                  <span className="cell-bar">
                    <span
                      className="cell-bar__fill"
                      style={{
                        width: `${Math.min(100, row.cpu_pct)}%`,
                        background: level ? levelColor(level) : 'var(--series-2)',
                      }}
                    />
                  </span>
                  {metric(row.cpu_pct, 0)}%
                </td>
                <td className="num">{metric(row.mem_pct, 0)}%</td>
                <td className="num">{metric(row.load, 2)}</td>
                <td className="num">{metric(row.rtt_ms, 0)} мс</td>
                <td className="num">{metric(row.sessions, 0)}</td>
                <td className="num">{metric(row.throughput_mbps, 0)}</td>
                <td>
                  <RowSpark points={row.spark} />
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

function RowSpark({ points }: { points: number[] }) {
  if (points.length < 2) return null
  const w = 88
  const h = 20
  const min = Math.min(...points)
  const max = Math.max(...points)
  const span = max - min || 1
  const step = w / (points.length - 1)

  const d = points
    .map((v, i) => `${i === 0 ? 'M' : 'L'}${(i * step).toFixed(1)} ${(h - ((v - min) / span) * (h - 4) - 2).toFixed(1)}`)
    .join(' ')

  return (
    <svg width={w} height={h} viewBox={`0 0 ${w} ${h}`} aria-hidden="true" focusable="false">
      <path d={d} fill="none" stroke="var(--chart-deemphasis)" strokeWidth={1.5} strokeLinejoin="round" />
    </svg>
  )
}

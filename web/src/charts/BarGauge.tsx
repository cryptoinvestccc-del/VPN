import type { BarGaugePanel } from '../dashboard/types'
import { levelColor, levelFor, levelLabel } from './colors'
import { metric } from '../lib/format'

/**
 * Bars, because the question is "which node is hot right now" and not
 * "how did it get there" — that one is answered by the time series above.
 *
 * Every bar shares one scale and grows from one baseline, so their
 * lengths are comparable by eye; the value sits at the tip rather than
 * inside, where a short bar would clip it.
 */
export function BarGauge({ panel }: { panel: BarGaugePanel }) {
  return (
    <div className="bars">
      {panel.rows.map((row) => {
        const level = levelFor(row.value, panel.thresholds)
        const pct = Math.min(100, Math.max(0, ((row.value - panel.min) / (panel.max - panel.min || 1)) * 100))
        return (
          <div className="bars__row" key={row.label}>
            <span className="bars__label">{row.label}</span>
            <span className="bars__track">
              <span
                className="bars__fill"
                style={{ width: `${pct}%`, background: level ? levelColor(level) : 'var(--series-2)' }}
              />
            </span>
            <span className="bars__value">
              {metric(row.value, 0)}
              <span className="bars__unit">{panel.unit}</span>
            </span>
            <span className="visually-hidden">{level ? levelLabel[level] : ''}</span>
          </div>
        )
      })}
    </div>
  )
}

import type { Tile } from '../lib/types'
import { compact, signed } from '../lib/format'
import { Sparkline } from './Sparkline'

/**
 * Label, value, delta, trend — in that order, per the stat-tile contract.
 * The delta's colour is direction × whether up is good news, which is why
 * a fall in rejected probes reads green and a fall in sessions would not.
 */
export function StatTile({ tile }: { tile: Tile }) {
  const delta = tile.delta
  const tone =
    delta === undefined || delta === 0
      ? 'flat'
      : delta > 0 === tile.good_when_up
        ? 'good'
        : 'bad'

  return (
    <div className="card stat">
      <div className="stat__top">
        <div>
          <p className="card__label">{tile.label}</p>
          <div className="stat__value">
            {compact(tile.value)}
            {tile.unit && (
              <span className={`stat__unit ${tile.unit === '%' ? 'stat__unit--tight' : ''}`}>
                {tile.unit}
              </span>
            )}
          </div>
        </div>
        <div className="stat__spark">
          <Sparkline series={tile.series} label={`${tile.label}: динамика за последние точки`} />
        </div>
      </div>
      <div className="stat__foot">
        {delta !== undefined && (
          <span className={`delta delta--${tone}`}>
            <Arrow direction={delta === 0 ? 'flat' : delta > 0 ? 'up' : 'down'} />
            {signed(delta, 2).replace(/,00$/, '')}%
          </span>
        )}
        <span>{tile.delta_window ?? tile.note}</span>
      </div>
    </div>
  )
}

function Arrow({ direction }: { direction: 'up' | 'down' | 'flat' }) {
  if (direction === 'flat') {
    return (
      <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
        <path d="M2 5h6" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
      </svg>
    )
  }
  const d = direction === 'up' ? 'M5 8V2m0 0L2.4 4.6M5 2l2.6 2.6' : 'M5 2v6m0 0l2.6-2.6M5 8L2.4 5.4'
  return (
    <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
      <path d={d} stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" fill="none" />
    </svg>
  )
}

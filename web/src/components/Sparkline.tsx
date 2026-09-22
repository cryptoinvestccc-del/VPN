type Props = {
  series: number[]
  width?: number
  height?: number
  /** Accessible summary; the tile's label and value carry the detail. */
  label: string
}

/**
 * One series, no axes, no labels — the trend behind a stat tile's number.
 * The line is de-emphasised and only the current point wears full ink, so
 * a row of four tiles reads as four numbers, not four charts.
 */
export function Sparkline({ series, width = 84, height = 28, label }: Props) {
  if (series.length < 2) return null

  const pad = 4
  const min = Math.min(...series)
  const max = Math.max(...series)
  const span = max - min || 1
  const stepX = (width - pad * 2) / (series.length - 1)

  const points = series.map((v, i) => ({
    x: pad + i * stepX,
    y: pad + (1 - (v - min) / span) * (height - pad * 2),
  }))

  const d = points.map((p, i) => `${i === 0 ? 'M' : 'L'}${p.x.toFixed(2)} ${p.y.toFixed(2)}`).join(' ')
  const last = points[points.length - 1]!

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      role="img"
      aria-label={label}
      focusable="false"
    >
      <path
        d={d}
        fill="none"
        stroke="var(--chart-deemphasis)"
        strokeWidth={2}
        strokeLinecap="round"
        strokeLinejoin="round"
        vectorEffect="non-scaling-stroke"
      />
      <circle
        cx={last.x}
        cy={last.y}
        r={4}
        fill="var(--chart-ink)"
        stroke="var(--surface)"
        strokeWidth={2}
      />
    </svg>
  )
}

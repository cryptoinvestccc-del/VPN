/** Axis maths shared by every panel, so they agree on what a tick is. */

/** 1, 2, 5 × 10ⁿ — the steps people read without doing arithmetic. */
export function niceStep(raw: number): number {
  if (!Number.isFinite(raw) || raw <= 0) return 1
  const magnitude = 10 ** Math.floor(Math.log10(raw))
  const scaled = raw / magnitude
  const nice = scaled <= 1 ? 1 : scaled <= 2 ? 2 : scaled <= 5 ? 5 : 10
  return nice * magnitude
}

export type Axis = {
  min: number
  max: number
  ticks: number[]
}

/**
 * Builds a y-axis over `values`, rounded out to clean ticks.
 *
 * The floor is zero whenever the data is non-negative. A chart that
 * starts its axis at the lowest observed value turns a 2% wobble into a
 * cliff, which is the most common way a dashboard lies without anyone
 * writing a false number.
 */
export function axisFor(values: number[], target = 4, softMax?: number): Axis {
  const finite = values.filter((v) => Number.isFinite(v))
  if (finite.length === 0) return { min: 0, max: 1, ticks: [0, 1] }

  const rawMin = Math.min(...finite)
  const rawMax = Math.max(softMax ?? -Infinity, ...finite)
  const zeroBased = rawMin >= 0
  const lo = zeroBased ? 0 : rawMin
  const span = rawMax - lo || Math.abs(rawMax) || 1

  const step = niceStep(span / target)
  const min = Math.floor(lo / step) * step
  const max = Math.ceil(rawMax / step) * step || step

  const ticks: number[] = []
  for (let v = min; v <= max + step / 2; v += step) ticks.push(roundTo(v, 6))
  return { min, max, ticks }
}

export function roundTo(v: number, digits: number): number {
  const f = 10 ** digits
  return Math.round(v * f) / f
}

/** Turns a series list into the cumulative bands a stacked area needs. */
export function stack(series: number[][]): number[][] {
  // Typed explicitly, and sized by the longest series rather than the
  // first. new Array(n).fill(0) is any[], so every total read back out
  // of it left the type system behind — and with it went the case where
  // a later series is longer than the first: those points indexed past
  // the running totals and added to undefined, turning the rest of the
  // band into NaN, which draws as nothing at all.
  const width = series.reduce((w, points) => Math.max(w, points.length), 0)
  const running: number[] = new Array<number>(width).fill(0)

  return series.map((points) =>
    points.map((v, i) => {
      const total = (running[i] ?? 0) + v
      running[i] = total
      return total
    }),
  )
}

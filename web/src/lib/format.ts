const nbsp = ' '

/**
 * 1284 -> "1 284", 12900 -> "12,9K", 4200000 -> "4,2M".
 *
 * Only large numbers are abbreviated. A figure like 99,94% has to keep
 * both decimals: rounding an availability number to one place turns
 * three nines into two and quietly overstates an outage.
 */
export function compact(n: number): string {
  if (Math.abs(n) >= 1_000_000) return `${trim(n / 1_000_000)}M`
  if (Math.abs(n) >= 10_000) return `${trim(n / 1000)}K`
  return group(n)
}

/** Thin-space grouping and a decimal comma, the Russian convention. */
export function group(n: number): string {
  const [int, frac] = String(round(n, 2)).split('.')
  const spaced = int!.replace(/\B(?=(\d{3})+(?!\d))/g, nbsp)
  return frac ? `${spaced},${frac}` : spaced
}

export function round(n: number, digits = 0): number {
  const f = 10 ** digits
  return Math.round(n * f) / f
}

function trim(n: number): string {
  return String(round(n, 1)).replace('.', ',')
}

export function signed(n: number, digits = 0): string {
  const v = round(n, digits)
  const body = String(Math.abs(v)).replace('.', ',')
  if (v > 0) return `+${body}`
  // A minus sign, not a hyphen: the hyphen sits too high beside digits.
  if (v < 0) return `−${body}`
  return body
}

export function money(amount: number, currency: string): string {
  return `${group(amount)}${nbsp}${currency}`
}

export function timeOfDay(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function pad(n: number): string {
  return String(n).padStart(2, '0')
}

import type { Level, Series } from '../dashboard/types'

/**
 * The fixed categorical order. A series names its slot and the page
 * resolves it here, so the same node is the same colour on every panel
 * and a panel that drops a series cannot repaint the survivors.
 */
const seriesVar: Record<Series['color'], string> = {
  green: 'var(--series-1)',
  blue: 'var(--series-2)',
  orange: 'var(--series-3)',
  purple: 'var(--series-4)',
}

export function seriesColor(slot: Series['color']): string {
  return seriesVar[slot] ?? 'var(--series-1)'
}

const levelVar: Record<Level, string> = {
  ok: 'var(--level-ok)',
  warn: 'var(--level-warn)',
  crit: 'var(--level-crit)',
}

export function levelColor(level: Level): string {
  return levelVar[level] ?? levelVar.ok
}

export const levelLabel: Record<Level, string> = {
  ok: 'норма',
  warn: 'внимание',
  crit: 'критично',
}

/**
 * The level a value falls into, given thresholds sorted by their floor.
 * Returns null when a panel declares none, so "no thresholds" stays
 * distinct from "below the first one".
 */
export function levelFor(value: number, thresholds?: { from: number; level: Level }[]): Level | null {
  if (!thresholds || thresholds.length === 0) return null
  let current: Level | null = null
  for (const t of [...thresholds].sort((a, b) => a.from - b.from)) {
    if (value >= t.from) current = t.level
  }
  return current
}

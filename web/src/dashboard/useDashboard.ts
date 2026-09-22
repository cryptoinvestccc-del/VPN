import { useCallback, useEffect, useRef, useState } from 'react'
import type { DashboardData } from './types'

export type DashboardState = {
  data: DashboardData | null
  /**
   * The window `data` was requested for, which is not always the window
   * the server answered with: an unknown one falls back to the default.
   * A caller that wants to correct itself has to compare the two, and
   * comparing against the window currently selected would instead
   * compare against a request still in flight.
   */
  requested: string | null
  loading: boolean
  error: string | null
  lastUpdated: Date | null
}

/**
 * Fetches the dashboard for a window, and re-fetches on a timer.
 *
 * Two details matter for a page that refreshes itself. A refresh keeps
 * the panels that are already on screen rather than blanking them — a
 * dashboard that flashes empty every thirty seconds is unusable. And a
 * failed refresh leaves the last good data in place and says so in the
 * bar, because stale-and-labelled beats blank.
 */
export function useDashboard(rangeID: string, refreshSeconds: number): DashboardState & { refresh: () => void } {
  const [state, setState] = useState<DashboardState>({
    data: null,
    requested: null,
    loading: true,
    error: null,
    lastUpdated: null,
  })
  const [tick, setTick] = useState(0)
  const inFlight = useRef<AbortController | null>(null)

  const refresh = useCallback(() => setTick((t) => t + 1), [])

  useEffect(() => {
    let unmounted = false
    inFlight.current?.abort()
    const controller = new AbortController()
    inFlight.current = controller
    const timer = setTimeout(() => controller.abort(), 10_000)

    setState((prev) => ({ ...prev, loading: true }))

    fetch(`/api/v1/dashboard?range=${encodeURIComponent(rangeID)}`, {
      signal: controller.signal,
      headers: { accept: 'application/json' },
    })
      .then(async (res) => {
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
        return (await res.json()) as DashboardData
      })
      .then((data) => {
        if (unmounted) return
        setState({ data, requested: rangeID, loading: false, error: null, lastUpdated: new Date() })
      })
      .catch((err: unknown) => {
        if (unmounted) return
        const reason =
          err instanceof Error && err.name === 'AbortError'
            ? 'API не ответил за 10 с'
            : err instanceof Error
              ? err.message
              : 'нет связи с API'
        setState((prev) => ({ ...prev, loading: false, error: reason }))
      })
      .finally(() => clearTimeout(timer))

    return () => {
      unmounted = true
      clearTimeout(timer)
      controller.abort()
    }
  }, [rangeID, tick])

  useEffect(() => {
    if (refreshSeconds <= 0) return
    const id = setInterval(refresh, refreshSeconds * 1000)
    return () => clearInterval(id)
  }, [refreshSeconds, refresh])

  return { ...state, refresh }
}

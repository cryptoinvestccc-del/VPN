import { useCallback, useEffect, useRef, useState } from 'react'
import type { DashboardData } from './types'

export type DashboardState = {
  data: DashboardData | null
  loading: boolean
  error: string | null
  lastUpdated: Date | null
}

// settled names the request whose outcome the state below already
// reflects. Loading is then that name not matching the request the
// component wants, which is a thing to derive rather than a second
// piece of state an effect has to remember to set.
type Settled = {
  data: DashboardData | null
  error: string | null
  lastUpdated: Date | null
  request: string | null
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
  const [settled, setSettled] = useState<Settled>({
    data: null,
    error: null,
    lastUpdated: null,
    request: null,
  })
  const [tick, setTick] = useState(0)
  const inFlight = useRef<AbortController | null>(null)

  const request = `${rangeID}#${tick}`
  const refresh = useCallback(() => setTick((t) => t + 1), [])

  useEffect(() => {
    let unmounted = false
    inFlight.current?.abort()
    const controller = new AbortController()
    inFlight.current = controller
    const timer = setTimeout(() => controller.abort(), 10_000)

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
        setSettled({ data, error: null, lastUpdated: new Date(), request })
      })
      .catch((err: unknown) => {
        if (unmounted) return
        const reason =
          err instanceof Error && err.name === 'AbortError'
            ? 'API не ответил за 10 с'
            : err instanceof Error
              ? err.message
              : 'нет связи с API'
        setSettled((prev) => ({ ...prev, error: reason, request }))
      })
      .finally(() => clearTimeout(timer))

    return () => {
      unmounted = true
      clearTimeout(timer)
      controller.abort()
    }
  }, [rangeID, tick, request])

  useEffect(() => {
    if (refreshSeconds <= 0) return
    const id = setInterval(refresh, refreshSeconds * 1000)
    return () => clearInterval(id)
  }, [refreshSeconds, refresh])

  return {
    data: settled.data,
    error: settled.error,
    lastUpdated: settled.lastUpdated,
    loading: settled.request !== request,
    refresh,
  }
}

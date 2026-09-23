import { useEffect, useState } from 'react'

export type Resource<T> = {
  data: T
  /** "fallback" until the API answers; "live" once it has. */
  source: 'fallback' | 'live'
  error: string | null
}

/**
 * Fetches `path`, holding `fallback` until it answers.
 *
 * The page never blocks on the network: it renders the snapshot first and
 * swaps in the live figures when they arrive. A failed request leaves the
 * snapshot in place and records the reason, which the console panel shows
 * rather than hiding.
 */
export function useResource<T>(path: string, fallback: T): Resource<T> {
  const [state, setState] = useState<Resource<T>>({
    data: fallback,
    source: 'fallback',
    error: null,
  })

  useEffect(() => {
    // `unmounted` and the abort are separate on purpose: aborting on
    // unmount and aborting on timeout both raise AbortError, and only the
    // timeout is worth reporting.
    let unmounted = false
    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), 6000)

    fetch(path, { signal: controller.signal, headers: { accept: 'application/json' } })
      .then(async (res) => {
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
        return (await res.json()) as T
      })
      .then((data) => {
        if (!unmounted) setState({ data, source: 'live', error: null })
      })
      .catch((err: unknown) => {
        if (unmounted) return
        const reason =
          err instanceof Error && err.name === 'AbortError'
            ? 'API не ответил за 6 с'
            : err instanceof Error
              ? err.message
              : 'нет связи с API'
        setState((prev) => ({ ...prev, error: reason }))
      })
      .finally(() => clearTimeout(timer))

    return () => {
      unmounted = true
      clearTimeout(timer)
      controller.abort()
    }
  }, [path])

  return state
}

/**
 * Like `useResource`, but asks again every `intervalMs` for as long as the
 * page is visible.
 *
 * A hidden tab stops polling: a status card nobody is looking at does not
 * need a request a second, and on a phone that is battery. Requests never
 * overlap — the next one is scheduled when the previous one settles — so a
 * slow network degrades to a slower refresh rather than a pile-up.
 */
export function usePolling<T>(path: string, fallback: T, intervalMs: number): Resource<T> {
  const [state, setState] = useState<Resource<T>>({
    data: fallback,
    source: 'fallback',
    error: null,
  })

  useEffect(() => {
    let stopped = false
    let timer: ReturnType<typeof setTimeout> | undefined
    let controller: AbortController | undefined

    const tick = () => {
      if (stopped) return
      if (document.visibilityState === 'hidden') {
        timer = setTimeout(tick, intervalMs)
        return
      }
      controller = new AbortController()
      const abortTimer = setTimeout(() => controller?.abort(), Math.max(intervalMs * 3, 3000))
      fetch(path, { signal: controller.signal, headers: { accept: 'application/json' } })
        .then(async (res) => {
          if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
          return (await res.json()) as T
        })
        .then((data) => {
          if (!stopped) setState({ data, source: 'live', error: null })
        })
        .catch((err: unknown) => {
          if (stopped) return
          const reason = err instanceof Error ? err.message : 'нет связи с API'
          setState((prev) => ({ ...prev, error: reason }))
        })
        .finally(() => {
          clearTimeout(abortTimer)
          if (!stopped) timer = setTimeout(tick, intervalMs)
        })
    }

    tick()
    return () => {
      stopped = true
      clearTimeout(timer)
      controller?.abort()
    }
  }, [path, intervalMs])

  return state
}

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

import { useCallback, useEffect, useState } from 'react'

export type Theme = 'light' | 'dark'

const KEY = 'besy.theme'

/**
 * Theme, with the system preference as the default.
 *
 * The default lives in CSS (a prefers-color-scheme media query), not
 * here, so the first paint is already the right theme. This hook only
 * handles the explicit override, which is why it starts as null rather
 * than guessing: null means "whatever the system says".
 */
export function useTheme(): [Theme, (next: Theme) => void] {
  const [override, setOverride] = useState<Theme | null>(() => read())
  const [system, setSystem] = useState<Theme>(() =>
    window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light',
  )

  useEffect(() => {
    const mq = window.matchMedia?.('(prefers-color-scheme: dark)')
    if (!mq) return
    const onChange = (e: MediaQueryListEvent) => setSystem(e.matches ? 'dark' : 'light')
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [])

  useEffect(() => {
    if (override) document.documentElement.setAttribute('data-theme', override)
    else document.documentElement.removeAttribute('data-theme')
  }, [override])

  const set = useCallback((next: Theme) => {
    setOverride(next)
    try {
      localStorage.setItem(KEY, next)
    } catch {
      // Private browsing, blocked storage: the theme still applies for
      // this visit, it just will not be remembered.
    }
  }, [])

  return [override ?? system, set]
}

function read(): Theme | null {
  try {
    const stored = localStorage.getItem(KEY)
    return stored === 'light' || stored === 'dark' ? stored : null
  } catch {
    return null
  }
}

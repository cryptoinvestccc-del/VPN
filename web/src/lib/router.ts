import { useEffect, useState } from 'react'

/**
 * Two pages, so two-page routing: the product page at / and the
 * operations dashboard at /dashboard.
 *
 * A router library would be a dependency for one branch. The server
 * already serves index.html for unknown paths, so a deep link and a
 * reload both land on the right page.
 */
export type Route = 'landing' | 'dashboard'

export function routeOf(pathname: string): Route {
  return pathname.replace(/\/+$/, '') === '/dashboard' ? 'dashboard' : 'landing'
}

export function useRoute(): Route {
  const [route, setRoute] = useState<Route>(() => routeOf(window.location.pathname))

  useEffect(() => {
    const onPop = () => setRoute(routeOf(window.location.pathname))
    window.addEventListener('popstate', onPop)
    return () => window.removeEventListener('popstate', onPop)
  }, [])

  return route
}

/** Navigates without a reload, and tells the app to re-render. */
export function navigate(to: string) {
  if (window.location.pathname === to) return
  window.history.pushState({}, '', to)
  window.dispatchEvent(new PopStateEvent('popstate'))
}

/** Click handler for an internal link that should not reload the page. */
export function linkHandler(to: string) {
  return (event: React.MouseEvent<HTMLAnchorElement>) => {
    // Leave modified clicks alone: ctrl/cmd-click means "new tab", and
    // hijacking it is the kind of helpfulness people hate.
    if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) {
      return
    }
    event.preventDefault()
    navigate(to)
  }
}

/**
 * Writes a query parameter without adding a history entry.
 *
 * The window a dashboard is showing is part of what the page *is*, so it
 * belongs in the address bar: a reload keeps it, and a link to "the last
 * 24 hours" is a link someone can send. replaceState rather than
 * pushState, because a back button that walks through every window the
 * reader tried is not what back is for.
 */
export function setQueryParam(key: string, value: string) {
  const url = new URL(window.location.href)
  if (url.searchParams.get(key) === value) return
  url.searchParams.set(key, value)
  window.history.replaceState(window.history.state, '', url.toString())
}

/** Reads a query parameter, or null when it is absent. */
export function queryParam(key: string): string | null {
  return new URLSearchParams(window.location.search).get(key)
}

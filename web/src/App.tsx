import { useEffect } from 'react'
import { useRoute } from './lib/router'
import { Landing } from './landing/Landing'
import { Dashboard } from './dashboard/Dashboard'

const titles = {
  dashboard: 'Обзор сети — Besy VPN',
  about: 'Besy VPN — обфускация WireGuard против DPI',
} as const

export function App() {
  const route = useRoute()

  // One document, two pages: the title has to follow the route, or a
  // bookmark and a browser-history entry both say the wrong thing.
  useEffect(() => {
    document.title = titles[route]
  }, [route])

  return (
    <>
      <a className="skip-link" href="#main">
        К основному содержанию
      </a>
      {route === 'about' ? <Landing /> : <Dashboard />}
    </>
  )
}

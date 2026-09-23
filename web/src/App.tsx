import { useEffect } from 'react'
import { useRoute } from './lib/router'
import { Landing } from './landing/Landing'
import { Dashboard } from './dashboard/Dashboard'

const titles = {
  landing: 'BESY VPN — интернет без блокировок за 100 ₽ в месяц',
  dashboard: 'Обзор сети — BESY VPN',
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
      {route === 'dashboard' ? <Dashboard /> : <Landing />}
    </>
  )
}

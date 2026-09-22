import { Logo } from '../components/Logo'
import { linkHandler } from '../lib/router'

export function Footer() {
  return (
    <footer className="site-footer">
      <div className="shell site-footer__bar">
        <a className="brand" href="#top">
          <Logo size={18} />
          BESY
        </a>
        <span>Обфускатор WireGuard-трафика. Открытый код, никакой телеметрии.</span>
        <a href="/dashboard" onClick={linkHandler('/dashboard')}>
          Состояние сети →
        </a>
      </div>
    </footer>
  )
}

import { useEffect, useState } from 'react'
import { Logo } from '../components/Logo'
import { linkHandler } from '../lib/router'

const links = [
  { href: '#how', label: 'Как это работает' },
  { href: '#features', label: 'Возможности' },
  { href: '#nodes', label: 'Узлы' },
  { href: '#platforms', label: 'Платформы' },
  { href: '#pricing', label: 'Тарифы' },
]

export function Header() {
  const [stuck, setStuck] = useState(false)
  const [open, setOpen] = useState(false)

  useEffect(() => {
    const onScroll = () => setStuck(window.scrollY > 8)
    onScroll()
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => window.removeEventListener('scroll', onScroll)
  }, [])

  return (
    <header className="site-header" data-stuck={stuck}>
      <div className="shell site-header__bar">
        <a className="brand" href="#top" aria-label="Besy VPN, на главную">
          <Logo />
          BESY
        </a>

        <nav className="site-nav" aria-label="Разделы">
          {links.map((l) => (
            <a key={l.href} href={l.href}>
              {l.label}
            </a>
          ))}
        </nav>

        <div className="site-header__actions">
          <a className="btn btn--ghost" href="/" onClick={linkHandler('/')}>
            Дашборд
          </a>
          <a className="btn btn--primary" href="#pricing">
            Подключиться
          </a>
          <button
            type="button"
            className="nav-toggle"
            aria-expanded={open}
            aria-controls="nav-panel"
            aria-label={open ? 'Закрыть меню' : 'Открыть меню'}
            onClick={() => setOpen((v) => !v)}
          >
            <svg width="16" height="12" viewBox="0 0 16 12" aria-hidden="true">
              {open ? (
                <path d="m2 2 12 8M14 2 2 10" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
              ) : (
                <path d="M1 1.5h14M1 10.5h14M1 6h14" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
              )}
            </svg>
          </button>
        </div>
      </div>

      {open && (
        <div className="nav-panel" id="nav-panel">
          <div className="shell">
            {links.map((l) => (
              <a key={l.href} href={l.href} onClick={() => setOpen(false)}>
                {l.label}
              </a>
            ))}
            <a href="/" onClick={linkHandler('/')}>
              Дашборд
            </a>
          </div>
        </div>
      )}
    </header>
  )
}

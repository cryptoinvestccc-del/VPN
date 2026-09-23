import { useEffect, useRef, useState } from 'react'
import { Logo } from '../components/Logo'
import { ThemeToggle } from '../components/ThemeToggle'
import { links as urls } from '../lib/links'

const links = [
  { href: '#pricing', label: 'Тарифы' },
  { href: '#features', label: 'Что вы получаете' },
  { href: '#platforms', label: 'Устройства' },
  { href: '#limits', label: 'Условия' },
]

export function Header() {
  const [stuck, setStuck] = useState(false)
  const [open, setOpen] = useState(false)
  const headerRef = useRef<HTMLElement>(null)
  const toggleRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    const onScroll = () => setStuck(window.scrollY > 8)
    onScroll()
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => window.removeEventListener('scroll', onScroll)
  }, [])

  // An open panel has to close the two ways every open panel closes, or
  // it is a trap: Escape, and a click on anything behind it. The
  // dashboard's dropdown has done this from the start; this one had
  // neither, so on a phone the only way out was the toggle itself.
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      setOpen(false)
      toggleRef.current?.focus()
    }
    const onDocClick = (e: MouseEvent) => {
      if (!headerRef.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('keydown', onKey)
    document.addEventListener('mousedown', onDocClick)
    return () => {
      document.removeEventListener('keydown', onKey)
      document.removeEventListener('mousedown', onDocClick)
    }
  }, [open])

  return (
    <header className="site-header" data-stuck={stuck} ref={headerRef}>
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
          <ThemeToggle className="round-btn" />
          <a className="btn btn--primary" href={urls.telegram} target="_blank" rel="noopener">
            Подключиться
          </a>
          <button
            type="button"
            ref={toggleRef}
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
            <a href={urls.telegram} target="_blank" rel="noopener" onClick={() => setOpen(false)}>
              Подключиться
            </a>
          </div>
        </div>
      )}
    </header>
  )
}

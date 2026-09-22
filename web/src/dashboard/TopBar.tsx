import { useEffect, useId, useRef, useState } from 'react'
import type { DashboardData } from './types'
import { Logo } from '../components/Logo'
import { ThemeToggle } from '../components/ThemeToggle'
import { linkHandler } from '../lib/router'

type Props = {
  data: DashboardData
  rangeID: string
  onRange: (id: string) => void
  refreshSeconds: number
  onRefreshSeconds: (seconds: number) => void
  onRefreshNow: () => void
  loading: boolean
  lastUpdated: Date | null
}

const refreshOptions = [
  { seconds: 0, label: 'выкл' },
  { seconds: 10, label: '10 с' },
  { seconds: 30, label: '30 с' },
  { seconds: 60, label: '1 мин' },
  { seconds: 300, label: '5 мин' },
]

export function TopBar({
  data,
  rangeID,
  onRange,
  refreshSeconds,
  onRefreshSeconds,
  onRefreshNow,
  loading,
  lastUpdated,
}: Props) {
  return (
    <header className="topbar">
      <div className="topbar__row">
        <a className="topbar__brand" href="/" onClick={linkHandler('/')} title="На страницу продукта">
          <Logo size={18} />
          BESY
        </a>
        <nav className="topbar__crumbs" aria-label="Хлебные крошки">
          <span>Дашборды</span>
          <Chevron />
          <span>Besy VPN</span>
          <Chevron />
          <strong>Обзор сети</strong>
        </nav>

        <div className="topbar__spacer" />

        <Dropdown
          name="Период"
          label={data.range.label}
          icon={<ClockIcon />}
          value={rangeID}
          options={data.range_options.map((o) => ({ value: o.id, label: o.label }))}
          onChange={onRange}
        />

        <Dropdown
          name="Интервал обновления"
          label={refreshOptions.find((o) => o.seconds === refreshSeconds)?.label ?? 'выкл'}
          icon={<RefreshIcon spinning={loading} />}
          value={String(refreshSeconds)}
          options={refreshOptions.map((o) => ({ value: String(o.seconds), label: o.label }))}
          onChange={(v) => onRefreshSeconds(Number(v))}
          before={
            <button type="button" className="icon-btn" onClick={onRefreshNow} aria-label="Обновить сейчас">
              <RefreshIcon spinning={loading} />
            </button>
          }
        />

        <ThemeToggle />
      </div>

      <div className="topbar__row topbar__row--vars">
        <Variable label="Источник" value="besy-metrics (образец)" />
        <Variable label="Окружение" value="prod" />
        <Variable label="Узел" value="все" />
        <div className="topbar__spacer" />
        <span className="topbar__stamp">
          {data.mock && <span className="badge badge--muted">демонстрационные данные</span>}
          {lastUpdated && <span>обновлено в {clock(lastUpdated)}</span>}
        </span>
      </div>
    </header>
  )
}

function Variable({ label, value }: { label: string; value: string }) {
  // These are display-only: the dashboard has one sample fleet behind it,
  // and a picker that cannot change anything would be a lie about what
  // the page can do.
  return (
    <span className="variable" title="В этой версии выборка не переключается">
      <span className="variable__label">{label}</span>
      <span className="variable__value">{value}</span>
    </span>
  )
}

type DropdownProps = {
  /** The accessible name of the control, e.g. "Период". */
  name: string
  label: string
  icon?: React.ReactNode
  value: string
  options: { value: string; label: string }[]
  onChange: (value: string) => void
  before?: React.ReactNode
}

/**
 * A listbox that a keyboard and a screen reader can both drive.
 *
 * The markup is what the ARIA listbox pattern requires and nothing else:
 * the options are direct children of the element carrying
 * `role="listbox"`. Wrapping each one in an `<li>` looked tidier and
 * broke the pattern outright — the parent/child relationship the role
 * depends on ran through an element with a role of its own, so a screen
 * reader announced a list of seven items and no selectable options at
 * all.
 *
 * Keyboard handling follows the same pattern: arrows move between
 * options, Home and End jump to the ends, Escape closes and hands focus
 * back to the button that opened the menu. Without that last part a
 * keyboard user who dismisses the menu is dropped at the top of the
 * document.
 */
function Dropdown({ name, label, icon, value, options, onChange, before }: DropdownProps) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const listID = useId()

  useEffect(() => {
    if (!open) return
    const onDocClick = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setOpen(false)
        buttonRef.current?.focus()
      }
    }
    document.addEventListener('mousedown', onDocClick)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDocClick)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  // Opening puts focus on the current choice, so the first arrow key
  // moves from where the user already is rather than from the top.
  useEffect(() => {
    if (!open) return
    const menu = menuRef.current
    if (!menu) return
    const selected = menu.querySelector<HTMLButtonElement>('[aria-selected="true"]')
    ;(selected ?? menu.querySelector<HTMLButtonElement>('[role="option"]'))?.focus()
  }, [open])

  const moveFocus = (from: HTMLElement, delta: number | 'first' | 'last') => {
    const items = Array.from(menuRef.current?.querySelectorAll<HTMLButtonElement>('[role="option"]') ?? [])
    if (items.length === 0) return
    const at = items.indexOf(from as HTMLButtonElement)
    const next =
      delta === 'first'
        ? 0
        : delta === 'last'
          ? items.length - 1
          : (at + delta + items.length) % items.length
    items[next]?.focus()
  }

  const onMenuKey = (e: React.KeyboardEvent<HTMLDivElement>) => {
    const target = e.target as HTMLElement
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        moveFocus(target, 1)
        break
      case 'ArrowUp':
        e.preventDefault()
        moveFocus(target, -1)
        break
      case 'Home':
        e.preventDefault()
        moveFocus(target, 'first')
        break
      case 'End':
        e.preventDefault()
        moveFocus(target, 'last')
        break
      case 'Tab':
        setOpen(false)
        break
    }
  }

  const onButtonKey = (e: React.KeyboardEvent<HTMLButtonElement>) => {
    if (!open && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) {
      e.preventDefault()
      setOpen(true)
    }
  }

  return (
    <div className="dropdown" ref={ref}>
      {before}
      <button
        type="button"
        ref={buttonRef}
        className="dropdown__btn"
        aria-expanded={open}
        aria-haspopup="listbox"
        aria-controls={open ? listID : undefined}
        aria-label={`${name}: ${label}`}
        onClick={() => setOpen((v) => !v)}
        onKeyDown={onButtonKey}
      >
        {icon}
        <span>{label}</span>
        <Caret />
      </button>
      {open && (
        <div
          className="dropdown__menu"
          id={listID}
          role="listbox"
          aria-label={name}
          ref={menuRef}
          onKeyDown={onMenuKey}
        >
          {options.map((o) => (
            <button
              key={o.value}
              type="button"
              role="option"
              aria-selected={o.value === value}
              className={o.value === value ? 'is-selected' : ''}
              onClick={() => {
                onChange(o.value)
                setOpen(false)
                buttonRef.current?.focus()
              }}
            >
              {o.label}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

function clock(d: Date): string {
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}:${String(
    d.getSeconds(),
  ).padStart(2, '0')}`
}

const Chevron = () => (
  <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true">
    <path d="m4.5 2.5 3.5 3.5-3.5 3.5" fill="none" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
  </svg>
)

const Caret = () => (
  <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
    <path d="m2.5 4 2.5 2.5L7.5 4" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
  </svg>
)

const ClockIcon = () => (
  <svg width="13" height="13" viewBox="0 0 16 16" aria-hidden="true">
    <circle cx="8" cy="8" r="6.3" fill="none" stroke="currentColor" strokeWidth="1.3" />
    <path d="M8 4.6V8l2.4 1.7" fill="none" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
  </svg>
)

const RefreshIcon = ({ spinning }: { spinning?: boolean }) => (
  <svg
    width="13"
    height="13"
    viewBox="0 0 16 16"
    aria-hidden="true"
    className={spinning ? 'is-spinning' : undefined}
  >
    <path
      d="M13.4 8a5.4 5.4 0 1 1-1.7-3.9"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
    />
    <path d="M13.4 2.2v3h-3" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
  </svg>
)

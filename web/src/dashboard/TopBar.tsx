import { useEffect, useRef, useState } from 'react'
import type { DashboardData } from './types'
import type { Theme } from '../lib/theme'
import { Logo } from '../components/Logo'
import { linkHandler } from '../lib/router'

type Props = {
  data: DashboardData
  rangeID: string
  onRange: (id: string) => void
  refreshSeconds: number
  onRefreshSeconds: (seconds: number) => void
  onRefreshNow: () => void
  loading: boolean
  theme: Theme
  onTheme: (theme: Theme) => void
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
  theme,
  onTheme,
  lastUpdated,
}: Props) {
  return (
    <header className="topbar">
      <div className="topbar__row">
        <a className="topbar__brand" href="/" onClick={linkHandler('/')}>
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
          label={data.range.label}
          icon={<ClockIcon />}
          value={rangeID}
          options={data.range_options.map((o) => ({ value: o.id, label: o.label }))}
          onChange={onRange}
        />

        <Dropdown
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

        <button
          type="button"
          className="icon-btn"
          onClick={() => onTheme(theme === 'dark' ? 'light' : 'dark')}
          aria-label={theme === 'dark' ? 'Светлая тема' : 'Тёмная тема'}
        >
          {theme === 'dark' ? <SunIcon /> : <MoonIcon />}
        </button>
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
  label: string
  icon?: React.ReactNode
  value: string
  options: { value: string; label: string }[]
  onChange: (value: string) => void
  before?: React.ReactNode
}

function Dropdown({ label, icon, value, options, onChange, before }: DropdownProps) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onDocClick = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDocClick)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDocClick)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  return (
    <div className="dropdown" ref={ref}>
      {before}
      <button
        type="button"
        className="dropdown__btn"
        aria-expanded={open}
        aria-haspopup="listbox"
        onClick={() => setOpen((v) => !v)}
      >
        {icon}
        <span>{label}</span>
        <Caret />
      </button>
      {open && (
        <ul className="dropdown__menu" role="listbox">
          {options.map((o) => (
            <li key={o.value}>
              <button
                type="button"
                role="option"
                aria-selected={o.value === value}
                className={o.value === value ? 'is-selected' : ''}
                onClick={() => {
                  onChange(o.value)
                  setOpen(false)
                }}
              >
                {o.label}
              </button>
            </li>
          ))}
        </ul>
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

const SunIcon = () => (
  <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true">
    <circle cx="8" cy="8" r="3.1" fill="none" stroke="currentColor" strokeWidth="1.3" />
    <path
      d="M8 1v1.8M8 13.2V15M1 8h1.8M13.2 8H15M3 3l1.3 1.3M11.7 11.7 13 13M13 3l-1.3 1.3M4.3 11.7 3 13"
      stroke="currentColor"
      strokeWidth="1.3"
      strokeLinecap="round"
    />
  </svg>
)

const MoonIcon = () => (
  <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true">
    <path
      d="M13.5 10.2A5.8 5.8 0 0 1 5.8 2.5a5.8 5.8 0 1 0 7.7 7.7Z"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.3"
      strokeLinejoin="round"
    />
  </svg>
)

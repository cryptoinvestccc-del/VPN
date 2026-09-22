import { useTheme } from '../lib/theme'

/**
 * Light/dark switch, shared by both pages.
 *
 * It holds no state of its own beyond the hook: the default comes from
 * the system preference in CSS, so the first paint is already correct and
 * this only ever records an explicit override.
 *
 * The caller passes the class because the two pages shape their buttons
 * differently — pills on the product page, square controls in the
 * dashboard bar — and that is a styling decision, not this component's.
 */
export function ThemeToggle({ className = 'icon-btn' }: { className?: string }) {
  const [theme, setTheme] = useTheme()
  const next = theme === 'dark' ? 'light' : 'dark'

  return (
    <button
      type="button"
      className={className}
      onClick={() => setTheme(next)}
      aria-label={next === 'dark' ? 'Включить тёмную тему' : 'Включить светлую тему'}
      title={next === 'dark' ? 'Тёмная тема' : 'Светлая тема'}
    >
      {theme === 'dark' ? <SunIcon /> : <MoonIcon />}
    </button>
  )
}

function SunIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 16 16" aria-hidden="true">
      <circle cx="8" cy="8" r="3.1" fill="none" stroke="currentColor" strokeWidth="1.3" />
      <path
        d="M8 1v1.8M8 13.2V15M1 8h1.8M13.2 8H15M3 3l1.3 1.3M11.7 11.7 13 13M13 3l-1.3 1.3M4.3 11.7 3 13"
        stroke="currentColor"
        strokeWidth="1.3"
        strokeLinecap="round"
      />
    </svg>
  )
}

function MoonIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 16 16" aria-hidden="true">
      <path
        d="M13.5 10.2A5.8 5.8 0 0 1 5.8 2.5a5.8 5.8 0 1 0 7.7 7.7Z"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.3"
        strokeLinejoin="round"
      />
    </svg>
  )
}

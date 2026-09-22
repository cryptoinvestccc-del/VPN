import { Logo } from '../components/Logo'

const columns = [
  {
    title: 'Продукт',
    links: [
      { label: 'Как это работает', href: '#how' },
      { label: 'Возможности', href: '#features' },
      { label: 'Узлы', href: '#nodes' },
      { label: 'Тарифы', href: '#pricing' },
    ],
  },
  {
    title: 'Документация',
    links: [
      { label: 'Быстрый старт', href: '#docs' },
      { label: 'Дизайн и модель угроз', href: '#docs' },
      { label: 'Результаты аудита', href: '#docs' },
      { label: 'Метрики', href: '#docs' },
    ],
  },
  {
    title: 'Честно',
    links: [
      { label: 'Чего мы не умеем', href: '#faq' },
      { label: 'Что не проверено', href: '#docs' },
      { label: 'Вопросы', href: '#faq' },
    ],
  },
]

export function Footer() {
  return (
    <footer className="site-footer">
      <div className="shell">
        <div className="site-footer__grid">
          <div>
            <a className="brand" href="#top">
              <Logo />
              BESY
            </a>
            <p style={{ marginTop: 12, maxWidth: '28ch', color: 'var(--ink-secondary)', fontSize: '0.875rem' }}>
              Обфускатор WireGuard-трафика. Открытый код, никакой телеметрии на этой
              странице.
            </p>
          </div>

          {columns.map((col) => (
            <nav key={col.title} aria-label={col.title}>
              <h4>{col.title}</h4>
              <ul>
                {col.links.map((l) => (
                  <li key={l.label}>
                    <a href={l.href}>{l.label}</a>
                  </li>
                ))}
              </ul>
            </nav>
          ))}
        </div>

        <div className="site-footer__bottom">
          <span>© {new Date().getFullYear()} Besy VPN</span>
          <span>
            Ни аналитики, ни внешних шрифтов, ни сторонних запросов — страница грузится
            целиком с этого же сервера.
          </span>
        </div>
      </div>
    </footer>
  )
}

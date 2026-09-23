import { Logo } from '../components/Logo'
import { links } from '../lib/links'

const columns = [
  {
    title: 'Разделы',
    links: [
      { label: 'Тарифы', href: '#pricing' },
      { label: 'Что вы получаете', href: '#features' },
      { label: 'Устройства', href: '#platforms' },
      { label: 'Условия', href: '#limits' },
    ],
  },
  {
    title: 'Подключиться',
    links: [
      { label: 'Бот в Telegram', href: links.telegram },
      { label: 'Бот в MAX', href: links.max },
      { label: 'Приложение для iPhone', href: links.iosDefaultVpn },
      { label: 'Приложение для Android', href: links.androidAmnezia },
    ],
  },
  {
    title: 'Полезное',
    links: [
      { label: 'Документация Amnezia', href: links.amneziaDocs },
    ],
  },
]

/** Links that leave the site get target/rel; in-page anchors must not. */
function isExternal(href: string): boolean {
  return !href.startsWith('#')
}

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
              Интернет без блокировок от 75 ₽ в месяц. Протокол AmneziaWG, доступ
              в боте за минуту.
            </p>
          </div>

          {columns.map((col) => (
            <nav key={col.title} aria-label={col.title}>
              <h3>{col.title}</h3>
              <ul>
                {col.links.map((l) => (
                  <li key={l.label}>
                    <a
                      href={l.href}
                      {...(isExternal(l.href) ? { target: '_blank', rel: 'noopener' } : {})}
                    >
                      {l.label}
                    </a>
                  </li>
                ))}
              </ul>
            </nav>
          ))}
        </div>

        <div className="site-footer__bottom">
          <span>© {new Date().getFullYear()} BESY VPN</span>
          <span>
            Сайт не использует аналитику и сторонние сервисы.
          </span>
        </div>
      </div>
    </footer>
  )
}

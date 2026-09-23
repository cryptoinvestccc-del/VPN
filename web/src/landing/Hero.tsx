import { links } from '../lib/links'

const notes = [
  'Протокол AmneziaWG — снаружи не похоже на VPN',
  'Ключ приходит в бот сразу после оплаты',
  'iPhone, Android, Windows, macOS, Linux',
]

const tariffs = [
  { label: '1 месяц', value: '100 ₽' },
  { label: '6 месяцев', value: '500 ₽' },
  { label: '12 месяцев', value: '900 ₽' },
  { label: 'Друг оплатил', value: '+14 дней' },
]

const included = [
  'Доступ к серверу на всё время подписки',
  'Ссылка на подключение и инструкция по установке',
  'Помощь с настройкой, если что-то не завелось',
]

export function Hero() {
  return (
    <section className="section hero" id="top">
      <div className="shell hero__grid">
        <div className="hero__copy">
          <p className="eyebrow">VPN на AmneziaWG</p>
          <h1>VPN за 100&nbsp;₽ в&nbsp;месяц, без личных кабинетов</h1>
          <p className="lede hero__lede">
            BESY — это сервер Amnezia, поднятый для себя и открытый для других.
            Никакого сайта с регистрацией и корзиной: тариф выбирается в боте,
            после оплаты туда же приходит ссылка на подключение. Дальше —
            приложение Amnezia и одна кнопка.
          </p>

          <div className="hero__actions">
            <a
              className="btn btn--primary btn--lg"
              href={links.telegram}
              target="_blank"
              rel="noopener"
            >
              Открыть бота в Telegram
            </a>
            <a
              className="btn btn--ghost btn--lg"
              href={links.max}
              target="_blank"
              rel="noopener"
            >
              То же самое в MAX
            </a>
          </div>

          <ul className="hero__notes">
            {notes.map((n) => (
              <li className="hero__note" key={n}>
                <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true">
                  <path
                    d="m2.5 6.2 2.4 2.4L9.5 4"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="1.5"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  />
                </svg>
                {n}
              </li>
            ))}
          </ul>
        </div>

        <AccessCard />
      </div>
    </section>
  )
}

/*
  What used to sit here was a live-looking network console. There is one
  server and no telemetry behind this page, so every number in it was
  invented. The panel now shows the only figures that are actually true
  and checkable: what the bot charges.
*/
function AccessCard() {
  return (
    <div className="console">
      <div className="console__bar">
        <span className="console__title">Доступ к VPN</span>
        <span className="badge badge--good">
          <span className="dot" />
          AmneziaWG
        </span>
      </div>

      <div className="console__body">
        <div className="console__kpis">
          {tariffs.map((t) => (
            <div className="console__kpi" key={t.label}>
              <div className="console__kpi-label">{t.label}</div>
              <div className="console__kpi-value">{t.value}</div>
            </div>
          ))}
        </div>

        <div className="console__chart">
          <div className="console__chart-head">
            <span className="console__chart-title">Что входит</span>
          </div>
          <ul className="access-list">
            {included.map((i) => (
              <li key={i}>{i}</li>
            ))}
          </ul>
        </div>
      </div>

      <div className="console__foot">
        <span>
          Оплата и выдача ключа — в боте. На сайте ничего вводить не нужно.
        </span>
      </div>
    </div>
  )
}

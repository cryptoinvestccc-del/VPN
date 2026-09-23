import { links } from '../lib/links'

const steps = [
  { title: 'Нажмите тариф в боте', body: 'Месяц, полгода или год — одна кнопка.' },
  { title: 'Оплатите', body: 'Ссылка на подключение придёт в тот же чат через минуту.' },
  { title: 'Откройте ссылку в приложении', body: 'Профиль подставится сам, остаётся нажать «Подключиться».' },
]

export function Hero() {
  return (
    <section className="section hero" id="top">
      <div className="shell hero__grid">
        <div className="hero__copy">
          <p className="eyebrow">VPN на AmneziaWG</p>
          <h1>Интернет без блокировок за&nbsp;100&nbsp;₽ в&nbsp;месяц</h1>
          <p className="lede hero__lede">
            Работает там, где обычный VPN уже заблокирован. Без регистрации, без
            анкет и без ожидания ответа поддержки.
          </p>

          <div className="hero__actions">
            <a
              className="btn btn--primary btn--lg"
              href={links.telegram}
              target="_blank"
              rel="noopener"
            >
              Подключиться за 100 ₽
            </a>
            <a
              className="btn btn--ghost btn--lg"
              href={links.max}
              target="_blank"
              rel="noopener"
            >
              Оплатить в MAX
            </a>
          </div>
        </div>

        <StartCard />
      </div>
    </section>
  )
}

/*
  The panel beside the headline is the whole purchase, start to finish.
  It replaced a separate "how it works" section further down: the same
  three steps were being told three times on one page.
*/
function StartCard() {
  return (
    <div className="console">
      <div className="console__bar">
        <span className="console__title">Подключение за минуту</span>
        <span className="badge badge--good">
          <span className="dot" />
          AmneziaWG
        </span>
      </div>

      <div className="console__body">
        <ol className="start-steps">
          {steps.map((s, i) => (
            <li key={s.title}>
              <span className="start-steps__num" aria-hidden="true">
                {i + 1}
              </span>
              <span>
                <strong>{s.title}</strong>
                <br />
                {s.body}
              </span>
            </li>
          ))}
        </ol>
      </div>

      <div className="console__foot">
        <span>Остались вопросы — спросите в боте, там отвечает живой человек.</span>
      </div>
    </div>
  )
}

import { links } from '../lib/links'

export function Closer() {
  return (
    <section className="section" id="start">
      <div className="shell">
        <div className="closer">
          <p className="eyebrow" style={{ color: 'var(--ink-inverse-muted)' }}>
            Начать
          </p>
          <h2>Сто рублей и пять минут</h2>
          <p>
            Выбрать срок, оплатить, открыть пришедшую ссылку в приложении Amnezia.
            Ни регистрации, ни личного кабинета, ни разговора с поддержкой перед
            подключением.
          </p>
          <div className="closer__actions">
            <a
              className="btn btn--primary btn--on-dark btn--lg"
              href={links.telegram}
              target="_blank"
              rel="noopener"
            >
              Открыть бота в Telegram
            </a>
            <a
              className="btn btn--ghost btn--on-dark btn--lg"
              href={links.max}
              target="_blank"
              rel="noopener"
            >
              То же самое в MAX
            </a>
          </div>
        </div>
      </div>
    </section>
  )
}

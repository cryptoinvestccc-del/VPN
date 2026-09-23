import { links } from '../lib/links'

export function Closer() {
  return (
    <section className="section" id="start">
      <div className="shell">
        <div className="closer">
          <p className="eyebrow" style={{ color: 'var(--ink-inverse-muted)' }}>
            Начать
          </p>
          <h2>Сто рублей — и блокировок нет</h2>
          <p>
            Нажать тариф, оплатить, открыть пришедшую ссылку. Минута — и интернет
            снова работает целиком, а не наполовину.
          </p>
          <div className="closer__actions">
            <a
              className="btn btn--primary btn--on-dark btn--lg"
              href={links.telegram}
              target="_blank"
              rel="noopener"
            >
              Подключиться за 100 ₽
            </a>
            <a
              className="btn btn--ghost btn--on-dark btn--lg"
              href={links.max}
              target="_blank"
              rel="noopener"
            >
              Оплатить в MAX
            </a>
          </div>
        </div>
      </div>
    </section>
  )
}

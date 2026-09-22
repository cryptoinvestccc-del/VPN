export function Closer() {
  return (
    <section className="section" id="docs">
      <div className="shell">
        <div className="closer">
          <p className="eyebrow" style={{ color: 'var(--ink-inverse-muted)' }}>
            Начать
          </p>
          <h2>Поднимите туннель на своём VPS за один вечер</h2>
          <p>
            Скрипт установки, готовые конфиги, проверка конфигурации до перезапуска и
            сквозные тесты, которые гоняют настоящий WireGuard через настоящий туннель.
            Всё это лежит в репозитории — читать можно до того, как что-то ставить.
          </p>
          <div className="closer__actions">
            <a className="btn btn--primary btn--on-dark btn--lg" href="#pricing">
              Выбрать тариф
            </a>
            <a className="btn btn--ghost btn--on-dark btn--lg" href="#how">
              Сначала разобраться
            </a>
          </div>
        </div>
      </div>
    </section>
  )
}

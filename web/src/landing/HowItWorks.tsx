const layers = [
  {
    step: 'Шаг 01',
    title: 'Жмёте тариф в боте',
    body:
      'Месяц, полгода или год — одна кнопка. Ни почты, ни пароля, ни анкеты: бот уже знает, кто вы.',
  },
  {
    step: 'Шаг 02',
    title: 'Оплачиваете',
    body:
      'Ссылка на подключение падает в тот же чат автоматически. Не нужно ждать, пока кто-то проснётся и выдаст доступ вручную.',
  },
  {
    step: 'Шаг 03',
    title: 'Открываете ссылку в приложении',
    body:
      'Профиль подставляется сам — ничего не копируете и не настраиваете. Одна кнопка «Подключиться», и блокировок больше нет.',
  },
]

export function HowItWorks() {
  return (
    <section className="section" id="how">
      <div className="shell">
        <div className="section-head">
          <div className="section-head__text">
            <p className="eyebrow">Как это работает</p>
            <h2>От оплаты до рабочего VPN — одна минута</h2>
            <p className="lede">
              Три шага, все в боте. Личного кабинета нет, потому что он здесь не
              нужен.
            </p>
          </div>
        </div>

        <div className="layers">
          {layers.map((l) => (
            <article className="layer" key={l.step}>
              <span className="layer__step">{l.step}</span>
              <h3>{l.title}</h3>
              <p className="layer__body">{l.body}</p>
            </article>
          ))}
        </div>
      </div>
    </section>
  )
}

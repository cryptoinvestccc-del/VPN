const steps = [
  { step: 'Шаг 01', title: 'Нажмите тариф в боте', body: 'Месяц, полгода или год — одна кнопка.' },
  { step: 'Шаг 02', title: 'Оплатите', body: 'Ссылка на подключение придёт в тот же чат через минуту.' },
  {
    step: 'Шаг 03',
    title: 'Откройте ссылку в приложении',
    body: 'Профиль подставится сам, остаётся нажать «Подключиться».',
  },
]

/**
 * The purchase in three steps, directly under the prices it follows from.
 * It has no heading of its own: the prices above are the context, and a
 * second section title between a price and how to pay it only adds a
 * scroll.
 */
export function Steps() {
  return (
    <section className="section section--tight" aria-label="Как подключиться">
      <div className="shell">
        <ol className="layers steps-row">
          {steps.map((s) => (
            <li className="layer" key={s.step}>
              <span className="layer__step">{s.step}</span>
              <h3>{s.title}</h3>
              <p className="layer__body">{s.body}</p>
            </li>
          ))}
        </ol>
        <p className="steps-row__help">
          Остались вопросы — спросите в боте, там отвечает живой человек.
        </p>
      </div>
    </section>
  )
}

/**
 * What the buyer gets, as facts rather than paragraphs. Each line is one
 * claim that can be checked against the repository; anything that needed
 * three sentences to explain belongs in the docs, not on a landing page.
 */
const items = [
  ['Ключ на каждое устройство', 'Отзыв одного не трогает остальные.'],
  ['Два режима', 'UDP — быстрее. TLS на :443 — незаметнее против активных проб.'],
  ['Без логов', 'Только агрегаты по сети. Разбивка по клиентам выключена по умолчанию.'],
  ['Свой сервер', 'Код открыт, ставится одним скриптом. Без телеметрии.'],
]

export function Included() {
  return (
    <section className="section section--tight" id="features">
      <div className="shell">
        <ul className="facts">
          {items.map(([title, body]) => (
            <li className="fact" key={title}>
              <h3>{title}</h3>
              <p>{body}</p>
            </li>
          ))}
        </ul>
      </div>
    </section>
  )
}

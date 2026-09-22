/**
 * The limits stay on the page. They are the opposite of filler: a VPN
 * that promises anonymity without qualification is selling something
 * other than technology, and these three are the ones code does not
 * close.
 */
const limits = [
  ['Анализ таймингов', 'Паддинг ломает длины и сигнатуру, но не ритм.'],
  ['PSK общий на сеть', 'Внешний слой держится на нём; его компрометация — компрометация обфускации, не WireGuard.'],
  ['UDP против активных проб', 'Молчащий порт — тоже признак. В такой сети берите TLS.'],
]

export function Honest() {
  return (
    <section className="section section--tight">
      <div className="shell">
        <p className="eyebrow">Чего мы не умеем</p>
        <ul className="limits-list">
          {limits.map(([title, body]) => (
            <li key={title}>
              <strong>{title}</strong>
              <span>{body}</span>
            </li>
          ))}
        </ul>
      </div>
    </section>
  )
}

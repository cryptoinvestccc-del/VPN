const limits = [
  {
    title: 'Страна по умолчанию одна',
    body:
      'Весь трафик идёт через основной сервер. Нужна другая страна — напишите в бот, подберём и выдадим доступ лично. Меню со списком локаций на сайте нет, но и отказа не будет.',
  },
  {
    title: 'Один ключ — одно устройство',
    body:
      'Ключ рассчитан на один телефон или компьютер. Нужен VPN на втором устройстве — нужна вторая подписка; перекинуть один ключ на всю семью не получится.',
  },
  {
    title: 'Это не анонимность',
    body:
      'VPN прячет трафик от провайдера, но не делает вас невидимым. Аккаунты, куки и вход в собственную почту опознают вас независимо от того, через какой сервер вы вышли.',
  },
]

export function Limits() {
  return (
    <section className="section section--tight" id="limits">
      <div className="shell">
        <div className="section-head">
          <div className="section-head__text">
            <p className="eyebrow">Честно</p>
            <h2>Что стоит знать до оплаты</h2>
            <p className="lede">
              Три вещи без прикрас — чтобы вы узнали их сейчас, а не после
              оплаты.
            </p>
          </div>
        </div>

        <div className="limits">
          {limits.map((l) => (
            <article className="limit" key={l.title}>
              <h3>{l.title}</h3>
              <p>{l.body}</p>
            </article>
          ))}
        </div>
      </div>
    </section>
  )
}

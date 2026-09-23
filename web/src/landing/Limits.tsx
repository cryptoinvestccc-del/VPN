const limits = [
  {
    title: 'Сервер один',
    body:
      'Выбора страны нет — есть один сервер, и весь трафик идёт через него. Если вам нужен айпи конкретной страны или несколько локаций на выбор, это не сюда.',
  },
  {
    title: 'Это не анонимность',
    body:
      'VPN прячет трафик от провайдера, но не делает вас невидимым. Аккаунты, куки и вход в собственную почту опознают вас независимо от того, через какой сервер вы вышли.',
  },
  {
    title: 'На iPhone нужен обходной путь',
    body:
      'Приложения AmneziaVPN в российском App Store больше нет. Решение есть и оно простое — DefaultVPN от тех же разработчиков, — но это лишний шаг, и честнее сказать об этом до оплаты, а не после.',
  },
]

export function Limits() {
  return (
    <section className="section section--tight">
      <div className="shell">
        <div className="section-head">
          <div className="section-head__text">
            <p className="eyebrow">Честно</p>
            <h2>Что стоит знать до оплаты</h2>
            <p className="lede">
              Сервис маленький и держится на одном человеке. Вот вещи, из-за которых
              он может вам не подойти — лучше узнать их сейчас.
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

const limits = [
  {
    title: 'Нужна другая страна?',
    body:
      'По умолчанию подключение идёт через основной сервер. Если нужна конкретная страна, напишите в бот, подберём вариант лично.',
  },
  {
    title: 'Один ключ на одно устройство',
    body:
      'Для телефона и ноутбука понадобятся две подписки. Каждый ключ работает только на одном устройстве.',
  },
]

export function Limits() {
  return (
    <section className="section section--tight" id="limits">
      <div className="shell">
        <div className="section-head">
          <div className="section-head__text">
            <p className="eyebrow">Условия</p>
            <h2>Перед покупкой</h2>
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

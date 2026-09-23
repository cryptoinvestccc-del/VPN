const layers = [
  {
    step: 'Шаг 01',
    title: 'Выбрать тариф в боте',
    body:
      'Открываете бота в Telegram или в MAX и жмёте на нужный срок: месяц, полгода или год. Регистрация, почта и пароль не нужны — бот уже знает, кто вы.',
  },
  {
    step: 'Шаг 02',
    title: 'Оплатить — ссылка придёт сама',
    body:
      'Сразу после оплаты бот присылает ссылку на подключение. Никто ничего не выдаёт вручную и не просит ждать до утра.',
  },
  {
    step: 'Шаг 03',
    title: 'Открыть ссылку в приложении Amnezia',
    body:
      'Ставите приложение по ссылке из бота и открываете полученный ключ — профиль подставится сам. Остаётся нажать «Подключиться».',
  },
]

export function HowItWorks() {
  return (
    <section className="section" id="how">
      <div className="shell">
        <div className="section-head">
          <div className="section-head__text">
            <p className="eyebrow">Как это работает</p>
            <h2>Три шага, и ни одной формы регистрации</h2>
            <p className="lede">
              Весь сервис живёт в боте. Это не фигура речи: сайт ничего не принимает
              и ничего не хранит — он только рассказывает, что к чему, и ведёт в бот.
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

import { Icon, type IconName } from '../components/Icon'

const items: { icon: IconName; title: string; body: string }[] = [
  {
    icon: 'gauge',
    title: 'Сервер виден до оплаты',
    body:
      'На первом экране в реальном времени показано, сколько людей подключено и какая сейчас скорость. Вы видите, что покупаете.',
  },
  {
    icon: 'refresh',
    title: 'Приводите друзей и платите реже',
    body:
      'В боте у вас будет личная ссылка. Каждый друг, который оплатит подписку, добавляет вам 14 дней. Двое друзей почти окупают месяц.',
  },
  {
    icon: 'terminal',
    title: 'Оплата в Telegram или MAX',
    body:
      'Бот есть в обоих мессенджерах. Если Telegram работает с перебоями, оплатите через MAX, доступ будет тот же.',
  },
  {
    icon: 'shield',
    title: 'Помощь с настройкой',
    body:
      'Если что-то не подключается, напишите в бот. Отвечает живой человек и поможет настроить.',
  },
]

export function Features() {
  return (
    <section className="section" id="features">
      <div className="shell">
        <div className="section-head">
          <div className="section-head__text">
            <p className="eyebrow">Что вы получаете</p>
            <h2>Почему берут BESY</h2>
          </div>
        </div>

        <div className="features">
          {items.map((f) => (
            <article className="feature" key={f.title}>
              <span className="feature__icon">
                <Icon name={f.icon} />
              </span>
              <h3>{f.title}</h3>
              <p>{f.body}</p>
            </article>
          ))}
        </div>
      </div>
    </section>
  )
}

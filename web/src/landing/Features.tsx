import { Icon, type IconName } from '../components/Icon'

const items: { icon: IconName; title: string; body: string }[] = [
  {
    icon: 'layers',
    title: 'Проходит там, где другие блокируются',
    body:
      'AmneziaWG маскирует трафик так, что он не опознаётся как VPN. Обычный WireGuard уже научились резать — этот протокол сделан ровно против этого.',
  },
  {
    icon: 'key',
    title: 'Доступ через минуту, а не через сутки',
    body:
      'Бот выдаёт ссылку сам, в любое время дня и ночи. Не нужно писать в поддержку и доказывать, что вы уже заплатили.',
  },
  {
    icon: 'globe',
    title: 'Свой сервер, а не перепродажа',
    body:
      'Это не перепроданный доступ к чужому сервису, который отключат вместе с тысячей других. Сервер свой, и за него отвечает конкретный человек.',
  },
  {
    icon: 'refresh',
    title: 'Приводите друзей — платите реже',
    body:
      'Личная ссылка есть у каждого. Друг оплатил — вам плюс 14 дней. Двое друзей окупают месяц, четверо — почти два.',
  },
  {
    icon: 'terminal',
    title: 'Telegram или MAX — что удобнее',
    body:
      'Один и тот же сервис в двух мессенджерах. Если Telegram у вас не стоит или плохо ходит, берите MAX: доступ тот же самый.',
  },
  {
    icon: 'shield',
    title: 'Заполнять нечего',
    body:
      'Ни формы входа, ни корзины, ни почты, ни привязки карты на сайте. Всё, что от вас нужно, — нажать кнопку в боте.',
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

import { Icon, type IconName } from '../components/Icon'

const items: { icon: IconName; title: string; body: string }[] = [
  {
    icon: 'layers',
    title: 'AmneziaWG, а не обычный WireGuard',
    body:
      'Amnezia меняет заголовки и добавляет мусорные пакеты, поэтому трафик не опознаётся как WireGuard. Именно поэтому он проходит там, где обычный WireGuard уже перестал работать.',
  },
  {
    icon: 'key',
    title: 'Ключ сразу после оплаты',
    body:
      'Бот выдаёт ссылку на подключение автоматически. Не нужно писать в поддержку, ждать ответа и объяснять, что вы уже заплатили.',
  },
  {
    icon: 'globe',
    title: 'Свой сервер, а не перепродажа',
    body:
      'Это не реселл чужого VPN с общим кабинетом на тысячу человек. Сервер один, он мой, и я знаю, что на нём происходит.',
  },
  {
    icon: 'refresh',
    title: 'Приводите друзей — получаете дни',
    body:
      'У каждого есть реферальная ссылка. За каждого друга, который оплатит подписку, вам добавляется 14 дней. Считает бот, просить не нужно.',
  },
  {
    icon: 'terminal',
    title: 'Два мессенджера на выбор',
    body:
      'Один и тот же сервис работает в Telegram и в MAX. Если один мессенджер у вас не стоит или плохо ходит — берите второй, доступ будет тот же.',
  },
  {
    icon: 'shield',
    title: 'Никакой регистрации на сайте',
    body:
      'Здесь нет ни формы входа, ни корзины, ни сбора почты. Сайт — это страница с объяснением, а всё остальное происходит в боте.',
  },
]

export function Features() {
  return (
    <section className="section" id="features">
      <div className="shell">
        <div className="section-head">
          <div className="section-head__text">
            <p className="eyebrow">Что вы получаете</p>
            <h2>Коротко о сервисе</h2>
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

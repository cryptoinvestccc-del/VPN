import { Icon, type IconName } from '../components/Icon'

const items: { icon: IconName; title: string; body: string }[] = [
  {
    icon: 'layers',
    title: 'Почему его не блокируют',
    body:
      'AmneziaWG маскирует трафик так, что со стороны он не похож на VPN. Обычный WireGuard научились резать по форме пакетов — здесь этой формы нет, а шифрование то же самое.',
  },
  {
    icon: 'refresh',
    title: 'Приводите друзей — платите реже',
    body:
      'Личная ссылка есть у каждого, на любом тарифе. Друг оплатил — вам плюс 14 дней, и так за каждого. Двое друзей окупают месяц.',
  },
  {
    icon: 'terminal',
    title: 'Telegram или MAX — что удобнее',
    body:
      'Один и тот же сервис в двух мессенджерах. Если Telegram у вас не стоит или плохо ходит, берите MAX: доступ тот же самый.',
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

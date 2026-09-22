import { Icon, type IconName } from '../components/Icon'

const items: { icon: IconName; title: string; body: string }[] = [
  {
    icon: 'layers',
    title: 'Второй слой поверх WireGuard',
    body:
      'XChaCha20-Poly1305 на общем PSK плюс паддинг, рассчитанный по MTU: длина пакета перестаёт быть признаком, а фрагментация не появляется.',
  },
  {
    icon: 'key',
    title: 'Отдельный ключ на каждое устройство',
    body:
      'Отзыв одного телефона не заставляет перевыпускать ключи на всех остальных. Запись об отозванном доступе остаётся — вернуть его молча нельзя.',
  },
  {
    icon: 'probe',
    title: 'Ответ на активный пробинг',
    body:
      'Режим TLS отвечает неавторизованному соединению как обычный веб-сервер. UDP-порт на чужие пакеты просто молчит — отвечать нечем и незачем.',
  },
  {
    icon: 'eyeOff',
    title: 'Логов нет, есть счётчики',
    body:
      'По умолчанию собираются только агрегаты по всей сети. Разбивка по клиентам — это запись о том, кто и когда пользовался сервисом; её надо включать осознанно.',
  },
  {
    icon: 'refresh',
    title: 'Ротация ключа без даунтайма',
    body:
      'Сервер какое-то время принимает и старый, и новый PSK, поэтому клиенты переезжают по одному, а не все разом в ночь на субботу.',
  },
  {
    icon: 'box',
    title: 'Статические бинарники и scratch-образ',
    body:
      'Ни shell, ни пакетного менеджера, ни libc в контейнере. VPN-сервер смотрит в интернет, и всё, что помогло бы атакующему после компрометации, лучше туда просто не класть.',
  },
]

export function Features() {
  return (
    <section className="section" id="features">
      <div className="shell">
        <div className="section-head">
          <div className="section-head__text">
            <p className="eyebrow">Возможности</p>
            <h2>Что внутри</h2>
          </div>
          <a className="btn btn--quiet" href="#docs">
            Дизайн и модель угроз →
          </a>
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

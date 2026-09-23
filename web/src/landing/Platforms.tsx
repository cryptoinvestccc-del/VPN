import { Icon, type IconName } from '../components/Icon'
import { links } from '../lib/links'

type Platform = {
  icon: IconName
  name: string
  state: 'ready' | 'partial'
  stateLabel: string
  body: string
  app: { label: string; href: string }
}

const platforms: Platform[] = [
  {
    icon: 'globe',
    name: 'Android',
    state: 'ready',
    stateLabel: 'приложение Amnezia',
    body:
      'Ставится из Google Play или APK с сайта Amnezia. Ключ из бота открывается прямо в приложении — профиль подставляется сам.',
    app: { label: 'AmneziaVPN в Google Play', href: links.androidAmnezia },
  },
  {
    icon: 'shield',
    name: 'iPhone и iPad',
    state: 'partial',
    stateLabel: 'нужно другое приложение',
    body:
      'AmneziaVPN убрали из российского App Store, и менять регион ради него не нужно: берите DefaultVPN — это приложение от тех же разработчиков, оно доступно в российском App Store и понимает AmneziaWG.',
    app: { label: 'DefaultVPN в App Store', href: links.iosDefaultVpn },
  },
  {
    icon: 'terminal',
    name: 'Windows и macOS',
    state: 'ready',
    stateLabel: 'приложение Amnezia',
    body:
      'Десктопная версия AmneziaVPN скачивается с сайта Amnezia. Тот же ключ из бота, тот же сервер — отдельная подписка не нужна.',
    app: { label: 'Загрузки Amnezia', href: links.desktopAmnezia },
  },
  {
    icon: 'box',
    name: 'Linux',
    state: 'ready',
    stateLabel: 'приложение Amnezia',
    body:
      'Есть сборка AmneziaVPN под Linux. Если привычнее из консоли — AmneziaWG ставится и так, конфиг тот же самый.',
    app: { label: 'Загрузки Amnezia', href: links.desktopAmnezia },
  },
]

export function Platforms() {
  return (
    <section className="section" id="platforms">
      <div className="shell">
        <div className="section-head">
          <div className="section-head__text">
            <p className="eyebrow">Устройства</p>
            <h2>Где это работает</h2>
            <p className="lede">
              Своего приложения у BESY нет и не планируется: подключение идёт через
              приложение Amnezia. Ссылку на нужную версию бот присылает вместе с
              ключом.
            </p>
          </div>
        </div>

        <div className="platforms">
          {platforms.map((p) => (
            <article className="platform" key={p.name}>
              <div className="platform__head">
                <span className="feature__icon">
                  <Icon name={p.icon} size={18} />
                </span>
                <span className={`badge ${p.state === 'ready' ? 'badge--good' : 'badge--warning'}`}>
                  {p.stateLabel}
                </span>
              </div>
              <h3>{p.name}</h3>
              <p>{p.body}</p>
              <p style={{ marginTop: 10 }}>
                <a href={p.app.href} target="_blank" rel="noopener">
                  {p.app.label} →
                </a>
              </p>
            </article>
          ))}
        </div>
      </div>
    </section>
  )
}

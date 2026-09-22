import { Icon, type IconName } from '../components/Icon'

type Platform = {
  icon: IconName
  name: string
  state: 'ready' | 'partial'
  stateLabel: string
  body: string
}

const platforms: Platform[] = [
  {
    icon: 'terminal',
    name: 'Linux',
    state: 'ready',
    stateLabel: 'готово',
    body:
      'Профиль одним файлом, туннель поднимается через wg-quick. Systemd-юнит идёт в комплекте: не от root, ProtectSystem=strict, NoNewPrivileges.',
  },
  {
    icon: 'globe',
    name: 'Android',
    state: 'partial',
    stateLabel: 'через Termux',
    body:
      'Клиент работает в Termux, конфиг для приложения WireGuard генерируется отдельно — без хуков, их приложение отвергает. Своего APK пока нет, и мы этого не скрываем.',
  },
  {
    icon: 'box',
    name: 'Docker',
    state: 'ready',
    stateLabel: 'готово',
    body:
      'Образ на scratch из статических бинарников, read_only, no-new-privileges, единственная привилегия — NET_BIND_SERVICE для 443-го порта.',
  },
  {
    icon: 'shield',
    name: 'Свой сервер',
    state: 'ready',
    stateLabel: 'готово',
    body:
      'deploy/install.sh поднимает сервер на VPS целиком: отдельный пользователь, конфиг 0640, юнит, проверка конфига до перезапуска.',
  },
]

export function Platforms() {
  return (
    <section className="section" id="platforms">
      <div className="shell">
        <div className="section-head">
          <div className="section-head__text">
            <p className="eyebrow">Платформы</p>
            <h2>Где это уже работает</h2>
            <p className="lede">
              Список честный: то, чего пока нет, отмечено как «нет», а не как «скоро».
              iOS и десктопного приложения под Windows и macOS в проекте сейчас нет.
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
            </article>
          ))}
        </div>
      </div>
    </section>
  )
}

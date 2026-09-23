import { usePolling, type Resource } from '../lib/api'
import { links } from '../lib/links'
import type { Server } from '../lib/types'
import { LineChart } from '../components/LineChart'
import { metric, timeOfDay } from '../lib/format'

const emptyServer: Server = {
  generated_at: '',
  mock: false,
  reachable: true,
  clients_online: 0,
  throughput_mbps: 0,
  cpu_pct: 0,
  mem_pct: 0,
  uptime_s: 0,
  history: [],
}

export function Hero() {
  const server = usePolling<Server>('/api/v1/server', emptyServer, 1000)

  return (
    <section className="section hero" id="top">
      <div className="shell hero__grid">
        <div className="hero__copy">
          <p className="eyebrow">VPN на AmneziaWG</p>
          <h1>Интернет без блокировок за&nbsp;100&nbsp;₽ в&nbsp;месяц</h1>
          <p className="lede hero__lede">
            Работает там, где обычный VPN уже заблокирован. Без регистрации, без
            анкет и без ожидания ответа поддержки.
          </p>

          <div className="hero__actions">
            <a
              className="btn btn--primary btn--lg"
              href={links.telegram}
              target="_blank"
              rel="noopener"
            >
              Оплатить в Telegram
            </a>
            <a
              className="btn btn--ghost btn--lg"
              href={links.max}
              target="_blank"
              rel="noopener"
            >
              Оплатить в MAX
            </a>
          </div>
        </div>

        <ServerCard server={server} />
      </div>
    </section>
  )
}

type Badge = { text: string; tone: string; pulse: boolean }

/*
  The badge says what the numbers are, not merely whether a request
  worked: a sample served promptly is still a sample, and the one thing
  this card must never do is call invented figures live.
*/
function badge(server: Resource<Server>): Badge {
  const s = server.data
  if (server.error) return { text: 'нет связи с сайтом', tone: 'badge--warning', pulse: false }
  if (server.source === 'fallback') return { text: 'подключаемся…', tone: 'badge--muted', pulse: false }
  if (!s.reachable) return { text: 'сервер не отвечает', tone: 'badge--warning', pulse: false }
  if (s.mock) return { text: 'демонстрационные данные', tone: 'badge--muted', pulse: false }
  return { text: 'онлайн', tone: 'badge--good', pulse: true }
}

function uptime(seconds: number): string {
  if (seconds <= 0) return '—'
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  if (days > 0) return `${days} дн ${hours} ч`
  const minutes = Math.floor((seconds % 3600) / 60)
  return `${hours} ч ${minutes} мин`
}

function ServerCard({ server }: { server: Resource<Server> }) {
  const s = server.data
  const b = badge(server)
  const live = server.source === 'live' && s.reachable && !server.error

  const kpis = [
    { label: 'Подключено сейчас', value: live ? metric(s.clients_online, 0) : '—' },
    { label: 'Скорость', value: live ? `${metric(s.throughput_mbps, 1)} Мбит/с` : '—' },
    { label: 'Загрузка CPU', value: live ? `${metric(s.cpu_pct, 0)} %` : '—' },
    { label: 'Без перезагрузки', value: live ? uptime(s.uptime_s) : '—' },
  ]

  return (
    <div className="console">
      <div className="console__bar">
        <span className="console__title">Сервер сейчас</span>
        <span className={`badge ${b.tone}`}>
          <span className={`dot ${b.pulse ? 'dot--pulse' : ''}`} />
          {b.text}
        </span>
        {s.generated_at && (
          <span className="console__chart-meta" style={{ marginLeft: 'auto' }}>
            {timeOfDay(s.generated_at)}
          </span>
        )}
      </div>

      <div className="console__body">
        {/*
          Deliberately not an aria-live region: four figures that change
          every second would make a screen reader interrupt the visitor
          every second. The values are read when the reader reaches them.
        */}
        <div className="console__kpis" role="group" aria-label="Состояние сервера">
          {kpis.map((k) => (
            <div className="console__kpi" key={k.label}>
              <div className="console__kpi-label">{k.label}</div>
              <div className="console__kpi-value">{k.value}</div>
            </div>
          ))}
        </div>

        <div className="console__chart">
          <div className="console__chart-head">
            <span className="console__chart-title">Трафик через VPN</span>
            <span className="console__chart-meta">Мбит/с, последние 2 минуты</span>
          </div>
          {live && s.history.length > 1 ? (
            <LineChart
              points={s.history}
              unit="Мбит/с"
              title="Трафик через VPN, Мбит/с, последние 2 минуты"
              height={150}
            />
          ) : (
            <div className="console__chart-empty">
              {server.source === 'fallback' ? 'Загружаем данные…' : 'Нет свежих данных с сервера'}
            </div>
          )}
        </div>
      </div>

      <div className="console__foot">
        <span>
          {s.mock
            ? 'Демонстрационные показатели. На рабочем сайте здесь данные сервера.'
            : `Обновляется каждую секунду. Память: ${live ? metric(s.mem_pct, 0) : '—'} %. Только общие цифры, без данных о пользователях.`}
        </span>
      </div>
    </div>
  )
}

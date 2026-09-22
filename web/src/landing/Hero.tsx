import type { Status } from '../lib/types'
import type { Resource } from '../lib/api'
import { LineChart } from '../components/LineChart'
import { StatTile } from '../components/StatTile'
import { group, timeOfDay } from '../lib/format'

const notes = [
  'Криптография WireGuard не тронута',
  'Открытый код, ставится на свой сервер',
  'Агрегированные счётчики вместо логов',
]

export function Hero({ status }: { status: Resource<Status> }) {
  const s = status.data

  return (
    <section className="section hero" id="top">
      <div className="shell hero__stats">
        <p className="eyebrow">Сеть сейчас</p>
        <div className="stat-strip">
          {s.tiles.map((tile) => (
            <StatTile key={tile.id} tile={tile} />
          ))}
        </div>
      </div>

      <div className="shell hero__grid">
        <div className="hero__copy">
          <p className="eyebrow">Обфускация WireGuard</p>
          <h1>Трафик, который DPI не за что зацепить</h1>
          <p className="lede hero__lede">
            Besy VPN оборачивает уже зашифрованные WireGuard-пакеты во второй слой:
            XChaCha20-Poly1305 поверх PSK, случайный паддинг и мусорные пакеты.
            Сигнатура, распределение длин и поведение соединения перестают быть
            похожими на VPN — при этом сам WireGuard остаётся ровно тем, чем был.
          </p>

          <div className="hero__actions">
            <a className="btn btn--primary btn--lg" href="#pricing">
              Подключиться за 5 минут
            </a>
            <a className="btn btn--ghost btn--lg" href="#how">
              Как это устроено
            </a>
          </div>

          <ul className="hero__notes">
            {notes.map((n) => (
              <li className="hero__note" key={n}>
                <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true">
                  <path
                    d="m2.5 6.2 2.4 2.4L9.5 4"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="1.5"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  />
                </svg>
                {n}
              </li>
            ))}
          </ul>
        </div>

        <Console status={status} />
      </div>

    </section>
  )
}

function badge(status: Resource<Status>): { text: string; tone: string; pulse: boolean } {
  if (status.error) return { text: 'нет связи с API', tone: 'badge--warning', pulse: false }
  if (status.data.mock) return { text: 'демонстрационные данные', tone: 'badge--muted', pulse: false }
  return { text: 'живые данные', tone: 'badge--good', pulse: true }
}

function Console({ status }: { status: Resource<Status> }) {
  const s = status.data
  const kpis = [
    { label: 'Активные сессии', value: group(s.sessions_active) },
    { label: 'Трафик', value: `${group(s.throughput_mbps)} Мбит/с` },
    { label: 'Узлы онлайн', value: `${s.nodes_online} / ${s.nodes_total}` },
    { label: 'Режим по умолчанию', value: 'TLS :443' },
  ]

  return (
    <div className="console">
      <div className="console__bar">
        <span className="console__title">Состояние сети</span>
        {/*
          The badge reports what the numbers are, not whether the request
          succeeded. A server that answers promptly with its built-in
          sample set is still serving a sample, and calling that "живые
          данные" would be the one lie this page cannot afford.
        */}
        <span className={`badge ${badge(status).tone}`}>
          <span className={`dot ${badge(status).pulse ? 'dot--pulse' : ''}`} />
          {badge(status).text}
        </span>
        <span className="console__chart-meta" style={{ marginLeft: 'auto' }}>
          {timeOfDay(s.generated_at)}
        </span>
      </div>

      <div className="console__body">
        <div className="console__kpis">
          {kpis.map((k) => (
            <div className="console__kpi" key={k.label}>
              <div className="console__kpi-label">{k.label}</div>
              <div className="console__kpi-value">{k.value}</div>
            </div>
          ))}
        </div>

        <div className="console__chart">
          <div className="console__chart-head">
            <span className="console__chart-title">Трафик через туннель</span>
            <span className="console__chart-meta">
              {s.throughput.unit}, {s.throughput.window}
            </span>
          </div>
          <LineChart
            points={s.throughput.points}
            unit={s.throughput.unit}
            title={`Трафик через туннель, ${s.throughput.unit}, ${s.throughput.window}`}
          />
        </div>
      </div>

      <div className="console__foot">
        <span>
          {s.mock
            ? 'Демонстрационные показатели: сервер отдаёт встроенный образец, а не измерения.'
            : 'Агрегаты по сети. Разбивки по клиентам нет — она не собирается.'}
        </span>
      </div>
    </div>
  )
}

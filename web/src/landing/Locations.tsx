import type { Resource } from '../lib/api'
import type { Location } from '../lib/types'

const statusLabel: Record<Location['status'], string> = {
  online: 'онлайн',
  degraded: 'нагружен',
  maintenance: 'обслуживание',
}

const statusTone: Record<Location['status'], string> = {
  online: 'badge--good',
  degraded: 'badge--warning',
  maintenance: 'badge--muted',
}

type Props = {
  locations: Resource<Location[]>
  /** True while the API is serving its built-in sample set. */
  sample: boolean
}

export function Locations({ locations, sample }: Props) {
  const list = locations.data

  return (
    <section className="section section--tight" id="nodes">
      <div className="shell">
        <div className="section-head">
          <div className="section-head__text">
            <p className="eyebrow">Узлы</p>
            <h2>Куда можно подключиться</h2>
          </div>
          <span className={`badge ${sample ? 'badge--muted' : ''}`}>
            {sample ? 'демонстрационный список' : 'данные с сервера'}
          </span>
        </div>

        <div className="table-wrap" tabIndex={0} role="region" aria-label="Список узлов сети">
          <table className="data">
            <caption className="visually-hidden">Список узлов сети с задержкой и загрузкой</caption>
            <thead>
              <tr>
                <th scope="col">Город</th>
                <th scope="col">Страна</th>
                <th scope="col">Режим</th>
                <th scope="col" className="num">
                  Задержка
                </th>
                <th scope="col" className="num">
                  Загрузка
                </th>
                <th scope="col">Состояние</th>
              </tr>
            </thead>
            <tbody>
              {list.map((l) => (
                <tr key={l.id}>
                  <th scope="row" style={{ fontWeight: 500 }}>
                    {l.city}
                  </th>
                  <td>{l.country}</td>
                  <td>{l.mode === 'tls' ? 'TLS :443' : 'UDP'}</td>
                  <td className="num">{l.rtt_ms} мс</td>
                  <td className="num">{l.load_pct}%</td>
                  <td>
                    <span className={`badge ${statusTone[l.status]}`}>
                      <span className="dot" />
                      {statusLabel[l.status]}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  )
}

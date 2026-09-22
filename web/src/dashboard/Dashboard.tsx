import { useEffect, useState } from 'react'
import { TopBar } from './TopBar'
import { Panel } from './Panel'
import { useDashboard } from './useDashboard'
import { TimeSeriesChart } from '../charts/TimeSeriesChart'
import { Gauge } from '../charts/Gauge'
import { BarGauge } from '../charts/BarGauge'
import { StatBlock } from '../charts/StatBlock'
import { NodeTable } from '../charts/NodeTable'
import { linkHandler, queryParam, setQueryParam } from '../lib/router'

const RANGE_KEY = 'besy.range'
const REFRESH_KEY = 'besy.refresh'

export function Dashboard() {
  // The URL wins over the remembered choice: someone who opened a link
  // to a particular window asked for that window, whatever this browser
  // last looked at.
  const [rangeID, setRangeID] = useState(() => queryParam('range') ?? readString(RANGE_KEY, '6h'))
  const [refreshSeconds, setRefreshSeconds] = useState(() => readNumber(REFRESH_KEY, 30))
  const { data, requested, loading, error, lastUpdated, refresh } = useDashboard(rangeID, refreshSeconds)

  useEffect(() => {
    write(RANGE_KEY, rangeID)
    setQueryParam('range', rangeID)
  }, [rangeID])

  // The server answers an unknown window with the default rather than an
  // error, and says in the response which one it used. Reading that back
  // is what keeps a mistyped ?range= from leaving the address bar and the
  // picker claiming one thing while the panels show another.
  //
  // The comparison is against the window this response was *asked* for,
  // not against the one currently selected: while a new request is in
  // flight those differ, and correcting on that difference would undo
  // the reader's own choice a moment after they made it.
  const asked = requested
  const served = data?.range.id
  useEffect(() => {
    if (asked && served && served !== asked) setRangeID(served)
  }, [asked, served])
  useEffect(() => {
    write(REFRESH_KEY, String(refreshSeconds))
  }, [refreshSeconds])

  if (!data) {
    return (
      <div className="dash dash--empty">
        {error ? (
          <div className="notice">
            <strong>Не удалось загрузить дашборд.</strong>
            <br />
            {error}. Проверьте, что <code>obfsweb</code> запущен.
            <br />
            <button type="button" className="btn btn--ghost" onClick={refresh} style={{ marginTop: 12 }}>
              Повторить
            </button>
          </div>
        ) : (
          <div className="notice">Загружаем панели…</div>
        )}
      </div>
    )
  }

  return (
    <div className="dash">
      <TopBar
        data={data}
        rangeID={rangeID}
        onRange={setRangeID}
        refreshSeconds={refreshSeconds}
        onRefreshSeconds={setRefreshSeconds}
        onRefreshNow={refresh}
        loading={loading}
        lastUpdated={lastUpdated}
      />

      <main id="main" className="dash__body" tabIndex={-1}>
        {/*
          The page's name lives in the breadcrumbs, which are navigation
          rather than a heading. A screen reader user listing the
          headings on this page used to get panels and no page.
        */}
        <h1 className="visually-hidden">Обзор сети Besy VPN</h1>
        {error && (
          <div className="dash__banner">
            Данные не обновились: {error}. На панелях — последний удачный ответ
            {lastUpdated ? ` от ${lastUpdated.toLocaleTimeString('ru-RU')}` : ''}.
          </div>
        )}

        <div className="grid">
          {data.stats.map((stat) => (
            <div className="grid__cell" style={{ gridColumn: 'span 3' }} key={stat.id}>
              <div className="panel panel--stat">
                <StatBlock stat={stat} />
              </div>
            </div>
          ))}

          {data.gauges.map((gauge) => (
            <Panel key={gauge.id} title={gauge.title} span={4}>
              <Gauge panel={gauge} />
            </Panel>
          ))}

          {data.time_series.map((panel) => (
            <Panel key={panel.id} title={panel.title} description={panel.description} span={panel.span}>
              <TimeSeriesChart panel={panel} range={data.range} />
            </Panel>
          ))}

          <Panel
            title={data.bar_gauge.title}
            span={4}
            description="Текущая загрузка процессора на каждом узле, на одной шкале."
          >
            <BarGauge panel={data.bar_gauge} />
          </Panel>

          <Panel
            title="Узлы"
            span={8}
            description="Полная таблица: девять показателей на шесть узлов — случай, когда таблица честнее графика."
          >
            <NodeTable rows={data.nodes} />
          </Panel>
        </div>

        <footer className="dash__foot">
          <span>
            {data.mock
              ? 'Показатели демонстрационные: сервер отдаёт встроенный образец, а не измерения.'
              : 'Агрегаты по сети. Разбивки по клиентам нет — она не собирается.'}
          </span>
          <a href="/" onClick={linkHandler('/')}>
            О продукте →
          </a>
        </footer>
      </main>
    </div>
  )
}

function readString(key: string, fallback: string): string {
  try {
    return localStorage.getItem(key) ?? fallback
  } catch {
    return fallback
  }
}

function readNumber(key: string, fallback: number): number {
  const raw = readString(key, String(fallback))
  const n = Number(raw)
  return Number.isFinite(n) ? n : fallback
}

function write(key: string, value: string) {
  try {
    localStorage.setItem(key, value)
  } catch {
    // Blocked storage: the choice still applies for this visit.
  }
}

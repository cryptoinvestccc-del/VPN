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
  const { data, loading, error, lastUpdated, refresh } = useDashboard(rangeID, refreshSeconds)

  // rangeID is what was asked for; data.range.id is what the server
  // used, and an unknown window falls back to the default rather than
  // failing. The address bar, the remembered choice and the tick in the
  // picker all follow the second one, so a mistyped ?range=zzz corrects
  // itself instead of leaving the URL claiming one window while the
  // panels show another.
  const shownRange = data?.range.id ?? rangeID

  useEffect(() => {
    write(RANGE_KEY, shownRange)
    setQueryParam('range', shownRange)
  }, [shownRange])

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
        rangeID={shownRange}
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

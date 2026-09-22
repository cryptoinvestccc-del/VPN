const rows = [
  {
    trait: 'Транспорт',
    udp: 'UDP, произвольный порт',
    tls: 'Настоящий TLS поверх TCP, обычно :443',
  },
  {
    trait: 'Пассивный DPI',
    udp: 'Не опознаёт: ни сигнатуры, ни характерных длин',
    tls: 'Не опознаёт: снаружи это TLS-сессия',
  },
  {
    trait: 'Активный пробинг',
    udp: 'Слабое место: порт молчит, и само молчание заметно',
    tls: 'Отвечает как веб-сервер, проба ничего не даёт',
  },
  {
    trait: 'Накладные расходы',
    udp: 'Минимальные',
    tls: 'Выше: TCP-заголовки и рукопожатие',
  },
  {
    trait: 'Когда выбирать',
    udp: 'Сеть без активных проверок, важна задержка',
    tls: 'Жёсткая фильтрация, важно не выделяться',
  },
]

export function Modes() {
  return (
    <section className="section section--tight">
      <div className="shell">
        <div className="section-head">
          <div className="section-head__text">
            <p className="eyebrow">Два режима</p>
            <h2>UDP быстрее, TLS незаметнее</h2>
            <p className="lede">
              Оба режима включены на всех тарифах и переключаются одной строкой в
              конфиге. Разница не в маркетинге, а в том, против чего каждый держится.
            </p>
          </div>
        </div>

        <div className="table-wrap">
          <table className="data">
            <caption className="visually-hidden">Сравнение режимов UDP и TLS</caption>
            <thead>
              <tr>
                <th scope="col">Характеристика</th>
                <th scope="col">Режим UDP</th>
                <th scope="col">Режим TLS</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.trait}>
                  <th scope="row" style={{ fontWeight: 500 }}>
                    {r.trait}
                  </th>
                  <td>{r.udp}</td>
                  <td>{r.tls}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  )
}

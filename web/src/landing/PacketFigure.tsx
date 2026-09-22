/**
 * What a passive observer sees, before and after.
 *
 * Both rows are the same conversation. The top row is bare WireGuard: a
 * constant first byte, and packets that fall into a handful of repeated
 * sizes at a steady cadence — three features a classifier can key on
 * without decrypting anything. The bottom row is the same traffic through
 * this tunnel: no constant prefix, no size clustering, and junk packets
 * (hatched) breaking the cadence.
 *
 * Drawn in HTML rather than SVG: the packets have to keep their
 * proportions while the column width changes, and a stretched SVG would
 * take its corner radii and stroke widths with it. Flex-grow does the
 * same job here and stays crisp.
 */
const bare = [1, 1, 1, 1, 1]
const wrapped = [
  { size: 58, junk: false },
  { size: 104, junk: false },
  { size: 36, junk: true },
  { size: 78, junk: false },
  { size: 122, junk: false },
  { size: 46, junk: true },
  { size: 88, junk: false },
]

export function PacketFigure() {
  return (
    <figure className="packet-figure">
      <div className="packet-row">
        <p className="packet-row__label">Без обфускации</p>
        <div
          className="packets"
          role="img"
          aria-label="Пять одинаковых по длине пакетов, у каждого одинаковый первый байт, интервалы между ними равны."
        >
          {bare.map((grow, i) => (
            <span className="packet packet--bare" key={i} style={{ flexGrow: grow }}>
              <span className="packet__prefix" />
            </span>
          ))}
        </div>
        <p className="packet-row__caption">один и тот же префикс, одна и та же длина, ровный ритм</p>
      </div>

      <div className="packet-row">
        <p className="packet-row__label">Через Besy</p>
        <div
          className="packets"
          role="img"
          aria-label="Семь пакетов разной длины без постоянного первого байта, два из них — штрихованные мусорные пакеты, интервалы неравномерные."
        >
          {wrapped.map((p, i) => (
            <span
              className={`packet ${p.junk ? 'packet--junk' : 'packet--wrapped'}`}
              key={i}
              style={{ flexGrow: p.size }}
            />
          ))}
        </div>
        <p className="packet-row__caption">
          длины разные, префикса нет, штрихованные — мусорные пакеты
        </p>
      </div>

      <figcaption>
        Содержимое в обоих случаях расшифровать одинаково невозможно. Разница в том,
        что в верхнем потоке классификатору хватает метаданных, чтобы назвать его
        VPN-трафиком, а в нижнем — не за что зацепиться.
      </figcaption>
    </figure>
  )
}

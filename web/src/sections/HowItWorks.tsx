import { PacketFigure } from '../components/PacketFigure'

const layers = [
  {
    step: 'Шаг 01',
    title: 'WireGuard шифрует как обычно',
    body:
      'Curve25519, ChaCha20-Poly1305, forward secrecy — всё как в оригинале. Мы ничего не подменяем и не ослабляем: наружу выходит нормальный, полностью зашифрованный WG-пакет.',
  },
  {
    step: 'Шаг 02',
    title: 'Второй слой прячет форму',
    body:
      'Готовый пакет заворачивается ещё раз: nonce + XChaCha20-Poly1305 на PSK, сверху случайный паддинг с учётом MTU. Ни сигнатуры, ни характерного распределения длин не остаётся.',
  },
  {
    step: 'Шаг 03',
    title: 'Порт ведёт себя как веб-сервер',
    body:
      'В режиме TLS соединение идёт настоящим TLS-рукопожатием на 443-й порт. Тот, кто постучится без ключа, получит ответ обычного веб-сервера — а не молчание, по которому VPN и вычисляют.',
  },
]

export function HowItWorks() {
  return (
    <section className="section" id="how">
      <div className="shell">
        <div className="section-head">
          <div className="section-head__text">
            <p className="eyebrow">Как это работает</p>
            <h2>Три слоя, из которых наружу виден только третий</h2>
            <p className="lede">
              DPI ловит VPN не потому, что умеет расшифровать трафик, а потому, что
              трафик узнаётся по форме: фиксированный первый байт, характерные длины
              пакетов, ритм обмена. Besy убирает именно форму.
            </p>
          </div>
        </div>

        <div className="layers">
          {layers.map((l) => (
            <article className="layer" key={l.step}>
              <span className="layer__step">{l.step}</span>
              <h3>{l.title}</h3>
              <p className="layer__body">{l.body}</p>
            </article>
          ))}
        </div>

        <PacketFigure />
      </div>
    </section>
  )
}

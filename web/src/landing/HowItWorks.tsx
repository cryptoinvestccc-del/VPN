import { PacketFigure } from './PacketFigure'

const steps = [
  ['WireGuard шифрует как обычно', 'Curve25519 и ChaCha20-Poly1305, ничего не подменяем.'],
  ['Второй слой прячет форму', 'XChaCha20-Poly1305 на PSK плюс паддинг с учётом MTU.'],
  ['Порт отвечает как веб-сервер', 'В режиме TLS проба без ключа получает обычный ответ, а не молчание.'],
]

export function HowItWorks() {
  return (
    <section className="section" id="how">
      <div className="shell">
        <div className="section-head">
          <div className="section-head__text">
            <p className="eyebrow">Как это работает</p>
            <h2>DPI ловит VPN по форме трафика. Besy убирает форму</h2>
          </div>
        </div>

        <ol className="steps">
          {steps.map(([title, body], i) => (
            <li className="step" key={title}>
              <span className="step__num">{String(i + 1).padStart(2, '0')}</span>
              <h3>{title}</h3>
              <p>{body}</p>
            </li>
          ))}
        </ol>

        <PacketFigure />
      </div>
    </section>
  )
}

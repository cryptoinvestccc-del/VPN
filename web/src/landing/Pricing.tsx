import type { Resource } from '../lib/api'
import type { Plan } from '../lib/types'
import { money } from '../lib/format'
import { links } from '../lib/links'

export function Pricing({ plans }: { plans: Resource<Plan[]> }) {
  return (
    <section className="section" id="pricing">
      <div className="shell">
        <div className="section-head">
          <div className="section-head__text">
            <p className="eyebrow">Тарифы</p>
            <h2>Год выходит по 75 ₽ в месяц</h2>
            <p className="lede">
              Доступ на всех тарифах одинаковый — отличается только срок и цена
              месяца. Оплата в боте, ссылка на подключение приходит туда же.
            </p>
          </div>
        </div>

        <div className="plans">
          {plans.data.map((plan) => (
            <article className={`plan ${plan.featured ? 'plan--featured' : ''}`} key={plan.id}>
              <div className="plan__head">
                <h3>{plan.name}</h3>
                {plan.featured && <span className="badge">популярный</span>}
              </div>

              <div>
                <div className="plan__price">
                  <span className="plan__amount">
                    {plan.price === 0 ? 'Бесплатно' : money(plan.price, plan.currency)}
                  </span>
                  {plan.price !== 0 && <span className="plan__period">{plan.period}</span>}
                </div>
                <p className="plan__note">{plan.note}</p>
              </div>

              <ul className="plan__features">
                {plan.features.map((f) => (
                  <li key={f}>{f}</li>
                ))}
              </ul>

              <div className="plan__cta">
                <a
                  className={`btn btn--block ${
                    plan.featured ? 'btn--primary btn--on-dark' : 'btn--ghost'
                  }`}
                  href={links.telegram}
                  target="_blank"
                  rel="noopener"
                >
                  {plan.cta}
                </a>
              </div>
            </article>
          ))}
        </div>

        <p className="pricing__footnote">
          Друг оплатил подписку по вашей ссылке из бота — вам плюс 14 дней, и так
          за каждого. Вопросы — прямо в бот, там отвечает живой человек.{' '}
          <a href={links.max} target="_blank" rel="noopener">
            Если Telegram неудобен, тот же бот есть в MAX
          </a>
          .
        </p>
      </div>
    </section>
  )
}

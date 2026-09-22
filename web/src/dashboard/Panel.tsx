type Props = {
  title: string
  description?: string
  /** Width in the 12-column grid. */
  span?: number
  actions?: React.ReactNode
  children: React.ReactNode
}

/**
 * Panel chrome: a title bar and a body, nothing else.
 *
 * The description is a hover target rather than body text on purpose. It
 * explains what the panel measures, which a reader needs once and then
 * never again, and printing it under every title would cost more room
 * than the charts themselves.
 */
export function Panel({ title, description, span = 12, actions, children }: Props) {
  return (
    <section className="panel" style={{ gridColumn: `span ${span}` }}>
      <header className="panel__head">
        <h2 className="panel__title">{title}</h2>
        {description && (
          <span className="panel__info" tabIndex={0} role="note" aria-label={description}>
            <svg width="13" height="13" viewBox="0 0 16 16" aria-hidden="true">
              <circle cx="8" cy="8" r="6.4" fill="none" stroke="currentColor" strokeWidth="1.3" />
              <path d="M8 7.2v4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
              <circle cx="8" cy="4.9" r="0.85" fill="currentColor" />
            </svg>
            <span className="panel__tip">{description}</span>
          </span>
        )}
        {actions && <div className="panel__actions">{actions}</div>}
      </header>
      <div className="panel__body">{children}</div>
    </section>
  )
}

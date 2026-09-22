import { useId, useState } from 'react'

type Props = {
  question: string
  children: React.ReactNode
  defaultOpen?: boolean
}

export function Disclosure({ question, children, defaultOpen = false }: Props) {
  const [open, setOpen] = useState(defaultOpen)
  const id = useId()

  return (
    <div className="disclosure" data-open={open}>
      <button
        type="button"
        className="disclosure__btn"
        aria-expanded={open}
        aria-controls={id}
        onClick={() => setOpen((v) => !v)}
      >
        <span>{question}</span>
        <svg className="disclosure__sign" width="14" height="14" viewBox="0 0 14 14" aria-hidden="true">
          <path d="M7 1.5v11M1.5 7h11" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
        </svg>
      </button>
      {open && (
        <div className="disclosure__body" id={id}>
          {children}
        </div>
      )}
    </div>
  )
}

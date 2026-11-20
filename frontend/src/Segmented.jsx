import { useRef } from 'react'

/** Tabs as one pill: arrow keys move, and only the chosen tab is in the Tab order. `count` adds a number to a tab. */
export function Segmented({ label, value, options, onChange, controls }) {
  const refs = useRef({})
  const move = (ev) => {
    const step = ev.key === 'ArrowRight' || ev.key === 'ArrowDown' ? 1 : ev.key === 'ArrowLeft' || ev.key === 'ArrowUp' ? -1 : 0
    if (step === 0) {
      return
    }
    ev.preventDefault()
    const index = options.findIndex((o) => o.value === value)
    const next = options[(index + step + options.length) % options.length]
    onChange(next.value)
    refs.current[next.value]?.focus()
  }
  return (
    <div className="segmented" role="tablist" aria-label={label} onKeyDown={move}>
      {options.map((o) => (
        <button
          key={o.value}
          ref={(el) => { refs.current[o.value] = el }}
          type="button"
          role="tab"
          id={`tab-${o.value}`}
          aria-controls={controls}
          aria-selected={o.value === value}
          tabIndex={o.value === value ? 0 : -1}
          className="segmented__tab"
          onClick={() => onChange(o.value)}
        >
          {o.label}
          {o.count > 0 ? <span className="segmented__count">{o.count}</span> : null}
        </button>
      ))}
    </div>
  )
}

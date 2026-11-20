import { useEffect, useRef } from 'react'
import { useI18n } from './context.js'

const FOCUSABLE = 'button:not(:disabled), [href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])'

/**
 * A panel over the window, on its right edge and as tall as it (`page`: the whole window; `sheet`: a short dialog in the middle): the close button, Escape or a click outside close it. Focus moves into
 * the dialog (the box itself, so nothing is activated by accident), stays inside while it is open, and goes back to where it was when it closes.
 */
export function Modal({ title, onClose, page = false, sheet = false, children }) {
  const { t } = useI18n()
  const box = useRef(null)

  useEffect(() => {
    const before = document.activeElement
    box.current?.focus()
    return () => {
      if (before instanceof HTMLElement) {
        before.focus()
      }
    }
  }, [])

  const onKeyDown = (ev) => {
    if (ev.key === 'Escape') {
      ev.stopPropagation()
      onClose()
      return
    }
    if (ev.key !== 'Tab' || !box.current) {
      return
    }
    const items = Array.from(box.current.querySelectorAll(FOCUSABLE))
    if (items.length === 0) {
      ev.preventDefault()
      return
    }
    const first = items[0]
    const last = items[items.length - 1]
    if (ev.shiftKey && (document.activeElement === first || document.activeElement === box.current)) {
      ev.preventDefault()
      last.focus()
    } else if (!ev.shiftKey && document.activeElement === last) {
      ev.preventDefault()
      first.focus()
    }
  }

  return (
    <div
      className={`modal ${page ? 'modal--page' : sheet ? 'modal--sheet' : 'modal--side'}`}
      role="presentation"
      onKeyDown={onKeyDown}
      onMouseDown={(ev) => {
        if (ev.target === ev.currentTarget) {
          onClose()
        }
      }}
    >
      <div className="modal__box" role="dialog" aria-modal="true" aria-label={title} tabIndex={-1} ref={box}>
        <div className="modal__head">
          <h2 className="modal__title">{title}</h2>
          <button type="button" className="pill" onClick={onClose}>{t('nav.close')}</button>
        </div>
        <div className="modal__body">{children}</div>
      </div>
    </div>
  )
}

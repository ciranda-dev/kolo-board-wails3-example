import { useEffect, useMemo, useRef, useState } from 'react'
import { useI18n } from './context.js'
import { byKey, dropIndex, keyboardMove, sameSpot, settledShare, sharedNames, syncNote, personName, membersOf } from './board.js'
import { safeText } from './format.js'
import { Ring } from './Ring.jsx'
import { Icon } from './Icons.jsx'
import { Avatar, SyncMark } from './parts.jsx'

const KEYS = { ArrowUp: 'up', ArrowDown: 'down', ArrowLeft: 'left', ArrowRight: 'right', Home: 'top', End: 'bottom' }
const DRAG_START = 6
const EDGE = 48

/** One card: its label color, title, the start of its description, who has it, and whether it is settled. */
function Card({ card, people, shared, dragged, stuck, canEdit, onOpen, onKey, onPointerDown }) {
  const { t } = useI18n()
  const assignee = card.assignee ? people.get(card.assignee) : null
  const title = safeText(card.title)
  return (
    <button
      type="button"
      className={`kcard${card.syncing ? ' is-syncing' : ''}${dragged ? ' is-dragged' : ''}`}
      data-card={card.id}
      aria-keyshortcuts={canEdit ? 'Alt+ArrowUp Alt+ArrowDown Alt+ArrowLeft Alt+ArrowRight' : undefined}
      onClick={onOpen}
      onKeyDown={onKey}
      onPointerDown={onPointerDown}
    >
      {card.color ? (
        <span className={`kcard__label is-${card.color}`}>
          <span className="sr-only">{t('card.label', { color: t(`color.${card.color}`) })}</span>
        </span>
      ) : null}
      <span className={`kcard__title${title ? '' : ' is-empty'}`}>{title || t('card.untitled')}</span>
      {card.description ? <span className="kcard__text">{safeText(card.description.replace(/\s+/g, ' '))}</span> : null}
      <span className="kcard__foot">
        {assignee ? (
          <span className={`kcard__who${assignee.member ? '' : ' is-former'}`}>
            <Avatar person={assignee} size="tiny" />
            <span className="kcard__name">{personName(assignee, shared, t('person.unnamed'))}</span>
            {assignee.member ? null : <span className="sr-only">{t('person.former')}</span>}
          </span>
        ) : <span className="kcard__who" />}
        <SyncMark syncing={card.syncing} stuck={stuck} quiet={!card.syncing} />
      </span>
    </button>
  )
}

/**
 * The board: a bar that says how settled it is, then the columns side by side over the whole window
 * (stacked in a narrow one). A card moves by dragging it, or with Alt and an arrow key.
 */
export function BoardView({ board, notice, problem, leaving, onDismiss, onReconnect, onLeave, onSync, onCard, onNewCard, onColumn, onNewColumn, onMove }) {
  const { t, fail } = useI18n()
  const root = useRef(null)
  const pending = useRef(null)
  const skipClick = useRef(false)
  const refocus = useRef('')
  const [drag, setDrag] = useState(null)
  const people = useMemo(() => byKey(board.people), [board.people])
  const shared = useMemo(() => sharedNames(board.people), [board.people])
  const members = membersOf(board.people)
  const sync = board.sync
  const removed = board.state === 'removed'
  const note = removed ? null : syncNote(sync)
  const canEdit = board.canEdit && !removed
  const blocked = !removed && !board.canEdit && board.blocked ? board.blocked : null
  const stuck = removed || sync.stuck

  // A card moved by keyboard keeps the focus where it lands.
  useEffect(() => {
    if (refocus.current) {
      root.current?.querySelector(`[data-card="${CSS.escape(refocus.current)}"]`)?.focus()
      refocus.current = ''
    }
  }, [board])

  const columnTitle = (id) => safeText(board.columns.find((c) => c.id === id)?.title) || t('column.untitled')

  const onKey = (card) => (ev) => {
    const direction = ev.altKey ? KEYS[ev.key] : undefined
    if (!direction || !canEdit) {
      return
    }
    ev.preventDefault()
    const to = keyboardMove(board.columns, card.id, direction)
    if (!to) {
      return
    }
    refocus.current = card.id
    onMove(card.id, to.columnId, to.index, t('card.moved.live', { card: safeText(card.title) || t('card.untitled'), column: columnTitle(to.columnId), n: to.index + 1 }))
  }

  // Where the pointer is over the board: the column under it (or the nearest), and the card it would land above.
  const target = (x, y, id) => {
    const lanes = [...root.current.querySelectorAll('[data-column]')]
    let lane = lanes.find((el) => {
      const r = el.getBoundingClientRect()
      return x >= r.left && x < r.right && y >= r.top && y < r.bottom
    })
    if (!lane) {
      const near = (el) => {
        const r = el.getBoundingClientRect()
        return Math.max(r.left - x, 0, x - r.right) + Math.max(r.top - y, 0, y - r.bottom)
      }
      lane = lanes.sort((a, b) => near(a) - near(b))[0]
    }
    if (!lane) {
      return null
    }
    const list = lane.querySelector('.lane__cards')
    const box = list.getBoundingClientRect()
    if (y < box.top + EDGE) {
      list.scrollTop -= 12
    } else if (y > box.bottom - EDGE) {
      list.scrollTop += 12
    }
    const before = [...list.querySelectorAll('[data-card]')].find((el) => {
      const r = el.getBoundingClientRect()
      return el.dataset.card !== id && y < r.top + r.height / 2
    })
    const column = board.columns.find((c) => c.id === lane.dataset.column)
    return column ? { columnId: column.id, index: dropIndex(column.cards, id, before?.dataset.card ?? '') } : null
  }

  const onPointerDown = (card) => (ev) => {
    if (!canEdit || ev.button !== 0 || ev.pointerType === 'touch') {
      return
    }
    pending.current = { id: card.id, title: card.title, x: ev.clientX, y: ev.clientY, started: false }
  }

  useEffect(() => {
    const move = (ev) => {
      const p = pending.current
      if (!p) {
        return
      }
      if (!p.started && Math.hypot(ev.clientX - p.x, ev.clientY - p.y) < DRAG_START) {
        return
      }
      p.started = true
      const to = target(ev.clientX, ev.clientY, p.id)
      setDrag({ id: p.id, title: p.title, x: ev.clientX, y: ev.clientY, to })
    }
    const up = (ev) => {
      const p = pending.current
      pending.current = null
      if (!p?.started) {
        return
      }
      skipClick.current = true
      window.setTimeout(() => { skipClick.current = false }, 0)
      const to = target(ev.clientX, ev.clientY, p.id)
      setDrag(null)
      if (to && !sameSpot(board.columns, p.id, to.columnId, to.index)) {
        onMove(p.id, to.columnId, to.index, t('card.moved.live', { card: safeText(p.title) || t('card.untitled'), column: columnTitle(to.columnId), n: to.index + 1 }))
      }
    }
    const cancel = (ev) => {
      if (ev.type === 'keydown' && ev.key !== 'Escape') {
        return
      }
      pending.current = null
      setDrag(null)
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
    window.addEventListener('pointercancel', cancel)
    window.addEventListener('keydown', cancel)
    return () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
      window.removeEventListener('pointercancel', cancel)
      window.removeEventListener('keydown', cancel)
    }
    // The handlers read the board of this render.
  })

  const online = members.filter((p) => p.online).length

  return (
    <main className={`board${drag ? ' is-dragging' : ''}`} data-pane ref={root}>
      {removed ? (
        <div className="strip is-error" role="alert">
          <p>{t('error.you-were-removed')} {t('removed.readonly')}</p>
          <button type="button" className="btn btn--ghost btn--small" disabled={leaving} onClick={onLeave}>{t('leave.action')}</button>
        </div>
      ) : null}
      {board.error ? <div className="strip is-error" role="alert"><p>{fail(board.error)}</p></div> : null}
      {blocked ? (
        <div className={`strip${blocked.reason === 'strand-unconfirmed' ? '' : ' is-error'}`} role={blocked.reason === 'strand-unconfirmed' ? 'status' : 'alert'}>
          <p>{fail(blocked)}</p>
        </div>
      ) : null}
      {sync.halted ? <div className="strip is-error" role="alert"><p>{fail(sync.halted)}</p></div> : null}
      {!removed && sync.stuck ? <div className="strip is-error" role="alert"><p>{t('sync.stuck.note')}</p></div> : null}
      {notice ? (
        <div className={`strip${notice.tone === 'error' ? ' is-error' : ''}`} role={notice.tone === 'error' ? 'alert' : 'status'}>
          <p>{notice.text}</p>
          <span className="notice__actions">
            {notice.retry ? <button type="button" className="btn btn--ghost btn--small" onClick={onReconnect}>{t('network.retry')}</button> : null}
          </span>
        </div>
      ) : null}
      {problem ? (
        <div className="strip is-error" role="alert">
          <p>{fail(problem)}</p>
          <button type="button" className="btn btn--ghost btn--small" onClick={onDismiss}>{t('nav.dismiss')}</button>
        </div>
      ) : null}

      <div className="syncbar">
        <button type="button" className="syncbar__state" onClick={onSync} title={t('sync.explain')}>
          <Ring value={settledShare(sync.cardsSettled, sync.cardsSyncing)} peers={Math.max(0, online - 1)} tone={sync.halted ? 'failed' : sync.settling ? '' : 'muted'} />
          <span className="syncbar__counts">
            <span>{t('sync.count.settled', { n: sync.cardsSettled })}</span>
            <span className={sync.cardsSyncing > 0 ? 'is-syncing' : ''}>{t('sync.count.syncing', { n: sync.cardsSyncing })}</span>
          </span>
        </button>
        {note ? <p className={`syncbar__note is-${note.tone}`} role="status">{t(note.key, note.vars)}</p> : <span className="syncbar__note" />}
        {canEdit ? <button type="button" className="btn btn--ghost btn--small" id="add-column" onClick={onNewColumn}>{t('column.add')}</button> : null}
      </div>

      {board.columns.length === 0 ? (
        <div className="board__empty">
          <p className="board__none">{t('board.empty.title')}</p>
          <p className="note">{t(canEdit ? 'board.empty.note' : 'board.empty.readonly')}</p>
          {canEdit ? <button type="button" className="btn" onClick={onNewColumn}>{t('column.add')}</button> : null}
        </div>
      ) : (
        <div className="lanes" data-count={board.columns.length}>
          {board.columns.map((column) => {
            const title = safeText(column.title) || t('column.untitled')
            const slot = drag?.to?.columnId === column.id ? drag.to.index : -1
            const others = column.cards.filter((c) => c.id !== drag?.id)
            let seen = 0
            return (
              <section key={column.id} className={`lane${slot >= 0 ? ' is-target' : ''}`} data-column={column.id} aria-label={t('column.label', { title, n: column.cards.length })}>
                <header className="lane__head">
                  <h2 className="lane__title" title={title}>{title}</h2>
                  <span className="lane__count" aria-hidden="true">{column.cards.length}</span>
                  {column.syncing ? <SyncMark syncing stuck={stuck} /> : null}
                  <span className="lane__spacer" />
                  {canEdit ? (
                    <>
                      <button type="button" className="iconbtn" aria-label={t('card.add.in', { column: title })} title={t('card.add')} onClick={() => onNewCard(column.id)}><Icon name="plus" /></button>
                      <button type="button" className="iconbtn" aria-label={t('column.edit.label', { column: title })} title={t('column.edit.title')} onClick={() => onColumn(column.id)}><Icon name="more" /></button>
                    </>
                  ) : null}
                </header>
                <ul className="lane__cards">
                  {column.cards.map((card) => {
                    const dragged = drag?.id === card.id
                    const here = !dragged && seen === slot
                    if (!dragged) {
                      seen++
                    }
                    return (
                      <li key={card.id} className="lane__item">
                        {here ? <span className="lane__slot" aria-hidden="true" /> : null}
                        <Card
                          card={card}
                          people={people}
                          shared={shared}
                          dragged={dragged}
                          stuck={stuck}
                          canEdit={canEdit}
                          onOpen={() => {
                            if (!skipClick.current) {
                              onCard(card.id)
                            }
                          }}
                          onKey={onKey(card)}
                          onPointerDown={onPointerDown(card)}
                        />
                      </li>
                    )
                  })}
                  {slot >= others.length ? <li className="lane__item" aria-hidden="true"><span className="lane__slot" /></li> : null}
                  {column.cards.length === 0 && slot < 0 ? <li className="lane__none">{t('column.empty')}</li> : null}
                  {canEdit ? (
                    <li className="lane__item">
                      <button type="button" className="lane__add" onClick={() => onNewCard(column.id)}>{t('card.add')}</button>
                    </li>
                  ) : null}
                </ul>
              </section>
            )
          })}
        </div>
      )}

      {drag ? <div className="ghost" aria-hidden="true" style={{ transform: `translate(${drag.x + 12}px, ${drag.y + 12}px)` }}>{safeText(drag.title) || t('card.untitled')}</div> : null}
    </main>
  )
}

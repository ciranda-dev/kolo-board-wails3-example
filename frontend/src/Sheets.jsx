import { useState } from 'react'
import { useI18n } from './context.js'
import { COLORS, INVITE_TTLS, byKey, cardEdit, columnMove, findCard, membersOf, personName, sharedNames } from './board.js'
import { formatExpiry, safeLines, safeText } from './format.js'
import { CopyButton, SyncMark } from './parts.jsx'

const CLAIMS = ['signed', 'connections', 'everyone', 'names', 'kept', 'order']

/** What the window may say about protection: the six sentences of the contract, all of them, nothing more. */
export function Claims() {
  const { t } = useI18n()
  return (
    <ul className="claims">
      {CLAIMS.map((id) => <li key={id}>{t(`claims.${id}`)}</li>)}
    </ul>
  )
}

/** A question with its consequences, a way out and the action. The action's error stays in the sheet. */
export function ConfirmSheet({ lines, action, onConfirm, onClose }) {
  const { t, fail } = useI18n()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(null)
  const confirm = () => {
    setBusy(true)
    setError(null)
    Promise.resolve(onConfirm()).then(onClose).catch((err) => {
      setError(err)
      setBusy(false)
    })
  }
  return (
    <div className="sheet">
      {lines.map((line, i) => <p key={line} className={i === 0 ? 'sheet__lead' : 'note'}>{line}</p>)}
      {error ? <p className="alert alert--plain" role="alert">{fail(error)}</p> : null}
      <div className="form__actions">
        <button type="button" className="btn btn--ghost" onClick={onClose}>{t('nav.cancel')}</button>
        <button type="button" className="btn btn--danger" id="confirm-action" disabled={busy} onClick={confirm}>{action}</button>
      </div>
    </div>
  )
}

/** The board's title, for an admin. */
export function TitleSheet({ board, onSave, onClose }) {
  const { t, fail } = useI18n()
  const [value, setValue] = useState(board.title ?? '')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(null)
  const save = (ev) => {
    ev.preventDefault()
    setBusy(true)
    setError(null)
    onSave(value).then(onClose).catch((err) => {
      setError(err)
      setBusy(false)
    })
  }
  return (
    <form className="sheet" onSubmit={save}>
      <div>
        <label className="label" htmlFor="title-field">{t('board.title.label')}</label>
        <input id="title-field" className="field" value={value} onChange={(ev) => setValue(ev.target.value)} autoComplete="off" autoFocus aria-invalid={error ? 'true' : undefined} />
      </div>
      {error ? <p className="alert alert--plain" role="alert">{fail(error)}</p> : null}
      <div className="form__actions">
        <button type="button" className="btn btn--ghost" onClick={onClose}>{t('nav.cancel')}</button>
        <button type="submit" className="btn" disabled={busy}>{t('nav.save')}</button>
      </div>
    </form>
  )
}

/** A new card, or a card to edit, move or delete. Only the fields that changed are sent. */
export function CardSheet({ board, card, columnId, onAdd, onSave, onMove, onDelete, onClose }) {
  const { t, fail } = useI18n()
  const place = card ? findCard(board.columns, card.id) : null
  const [draft, setDraft] = useState({ title: card?.title ?? '', description: card?.description ?? '', assignee: card?.assignee ?? '', color: card?.color ?? '' })
  const [column, setColumn] = useState(place?.column.id ?? columnId ?? board.columns[0]?.id ?? '')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(null)
  const [deleting, setDeleting] = useState(false)
  const people = byKey(board.people)
  const shared = sharedNames(board.people)
  const members = membersOf(board.people)
  const former = draft.assignee && !people.get(draft.assignee)?.member ? people.get(draft.assignee) : null
  const canEdit = board.canEdit && board.state === 'active'
  const set = (patch) => {
    setDraft((prev) => ({ ...prev, ...patch }))
    setError(null)
  }
  const name = (key) => personName(people.get(key), shared, t('person.unnamed'))

  const save = async (ev) => {
    ev.preventDefault()
    setBusy(true)
    setError(null)
    try {
      if (!card) {
        const target = board.columns.find((c) => c.id === column)
        await onAdd(column, draft, target ? target.cards.length : 0)
      } else {
        const edit = cardEdit(card, draft)
        if (edit) {
          await onSave(edit)
        }
        if (place && column !== place.column.id) {
          const target = board.columns.find((c) => c.id === column)
          await onMove(column, target ? target.cards.length : 0)
        }
      }
      onClose()
    } catch (err) {
      setError(err)
      setBusy(false)
    }
  }

  if (deleting) {
    return <ConfirmSheet lines={[t('card.delete.ask', { title: safeText(card.title) || t('card.untitled') }), t('card.delete.note')]} action={t('card.delete')} onConfirm={onDelete} onClose={() => setDeleting(false)} />
  }

  return (
    <form className="sheet" onSubmit={save}>
      {!canEdit ? <p className="note" role="status">{board.blocked ? fail(board.blocked) : t('removed.readonly')}</p> : null}
      <div>
        <label className="label" htmlFor="card-title">{t('card.title')}</label>
        <input id="card-title" className="field" value={draft.title} onChange={(ev) => set({ title: ev.target.value })} disabled={!canEdit} autoComplete="off" autoFocus={!card} />
      </div>
      <div>
        <label className="label" htmlFor="card-text">{t('card.description')}</label>
        <textarea id="card-text" className="field" rows={3} value={card && !canEdit ? safeLines(draft.description) : draft.description} onChange={(ev) => set({ description: ev.target.value })} disabled={!canEdit} />
      </div>
      <div className="sheet__grid">
        <div>
          <label className="label" htmlFor="card-assignee">{t('card.assignee')}</label>
          <select id="card-assignee" className="field" value={draft.assignee} onChange={(ev) => set({ assignee: ev.target.value })} disabled={!canEdit}>
            <option value="">{t('card.assignee.none')}</option>
            {former ? <option value={former.key}>{t('card.assignee.former', { name: name(former.key) })}</option> : null}
            {members.map((p) => <option key={p.key} value={p.key}>{p.you ? t('person.you.named', { name: name(p.key) }) : name(p.key)}</option>)}
          </select>
        </div>
        <div>
          <label className="label" htmlFor="card-column">{t('card.column')}</label>
          <select id="card-column" className="field" value={column} onChange={(ev) => setColumn(ev.target.value)} disabled={!canEdit}>
            {board.columns.map((c) => <option key={c.id} value={c.id}>{safeText(c.title) || t('column.untitled')}</option>)}
          </select>
        </div>
      </div>
      <fieldset className="swatches" disabled={!canEdit}>
        <legend className="label">{t('card.color')}</legend>
        {['', ...COLORS].map((color) => (
          <label key={color} className="swatch">
            <input type="radio" name="card-color" value={color} checked={draft.color === color} onChange={() => set({ color })} />
            <span className={`swatch__dot${color ? ` is-${color}` : ''}`} aria-hidden="true" />
            <span className="swatch__name">{t(color ? `color.${color}` : 'color.none')}</span>
          </label>
        ))}
      </fieldset>
      {card ? (
        <p className="sheet__meta">
          <SyncMark syncing={card.syncing} />
          <span>{t(card.syncing ? 'card.syncing.note' : 'card.settled.note')}</span>
        </p>
      ) : null}
      {card ? <p className="note sheet__by">{t('card.by', { made: name(card.createdBy), last: name(card.lastBy) })}</p> : null}
      {error ? <p className="alert alert--plain" role="alert">{fail(error)}</p> : null}
      <div className="form__actions">
        {card && canEdit ? <button type="button" className="btn btn--danger sheet__left" id="card-delete" onClick={() => setDeleting(true)}>{t('card.delete')}</button> : null}
        <button type="button" className="btn btn--ghost" onClick={onClose}>{t(canEdit ? 'nav.cancel' : 'nav.close')}</button>
        {canEdit ? <button type="submit" className="btn" id="card-save" disabled={busy}>{t(card ? 'nav.save' : 'card.add')}</button> : null}
      </div>
    </form>
  )
}

/** A new column, or a column to rename, move one place or delete with its cards. */
export function ColumnSheet({ board, column, onAdd, onRename, onMove, onDelete, onClose }) {
  const { t, fail } = useI18n()
  const [title, setTitle] = useState(column?.title ?? '')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(null)
  const [deleting, setDeleting] = useState(false)
  const left = column ? columnMove(board.columns, column.id, -1) : null
  const right = column ? columnMove(board.columns, column.id, 1) : null

  const run = (promise, then) => {
    setBusy(true)
    setError(null)
    Promise.resolve(promise).then(then).catch(setError).finally(() => setBusy(false))
  }
  const save = (ev) => {
    ev.preventDefault()
    if (column && title === column.title) {
      onClose()
      return
    }
    run(column ? onRename(title) : onAdd(title), onClose)
  }

  if (deleting) {
    const n = column.cards.length
    const lines = [t('column.delete.ask', { title: safeText(column.title) || t('column.untitled') }), n > 0 ? t('column.delete.note', { n }) : t('column.delete.empty')]
    return <ConfirmSheet lines={lines} action={t('column.delete')} onConfirm={onDelete} onClose={() => setDeleting(false)} />
  }

  return (
    <form className="sheet" onSubmit={save}>
      <div>
        <label className="label" htmlFor="column-title">{t('column.title')}</label>
        <input id="column-title" className="field" value={title} onChange={(ev) => setTitle(ev.target.value)} autoComplete="off" autoFocus aria-invalid={error ? 'true' : undefined} />
      </div>
      {column ? (
        <div className="sheet__row">
          <span className="label">{t('column.place', { n: board.columns.indexOf(column) + 1, m: board.columns.length })}</span>
          <span className="sheet__moves">
            <button type="button" className="btn btn--ghost btn--small" disabled={busy || left === null} onClick={() => run(onMove(left))}>{t('column.move.left')}</button>
            <button type="button" className="btn btn--ghost btn--small" disabled={busy || right === null} onClick={() => run(onMove(right))}>{t('column.move.right')}</button>
          </span>
        </div>
      ) : null}
      {error ? <p className="alert alert--plain" role="alert">{fail(error)}</p> : null}
      <div className="form__actions">
        {column ? <button type="button" className="btn btn--danger sheet__left" id="column-delete" onClick={() => setDeleting(true)}>{t('column.delete')}</button> : null}
        <button type="button" className="btn btn--ghost" onClick={onClose}>{t('nav.cancel')}</button>
        <button type="submit" className="btn" id="column-save" disabled={busy}>{t(column ? 'nav.save' : 'column.add')}</button>
      </div>
    </form>
  )
}

/**
 * Making an invite: how long it lasts, how many people it takes, and whether an admin approves each
 * one. The code is shown once, here, with its Copy button; it is kept nowhere else.
 */
export function InviteSheet({ ready, network, onCreate, onCopy, onClose }) {
  const { t, fail, lang } = useI18n()
  const [ttl, setTtl] = useState(86400)
  const [uses, setUses] = useState('1')
  const [approval, setApproval] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(null)
  // The answer of CreateInvite, with its code: it lives in this sheet only and goes away with it.
  const [made, setMade] = useState(null)

  const create = (ev) => {
    ev.preventDefault()
    setBusy(true)
    setError(null)
    onCreate({ ttlSeconds: ttl, uses: Math.floor(Number(uses)) || 0, requireApproval: approval }).then(setMade).catch(setError).finally(() => setBusy(false))
  }

  if (made) {
    return (
      <div className="sheet">
        <p className="sheet__lead">{t('invite.made')}</p>
        <p className="invite__value" id="invite-code" tabIndex={0} aria-label={t('invite.code')}>{safeText(made.code)}</p>
        <p className="note">{t('invite.made.note', { n: made.uses, when: formatExpiry(made.expires, lang) })} {t(made.requireApproval ? 'invite.approval.on' : 'invite.open')}</p>
        <p className="note">{t('invite.once')}</p>
        <div className="form__actions">
          <button type="button" className="btn btn--ghost" onClick={onClose}>{t('nav.done')}</button>
          <CopyButton label={t('invite.copy')} onCopy={() => onCopy(made.code)} />
        </div>
      </div>
    )
  }

  return (
    <form className="sheet" onSubmit={create}>
      {!ready ? <p className="alert alert--plain" role="alert">{fail(network.error, t('error.network-not-ready'))}</p> : null}
      <div className="sheet__grid">
        <div>
          <label className="label" htmlFor="invite-ttl">{t('invite.ttl')}</label>
          <select id="invite-ttl" className="field" value={ttl} onChange={(ev) => setTtl(Number(ev.target.value))}>
            {INVITE_TTLS.map((s) => <option key={s} value={s}>{t(`invite.ttl.${s}`)}</option>)}
          </select>
        </div>
        <div>
          <label className="label" htmlFor="invite-uses">{t('invite.uses')}</label>
          <input id="invite-uses" className="field" type="number" min={1} max={100} step={1} value={uses} onChange={(ev) => setUses(ev.target.value)} />
        </div>
      </div>
      <div>
        <label className="check">
          <input type="checkbox" id="invite-approval" checked={approval} onChange={(ev) => setApproval(ev.target.checked)} aria-describedby="invite-approval-note" />
          <span>{t('invite.approval')}</span>
        </label>
        <p className={`field-note${approval ? '' : ' is-warning'}`} id="invite-approval-note">{t(approval ? 'invite.approval.on' : 'invite.open')}</p>
      </div>
      <p className="note">{t('invite.firstAdmins')} {t('invite.secret')}</p>
      <section className="sheet__claims" aria-labelledby="invite-claims">
        <h3 className="label" id="invite-claims">{t('claims.title')}</h3>
        <Claims />
      </section>
      {error ? <p className="alert alert--plain" role="alert">{fail(error)}</p> : null}
      <div className="form__actions">
        <button type="button" className="btn btn--ghost" onClick={onClose}>{t('nav.cancel')}</button>
        <button type="submit" className="btn" id="invite-create" disabled={busy || !ready}>{t('invite.create')}</button>
      </div>
    </form>
  )
}

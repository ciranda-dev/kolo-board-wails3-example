import { useState } from 'react'
import { useI18n } from './context.js'
import { JOIN_PHASES } from './board.js'
import { formatWait, looksLikeInvite } from './format.js'
import { boardError } from './errors.js'
import { Ring } from './Ring.jsx'
import { Split } from './Split.jsx'

/** First run: a board cannot be made or joined until there is a name the others will see. */
export function NameForm({ onSave }) {
  const { t, fail } = useI18n()
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(null)
  const save = (ev) => {
    ev.preventDefault()
    setBusy(true)
    setError(null)
    onSave(name).catch(setError).finally(() => setBusy(false))
  }
  return (
    <Split
      form
      label={t('name.label')}
      intro={
        <>
          <Ring />
          <h1 className="intro__title">{t('name.title')}</h1>
          <p className="intro__note" id="name-note">{t('name.note')}</p>
        </>
      }
      side={
        <form className="nameform" onSubmit={save}>
          <label className="label" htmlFor="name-field">{t('name.label')}</label>
          <input
            id="name-field"
            className="field"
            value={name}
            onChange={(ev) => {
              setName(ev.target.value)
              setError(null)
            }}
            placeholder={t('name.placeholder')}
            autoComplete="off"
            spellCheck={false}
            autoFocus
            aria-invalid={error ? 'true' : undefined}
            aria-describedby="name-note"
          />
          <button type="submit" className="btn" disabled={busy}>{t('name.save')}</button>
          {error ? <p className="alert alert--plain" role="alert">{fail(error)}</p> : null}
        </form>
      }
    />
  )
}

const MAX_COLUMNS = 8

/** The start: make a board on one side of the window, join one with an invite on the other. */
export function Home({ ready, network, notice, onReconnect, onCreate, onJoin, onPaste }) {
  const { t, fail } = useI18n()
  const [title, setTitle] = useState('')
  const [columns, setColumns] = useState(() => [t('home.column.todo'), t('home.column.doing'), t('home.column.done')])
  const [invite, setInvite] = useState('')
  const [busy, setBusy] = useState('')
  const [createError, setCreateError] = useState(null)
  const [joinError, setJoinError] = useState(null)

  const create = (ev) => {
    ev.preventDefault()
    setBusy('create')
    setCreateError(null)
    onCreate(title, columns.map((c) => c.trim()).filter(Boolean)).catch(setCreateError).finally(() => setBusy(''))
  }

  const join = (ev) => {
    ev.preventDefault()
    if (!invite.trim()) {
      setJoinError({ key: 'join.needInvite' })
      return
    }
    if (!looksLikeInvite(invite)) {
      setJoinError({ reason: 'invalid-invite', kind: 'invalid-invite', detail: '', cause: '', message: '' })
      return
    }
    setBusy('join')
    setJoinError(null)
    // The pasted invite lives in this field for one attempt and is cleared once the join has started.
    onJoin(invite).then(() => setInvite('')).catch(setJoinError).finally(() => setBusy(''))
  }

  const setColumn = (i, value) => setColumns((prev) => prev.map((c, at) => (at === i ? value : c)))
  const joinBlocked = !ready && boardError(joinError)?.reason !== 'invalid-invite'

  return (
    <div className="split split--home">
      <main className="pane pane--intro" data-pane>
        <form className="pane__inner" onSubmit={create}>
          <h1 className="intro__title">{t('home.create.title')}</h1>
          <p className="intro__note">{t('home.create.note')}</p>
          <div className="home__field">
            <label className="label" htmlFor="create-title">{t('home.create.name')}</label>
            <input
              id="create-title"
              className="field"
              value={title}
              onChange={(ev) => {
                setTitle(ev.target.value)
                setCreateError(null)
              }}
              placeholder={t('home.create.placeholder')}
              autoComplete="off"
              aria-invalid={createError ? 'true' : undefined}
            />
          </div>
          <fieldset className="home__columns">
            <legend className="label">{t('home.create.columns')}</legend>
            <ul className="chips">
              {columns.map((value, i) => (
                // The inputs keep their place: a column is edited where it stands.
                <li key={i} className="chips__item">
                  <input className="field field--chip" value={value} aria-label={t('home.create.column', { n: i + 1 })} onChange={(ev) => setColumn(i, ev.target.value)} autoComplete="off" />
                  {columns.length > 1 ? (
                    <button type="button" className="link" aria-label={t('home.create.removeColumn', { n: i + 1 })} onClick={() => setColumns((prev) => prev.filter((_, at) => at !== i))}>{t('home.create.remove')}</button>
                  ) : null}
                </li>
              ))}
            </ul>
            {columns.length < MAX_COLUMNS ? <button type="button" className="link link--add" onClick={() => setColumns((prev) => [...prev, ''])}>{t('home.create.addColumn')}</button> : null}
          </fieldset>
          {createError ? <p className="alert alert--plain" role="alert">{fail(createError)}</p> : null}
          <div className="hero__actions">
            <button type="submit" className="btn" id="create-submit" disabled={busy !== ''}>{t('home.create.action')}</button>
          </div>
          <p className="intro__hint">{t('home.create.hint')}</p>
        </form>
      </main>
      <aside className="pane pane--side pane--form" aria-label={t('home.join.title')} data-pane>
        <form className="pane__inner" onSubmit={join}>
          <h2 className="intro__title intro__title--second">{t('home.join.title')}</h2>
          <p className="intro__note">{t('home.join.note')}</p>
          <div className="home__field">
            <label className="label" htmlFor="join-invite">{t('home.join.label')}</label>
            <textarea
              id="join-invite"
              className="field field--mono join__field"
              value={invite}
              onChange={(ev) => {
                setInvite(ev.target.value)
                setJoinError(null)
              }}
              placeholder={t('home.join.placeholder')}
              autoComplete="off"
              autoCapitalize="off"
              spellCheck={false}
              aria-invalid={joinError ? 'true' : undefined}
            />
          </div>
          {notice ? (
            <div className={`notice is-${notice.tone}`} role={notice.tone === 'error' ? 'alert' : 'status'}>
              <p>{notice.text}</p>
              <span className="notice__actions">
                {notice.retry ? <button type="button" className="btn btn--ghost btn--small" onClick={() => onReconnect().catch(setJoinError)}>{t('network.retry')}</button> : null}
              </span>
            </div>
          ) : network.state === 'starting' ? <p className="note" role="status">{t('network.starting')}</p> : null}
          {joinError ? <p className="alert alert--plain" role="alert">{fail(joinError)}</p> : null}
          <div className="hero__actions">
            <button type="submit" className="btn" id="join-submit" disabled={busy !== '' || joinBlocked}>{t('home.join.action')}</button>
            <button
              type="button"
              className="btn btn--ghost"
              onClick={() => onPaste().then((text) => {
                setInvite(String(text ?? '').trim())
                setJoinError(null)
              }).catch(setJoinError)}
            >
              {t('home.join.paste')}
            </button>
          </div>
          <p className="intro__hint">{t('home.join.hint')}</p>
        </form>
      </aside>
    </div>
  )
}

/**
 * A join in its three steps, with the way out. Finding and waiting have no deadline unless the app was
 * started with -find-timeout or -join-timeout, so Cancel stays in reach.
 */
export function Joining({ board, settings, notice, leaving, onCancel }) {
  const { t, fail, lang } = useI18n()
  const [error, setError] = useState(null)
  const creating = board.state === 'creating'
  const at = Math.max(0, JOIN_PHASES.indexOf(board.phase))
  const phase = JOIN_PHASES[at]
  const deadline = phase === 'finding' ? settings?.findTimeoutMs : phase === 'waiting' ? settings?.joinTimeoutMs : -1
  const limit = deadline > 0 ? t(`joining.limit.${phase}`, { time: formatWait(deadline, lang) }) : deadline === 0 || deadline === undefined ? t('joining.limit.none') : ''
  return (
    <Split
      form
      label={t('joining.title')}
      intro={
        <>
          <Ring busy label={t('joining.ring')} valueText={creating ? t('joining.creating') : t(`joining.${phase}`)} />
          <h1 className="intro__title">{t(creating ? 'joining.creating' : 'joining.title')}</h1>
          {creating ? null : <p className="intro__note">{t(`joining.${phase}`)}</p>}
        </>
      }
      side={creating ? null : (
        <>
          <ol className="steps">
            {JOIN_PHASES.map((step, i) => (
              <li key={step} className={`step${i < at ? ' is-done' : i === at ? ' is-now' : ''}`} aria-current={i === at ? 'step' : undefined}>
                <span className="step__mark" aria-hidden="true">{i + 1}</span>
                <span>{t(`joining.${step}`)}</span>
              </li>
            ))}
          </ol>
          <p className="intro__note">{t(`joining.${phase}.note`, { every: formatWait(settings?.lookupIntervalMs || 5000, lang) })}</p>
          {limit ? <p className="intro__note" id="joining-limit">{limit}</p> : null}
          {notice ? <p className="alert alert--plain" role={notice.tone === 'error' ? 'alert' : 'status'} id="joining-notice">{notice.text}</p> : null}
          {error ? <p className="alert alert--plain" role="alert">{fail(error)}</p> : null}
          <div className="hero__actions">
            <button type="button" className="btn btn--ghost" disabled={leaving} onClick={() => onCancel().catch(setError)}>{t('joining.cancel')}</button>
          </div>
        </>
      )}
    />
  )
}

/** A board that ended with a reason: the sentence, and the way back. One that could not be opened stays until its files are removed. */
export function Ended({ board, leaving, onHome, onRemove }) {
  const { t, fail } = useI18n()
  const [error, setError] = useState(null)
  const stuck = board.error?.reason === 'board-open'
  return (
    <Split
      form
      label={t(stuck ? 'ended.stuck.title' : 'ended.title')}
      intro={
        <>
          <Ring tone="failed" value={1} />
          <h1 className="intro__title">{t(stuck ? 'ended.stuck.title' : 'ended.title')}</h1>
        </>
      }
      side={
        <>
          <p className="alert alert--plain" role="alert">{fail(board.error)}</p>
          {stuck ? <p className="intro__note">{t('ended.stuck.note')}</p> : null}
          {error ? <p className="alert alert--plain" role="alert">{fail(error)}</p> : null}
          <div className="hero__actions">
            {stuck
              ? <button type="button" className="btn btn--danger" disabled={leaving} onClick={() => onRemove().catch(setError)}>{t('ended.stuck.action')}</button>
              : <button type="button" className="btn" onClick={onHome}>{t('ended.home')}</button>}
          </div>
        </>
      }
    />
  )
}

/** The service did not start: the sentence of the error, and nothing to press. */
export function Fatal({ error }) {
  const { t, fail } = useI18n()
  return (
    <Split
      form
      label={t('fatal.title')}
      intro={
        <>
          <Ring tone="failed" value={1} />
          <h1 className="intro__title">{t('fatal.title')}</h1>
        </>
      }
      side={
        <>
          <p className="alert alert--plain" role="alert">{fail(error)}</p>
          <p className="intro__note">{t('fatal.note')}</p>
        </>
      }
    />
  )
}

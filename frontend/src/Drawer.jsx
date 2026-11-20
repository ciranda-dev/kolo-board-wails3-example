import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { call } from './service.js'
import { useI18n } from './context.js'
import { byKey, changeSentence, membersOf, personName, sharedNames } from './board.js'
import { formatExpiry, safeText, shortId } from './format.js'
import { Segmented } from './Segmented.jsx'
import { Avatar, SyncMark } from './parts.jsx'
import { Claims } from './Sheets.jsx'

const STATE_KEY = { active: 'invite.state.active', expired: 'invite.state.expired', 'used up': 'invite.state.used', revoked: 'invite.state.revoked' }

/** A button whose action can fail: the sentence of the error shows under the row it belongs to. */
function useAction() {
  const [busy, setBusy] = useState('')
  const [error, setError] = useState(null)
  const run = (id, promise) => {
    setBusy(id)
    setError(null)
    Promise.resolve(promise).catch((err) => setError({ id, err })).finally(() => setBusy(''))
  }
  return { busy, error, run }
}

/** Who is on the board: people asking to join, the members, the invites (for an admin), and the way out. */
function People({ board, admin, ready, invites, onInvite, onRevoke, onApprove, onDeny, onRemove, onLeave }) {
  const { t, fail, lang } = useI18n()
  const { busy, error, run } = useAction()
  const shared = useMemo(() => sharedNames([...board.people, ...board.requests]), [board.people, board.requests])
  const members = membersOf(board.people)
  const live = invites.filter((x) => x.state === 'active')
  const over = invites.filter((x) => x.state !== 'active')
  const fault = (id) => (error?.id === id ? <p className="alert alert--plain" role="alert">{fail(error.err)}</p> : null)

  return (
    <>
      {board.requests.length > 0 ? (
        <section className="drawer__section" aria-labelledby="drawer-requests">
          <h3 className="label" id="drawer-requests">{t('requests.title')}</h3>
          <ul className="rows">
            {board.requests.map((r) => (
              <li key={r.request} className="row row--request">
                <Avatar person={{ name: r.name, member: true }} />
                <span className="row__text">
                  <span className="row__name">{personName(r, shared, t('person.unnamed'))}</span>
                  <span className="row__meta">{t('requests.key', { key: shortId(r.joiner) })}</span>
                </span>
                {admin ? (
                  <span className="row__actions">
                    <button type="button" className="btn btn--small" disabled={busy !== ''} onClick={() => run(r.request, onApprove(r.request))}>{t('requests.approve')}</button>
                    <button type="button" className="btn btn--ghost btn--small" disabled={busy !== ''} onClick={() => run(r.request, onDeny(r.request))}>{t('requests.deny')}</button>
                  </span>
                ) : null}
                {fault(r.request)}
              </li>
            ))}
          </ul>
          <p className="field-note">{t(admin ? 'requests.note' : 'requests.note.member')}</p>
        </section>
      ) : null}

      <section className="drawer__section" aria-labelledby="drawer-members">
        <h3 className="label" id="drawer-members">{t('members.title', { n: members.length })}</h3>
        <ul className="rows">
          {members.map((p) => (
            <li key={p.key} className="row">
              <Avatar person={p} dot />
              <span className="row__text">
                <span className="row__name">
                  {personName(p, shared, t('person.unnamed'))}
                  {p.you ? <span className="tag tag--you">{t('person.you')}</span> : null}
                </span>
                <span className="row__meta">
                  {[t(p.admin ? 'person.admin' : 'person.member'), p.seat ? t('person.settler') : '', p.syncing ? t('person.syncing') : '', t(p.online ? 'person.online' : 'person.offline')].filter(Boolean).join(' · ')}
                </span>
                <span className="row__key" title={safeText(p.key)}>{shortId(p.key)}</span>
              </span>
              {admin && !p.you ? (
                <span className="row__actions">
                  <button type="button" className="link" aria-label={t('remove.label', { name: personName(p, shared, t('person.unnamed')) })} onClick={() => onRemove(p.key)}>{t('remove.action')}</button>
                </span>
              ) : null}
            </li>
          ))}
        </ul>
        <p className="field-note">{t('person.online.means')} {t('person.name.note')}</p>
      </section>

      {admin ? (
        <section className="drawer__section" aria-labelledby="drawer-invites">
          <div className="drawer__row">
            <h3 className="label" id="drawer-invites">{t('invites.title')}</h3>
            <button type="button" className="btn btn--small" id="drawer-invite" onClick={onInvite}>{t('invites.new')}</button>
          </div>
          {!ready ? <p className="field-note">{t('invites.offline')}</p> : null}
          {invites.length === 0 ? <p className="field-note">{t('invites.none')}</p> : (
            <ul className="rows">
              {[...live, ...over].map((x) => (
                <li key={x.id} className={`row row--invite${x.state === 'active' ? '' : ' is-over'}`}>
                  <span className="row__text">
                    <span className="row__name">{t(STATE_KEY[x.state] ?? 'invite.state.active')} <span className="row__id">{shortId(x.id, 6, 0)}</span></span>
                    <span className="row__meta">
                      {[t('invites.used', { k: x.used, n: x.uses }), x.state === 'active' ? t('invites.expires', { when: formatExpiry(x.expires, lang) }) : '', t(x.requireApproval ? 'invites.approval' : 'invites.open')].filter(Boolean).join(' · ')}
                    </span>
                  </span>
                  {x.state === 'active' ? (
                    <span className="row__actions">
                      <button type="button" className="link" disabled={busy !== ''} onClick={() => run(x.id, onRevoke(x.id))}>{t('invites.revoke')}</button>
                    </span>
                  ) : null}
                  {fault(x.id)}
                </li>
              ))}
            </ul>
          )}
          <p className="field-note">{t('invites.note')}</p>
        </section>
      ) : null}

      <section className="drawer__section" aria-labelledby="drawer-leave">
        <h3 className="label" id="drawer-leave">{t('leave.title')}</h3>
        <p className="field-note">{t(`leave.plan.${board.leave || 'everyone'}`)}</p>
        <div><button type="button" className="btn btn--danger btn--small" id="drawer-leave-btn" onClick={onLeave}>{t('leave.action')}</button></div>
      </section>
    </>
  )
}

// Items per page of the activity list: the service takes the window's choice (at least 1).
const ACTIVITY_PAGE = 50

/** Who did what, in the board's order, newest first. The board has no clock, so there are no times. */
function ActivityList({ board }) {
  const { t, fail } = useI18n()
  const [items, setItems] = useState([])
  const [people, setPeople] = useState([])
  const [more, setMore] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState(null)
  const count = useRef(0)

  const load = useCallback((before) => {
    const mine = ++count.current
    call('Activity', before, ACTIVITY_PAGE)
      .then((page) => {
        if (mine !== count.current) {
          return
        }
        const list = page?.items ?? []
        setItems((prev) => (before > 0 ? [...prev, ...list.filter((x) => !prev.some((y) => y.seq === x.seq))] : list))
        setPeople((prev) => (before > 0 ? [...prev, ...(page?.people ?? [])] : page?.people ?? []))
        setMore(Boolean(page?.more))
        setError(null)
      })
      .catch(setError)
      .finally(() => setLoaded(true))
  }, [])

  // Every change of the board can add to the list or settle an item: read the newest page again.
  const stamp = `${board.sync.pending}:${board.sync.cardsSettled}:${board.sync.cardsSyncing}:${board.columns.length}:${board.people.length}:${board.title}`
  useEffect(() => {
    load(0)
  }, [stamp, load])

  const known = useMemo(() => byKey([...board.people, ...people]), [board.people, people])
  const shared = useMemo(() => sharedNames([...known.values()]), [known])
  const name = (key) => personName(known.get(key), shared, t('person.unnamed')) || t('person.unnamed')

  const sentence = (item) => {
    const fields = (item.fields ?? []).map((f) => t(`field.${f}`)).join(', ')
    const s = changeSentence({ kind: item.kind, who: name(item.who), name: name(item.subject), text: safeText(item.text), column: safeText(item.column), fields, n: item.cards })
    return t(s.key, s.vars)
  }

  return (
    <section className="drawer__section drawer__section--fill" aria-labelledby="drawer-activity">
      <h3 className="label" id="drawer-activity">{t('activity.title')}</h3>
      {error ? <p className="alert alert--plain" role="alert">{fail(error)}</p> : null}
      {loaded && items.length === 0 && !error ? <p className="field-note">{t('activity.none')}</p> : null}
      <ol className="rows rows--activity">
        {items.map((item) => (
          <li key={item.seq} className={`row row--activity${item.settled ? '' : ' is-syncing'}`}>
            <Avatar person={known.get(item.who) ?? { name: '', member: true }} size="small" />
            <span className="row__text">
              <span className="row__line">{sentence(item)}</span>
            </span>
            <SyncMark syncing={!item.settled} quiet={item.settled} />
          </li>
        ))}
      </ol>
      {more ? <div><button type="button" className="btn btn--ghost btn--small" onClick={() => load(items[items.length - 1]?.seq ?? 0)}>{t('activity.more')}</button></div> : null}
      <p className="field-note">{t('activity.note')}</p>
    </section>
  )
}

/** What settled and syncing mean, how this board settles now, and what the window may say about protection. */
function SyncInfo({ board }) {
  const { t, fail } = useI18n()
  const sync = board.sync
  return (
    <>
      <section className="drawer__section" aria-labelledby="drawer-legend">
        <h3 className="label" id="drawer-legend">{t('legend.title')}</h3>
        <dl className="legend">
          <div className="legend__item">
            <dt><SyncMark syncing={false} /></dt>
            <dd>{t('legend.settled')}</dd>
          </div>
          <div className="legend__item">
            <dt><SyncMark syncing /></dt>
            <dd>{t('legend.syncing')}</dd>
          </div>
        </dl>
        <p className="field-note">{t('legend.keyboard')}</p>
      </section>
      <section className="drawer__section" aria-labelledby="drawer-settling">
        <h3 className="label" id="drawer-settling">{t('settling.title')}</h3>
        <dl className="facts facts--three">
          <div className="fact"><dt className="label">{t('settling.settlers')}</dt><dd className="fact__value">{t('settling.online', { k: sync.seatsOnline, m: sync.seats })}</dd></div>
          <div className="fact"><dt className="label">{t('settling.quorum')}</dt><dd className="fact__value">{String(sync.quorum)}</dd></div>
          <div className="fact"><dt className="label">{t('settling.pending')}</dt><dd className="fact__value">{String(sync.pending)}</dd></div>
        </dl>
        {sync.halted ? <p className="alert alert--plain" role="alert">{t('sync.halted')} {fail(sync.halted)}</p> : null}
        {!sync.halted && !sync.settling ? <p className="note">{t('sync.waiting', { n: sync.quorum, m: sync.seats, k: sync.seatsOnline })}. {t('sync.waiting.note')}</p> : null}
        {sync.pendingSeats > 0 ? <p className="note">{t('sync.adding')}</p> : null}
        {sync.singleSettler ? <p className="note">{t('sync.single')}</p> : null}
        <p className="field-note">{t('settling.note')}</p>
      </section>
      <section className="drawer__section" aria-labelledby="drawer-claims">
        <h3 className="label" id="drawer-claims">{t('claims.title')}</h3>
        <Claims />
        <p className="field-note row__key" title={safeText(board.id)}>{t('board.id', { id: shortId(board.id, 12, 6) })}</p>
      </section>
    </>
  )
}

/** The panel beside the board: people, activity, and what settling means. */
export function Drawer({ board, tab, onTab, ...rest }) {
  const { t } = useI18n()
  const options = [
    { value: 'people', label: t('drawer.people'), count: board.requests.length },
    { value: 'activity', label: t('drawer.activity') },
    { value: 'sync', label: t('drawer.sync') },
  ]
  return (
    <div className="drawer__body">
      <Segmented label={t('drawer.title')} value={tab} options={options} onChange={onTab} controls="drawer-panel" />
      <div className="drawer__panel" id="drawer-panel" role="tabpanel" aria-labelledby={`tab-${tab}`}>
        {tab === 'people' ? <People board={board} {...rest} /> : null}
        {tab === 'activity' ? <ActivityList board={board} /> : null}
        {tab === 'sync' ? <SyncInfo board={board} /> : null}
      </div>
    </div>
  )
}

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { call, on, copyText, pasteText } from './service.js'
import { I18n } from './context.js'
import { htmlLang, locales, pickLocale, translate } from './i18n.js'
import { safeText } from './format.js'
import { errorSentence, boardError } from './errors.js'
import { boardLists, changeSentence, fromChange, membersOf } from './board.js'
import { loadPrefs, savePrefs } from './prefs.js'
import { createGate, createSeqGuard, newerNetwork } from './fresh.js'
import { failedRelay } from './relays.js'
import { ActivityIcon, LinkIcon, UsersIcon } from './Icons.jsx'
import { Modal } from './Modal.jsx'
import { Disc, useMedia } from './parts.jsx'
import { Ended, Fatal, Home, Joining, NameForm } from './Start.jsx'
import { BoardView } from './BoardView.jsx'
import { CardSheet, ColumnSheet, ConfirmSheet, InviteSheet, TitleSheet } from './Sheets.jsx'
import { Drawer } from './Drawer.jsx'
import { SettingsPanel } from './Settings.jsx'
import { Sidebar } from './Sidebar.jsx'
import { readCollapsed, writeCollapsed } from './sidebar.js'
import './board.css'

const TOAST_MS = 6000
const TABS = ['people', 'activity', 'sync']

function App() {
  const [prefs, setPrefs] = useState(loadPrefs)
  const locale = pickLocale(prefs.lang, window.navigator.languages ?? [])
  const theme = prefs.theme === 'light' || prefs.theme === 'dark' ? prefs.theme : 'system'
  const lang = htmlLang[locale]
  const t = useCallback((key, vars) => translate(locale, key, vars), [locale])
  const fail = useCallback((err, fallback) => errorSentence(t, locale, err, fallback), [t, locale])
  const i18n = useMemo(() => ({ t, fail, lang }), [t, fail, lang])

  const [info, setInfo] = useState(null)
  const [network, setNetwork] = useState({ mode: 'public', state: 'starting', message: '', error: null, entryNodes: 0, firstNode: '', localNode: '', seq: 0 })
  const [fatal, setFatal] = useState(null)
  const [settings, setSettings] = useState(null)
  const [board, setBoard] = useState(null)
  // The last board that ended with an error, kept for its sentence until the person goes back.
  const [ended, setEnded] = useState(null)
  const [invites, setInvites] = useState([])
  // What is open over the window: '' or { kind, ... }. Never holds an invite.
  const [dialog, setDialog] = useState(null)
  const [drawer, setDrawer] = useState(false)
  const [toasts, setToasts] = useState([])
  const [problem, setProblem] = useState(null)
  const [spoken, setSpoken] = useState('')
  const [leaving, setLeaving] = useState(false)
  const [collapsed, setCollapsed] = useState(() => readCollapsed())
  const said = useRef('')
  // A snapshot can arrive after a newer event: the board and the network carry a seq, the invites do not.
  const boardGuard = useState(createSeqGuard)[0]
  const inviteGate = useState(createGate)[0]
  const docked = useMedia('(min-width: 1360px)')
  const tab = TABS.includes(prefs.drawerTab) ? prefs.drawerTab : 'people'

  const update = (patch) => {
    setPrefs((prev) => {
      const next = { ...prev, ...patch }
      savePrefs(next)
      return next
    })
  }

  useEffect(() => {
    document.documentElement.lang = lang
    document.title = t('app.title')
    if (theme === 'system') {
      delete document.documentElement.dataset.theme
    } else {
      document.documentElement.dataset.theme = theme
    }
  }, [lang, theme, t])

  // The board as the service sends it is the only copy: an ended one leaves the window, with its sentence if it has one.
  const take = useCallback((next) => {
    if (!boardGuard.take(next)) {
      return
    }
    if (!next) {
      setBoard(null)
      return
    }
    if (next.state === 'ended') {
      setBoard(null)
      setInvites([])
      setDrawer(false)
      setDialog((open) => (open?.kind === 'settings' ? open : null))
      setEnded(next.error && next.error.reason !== 'cancelled' ? next : null)
      return
    }
    setEnded(null)
    setBoard(boardLists(next))
  }, [boardGuard])

  const refreshInvites = useCallback(() => {
    const mark = inviteGate.mark()
    return call('Invites').then((list) => inviteGate.fresh(mark) && setInvites(list ?? [])).catch(() => {})
  }, [inviteGate])

  useEffect(() => {
    const offs = [
      on('board:state', take),
      on('board:network', (next) => setNetwork((prev) => newerNetwork(prev, next))),
      on('board:invites', (list) => {
        inviteGate.event()
        setInvites(list ?? [])
      }),
      on('board:change', (change) => {
        if (!change) {
          return
        }
        const id = `${Date.now()}:${Math.random()}`
        setToasts((prev) => [...prev.slice(-2), { id, change }])
        window.setTimeout(() => setToasts((prev) => prev.filter((x) => x.id !== id)), TOAST_MS)
      }),
    ]
    call('Start')
      .then((started) => {
        setInfo(started)
        setNetwork((prev) => newerNetwork(prev, started?.network))
        return Promise.all([call('Settings'), call('Board')])
      })
      .then(([saved, now]) => {
        setSettings(saved)
        take(now)
      })
      .catch(setFatal)
    return () => offs.forEach((off) => off())
    // The service is started once; the handlers only use state setters and the guards.
  }, [take, inviteGate])

  const state = board?.state ?? ''
  const shown = state === 'active' || state === 'removed'
  const joining = state === 'joining' || state === 'creating'
  const me = board?.people.find((p) => p.key === board.self) ?? null
  const admin = Boolean(me?.admin) && state === 'active'
  const named = Boolean(settings?.displayName)
  const ready = !fatal && network.state === 'ready'

  // An admin's window lists the invites (never their codes).
  useEffect(() => {
    if (admin) {
      refreshInvites()
    }
  }, [admin, refreshInvites])

  // Looking at the board marks the changes of the others as seen.
  const unseen = shown ? board.unseen : 0
  useEffect(() => {
    if (unseen > 0 && document.hasFocus()) {
      call('MarkSeen').catch(() => {})
    }
  }, [unseen])
  useEffect(() => {
    if (!shown) {
      return undefined
    }
    const seen = () => call('MarkSeen').catch(() => {})
    window.addEventListener('focus', seen)
    return () => window.removeEventListener('focus', seen)
  }, [shown])

  // One polite sentence per step of a join and per end.
  useEffect(() => {
    const key = ended ? `ended:${ended.error?.reason}` : `${state}:${board?.phase ?? ''}`
    if (said.current === key) {
      return
    }
    said.current = key
    if (ended) {
      setSpoken(fail(ended.error))
    } else if (state === 'joining') {
      setSpoken(t(`joining.${board.phase || 'finding'}`))
    } else if (state === 'removed') {
      setSpoken(t('error.you-were-removed'))
    } else if (state === 'active') {
      setSpoken(t('board.live.ready', { title: safeText(board.title) }))
    }
  }, [state, board, ended, t, fail])

  const saveName = (name) =>
    call('SetDisplayName', name).then((next) => {
      setSettings(next)
      return next
    })

  const create = (title, columns) => call('CreateBoard', title, columns).then(take)
  const join = (invite) => call('JoinBoard', invite).then(take)

  const leave = () => {
    setLeaving(true)
    return call('LeaveBoard')
      .catch((err) => {
        if (boardError(err)?.reason !== 'no-board') {
          throw err
        }
      })
      .then(() => {
        setBoard(null)
        setEnded(null)
        setDialog(null)
        setDrawer(false)
      })
      .finally(() => setLeaving(false))
  }

  // A change asked from the board itself (a drag, a key): its error shows in a line over the board.
  const act = useCallback((promise, done) => {
    setProblem(null)
    return Promise.resolve(promise)
      .then(() => {
        if (done) {
          setSpoken(done)
        }
      })
      .catch(setProblem)
  }, [])

  const moveCard = useCallback((id, columnId, index, done) => act(call('MoveCard', id, columnId, index), done), [act])

  const openSettings = () => {
    call('Settings').then(setSettings).catch(() => {})
    setDialog({ kind: 'settings' })
  }
  const openDrawer = (which) => {
    update({ drawerTab: which })
    setDrawer(true)
  }
  const close = () => setDialog(null)
  const toggleSidebar = () => {
    writeCollapsed(!collapsed)
    setCollapsed(!collapsed)
  }

  const toastText = (change) => {
    const s = changeSentence({ ...fromChange(change), who: safeText(change.who) || t('person.unnamed'), text: safeText(fromChange(change).text), column: safeText(fromChange(change).column) })
    return t(s.key, s.vars)
  }

  const view = fatal ? 'fatal' : !settings ? 'loading' : shown ? 'board' : joining ? 'joining' : ended ? 'ended' : !named ? 'name' : 'home'
  const members = shown ? membersOf(board.people) : []
  const online = members.filter((p) => p.online).length

  const relay = failedRelay(network)
  const notice = fatal
    ? null
    : network.state === 'failed'
      ? { tone: 'error', text: fail(network.error, safeText(network.message)), retry: true }
      : relay
        ? { tone: 'error', text: fail(relay.error, relay.addr), retry: false }
        : null

  const drawerBody = shown ? (
    <Drawer
      board={board}
      tab={tab}
      admin={admin}
      ready={ready}
      invites={invites}
      onTab={(which) => update({ drawerTab: which })}
      onInvite={() => setDialog({ kind: 'invite' })}
      onRevoke={(id) => call('RevokeInvite', id).then(refreshInvites)}
      onApprove={(request) => call('ApproveRequest', request)}
      onDeny={(request) => call('DenyRequest', request)}
      onRemove={(key) => setDialog({ kind: 'remove', key })}
      onLeave={() => setDialog({ kind: 'leave' })}
    />
  ) : null

  const dialogCard = dialog?.kind === 'card' && shown ? board.columns.flatMap((c) => c.cards).find((c) => c.id === dialog.id) : null
  const dialogColumn = dialog?.kind === 'column' && shown ? board.columns.find((c) => c.id === dialog.id) : null
  const dialogPerson = dialog?.kind === 'remove' && shown ? board.people.find((p) => p.key === dialog.key) : null

  return (
    <I18n.Provider value={i18n}>
      <div className="app">
        <div className="titlebar" />
        <div className="body">
          {fatal ? null : (
            <Sidebar
              board={shown ? board : null}
              empty={view === 'home' || view === 'name'}
              network={network}
              me={info?.keyId ? { key: info.keyId, name: safeText(settings?.displayName) } : null}
              collapsed={collapsed}
              settingsOpen={dialog?.kind === 'settings'}
              settingsReady={Boolean(settings)}
              onToggle={toggleSidebar}
              onSelect={() => dialog?.kind === 'settings' && close()}
              onSettings={() => (dialog?.kind === 'settings' ? close() : openSettings())}
            />
          )}
          <div className="main">
            {shown ? (
              <header className="mainhead">
                <Disc name={safeText(board.title)} seed={board.id} size={40} />
                <div className="mainhead__text">
                  <h1 className="mainhead__name">
                    {admin ? (
                      <button type="button" className="mainhead__rename" title={t('board.rename')} onClick={() => setDialog({ kind: 'title' })}>
                        <span className="mainhead__titletext">{safeText(board.title) || t('board.untitled')}</span>
                      </button>
                    ) : <span className="mainhead__titletext">{safeText(board.title) || t('board.untitled')}</span>}
                  </h1>
                  <p className="mainhead__sub">
                    {t('head.members', { n: members.length })} · <span className={online > 0 ? 'is-online' : ''}>{t('head.connected', { n: online })}</span>
                  </p>
                </div>
                <div className="mainhead__actions">
                  {admin ? (
                    <button type="button" className="pill pill--primary" onClick={() => setDialog({ kind: 'invite' })}>
                      <LinkIcon />
                      <span className="pill__text">{t('nav.invite')}</span>
                    </button>
                  ) : null}
                  <button type="button" className="pill pill--activity" aria-expanded={drawer && tab === 'activity'} onClick={() => openDrawer('activity')}>
                    <ActivityIcon />
                    <span className="pill__text">{t('nav.activity')}</span>
                  </button>
                  <button type="button" className="pill pill--members" aria-expanded={drawer && tab === 'people'} onClick={() => openDrawer('people')}>
                    <UsersIcon />
                    <span className="pill__text">{t('nav.people')}</span>
                    {board.requests.length > 0 ? <span className="pill__badge" title={t('requests.title')}>{board.requests.length}</span> : null}
                  </button>
                </div>
              </header>
            ) : null}

            <div className="main__body">
              {view === 'board' ? (
                <BoardView
                  board={board}
                  notice={notice}
                  problem={problem}
                  leaving={leaving}
                  onDismiss={() => setProblem(null)}
                  onReconnect={() => call('Reconnect').catch(setProblem)}
                  onLeave={() => setDialog({ kind: 'leave' })}
                  onSync={() => openDrawer('sync')}
                  onCard={(id) => setDialog({ kind: 'card', id })}
                  onNewCard={(columnId) => setDialog({ kind: 'newCard', columnId })}
                  onColumn={(id) => setDialog({ kind: 'column', id })}
                  onNewColumn={() => setDialog({ kind: 'newColumn' })}
                  onMove={moveCard}
                />
              ) : null}
              {view === 'board' && drawer && docked ? (
                <aside className="drawer" aria-label={t('drawer.title')} data-pane>
                  <div className="drawer__head">
                    <h2 className="modal__title">{t('drawer.title')}</h2>
                    <button type="button" className="pill" onClick={() => setDrawer(false)}>{t('nav.close')}</button>
                  </div>
                  {drawerBody}
                </aside>
              ) : null}
              {view === 'fatal' ? <Fatal error={fatal} /> : null}
              {view === 'name' ? <NameForm onSave={saveName} /> : null}
              {view === 'home' ? (
                <Home
                  ready={ready}
                  network={network}
                  notice={notice}
                  onReconnect={() => call('Reconnect')}
                  onCreate={create}
                  onJoin={join}
                  onPaste={pasteText}
                />
              ) : null}
              {view === 'joining' ? <Joining board={board} settings={settings} notice={notice} leaving={leaving} onCancel={leave} /> : null}
              {view === 'ended' ? <Ended board={ended} leaving={leaving} onHome={() => setEnded(null)} onRemove={leave} /> : null}
            </div>

            {toasts.length > 0 ? (
              <div className="toasts">
                {toasts.map((toast) => <p key={toast.id} className="toast">{toastText(toast.change)}</p>)}
              </div>
            ) : null}

            {dialog?.kind === 'settings' && settings ? (
              <Modal page title={t('settings.title')} onClose={close}>
                <SettingsPanel
                  settings={settings}
                  info={info}
                  network={network}
                  language={locales.includes(prefs.lang) ? prefs.lang : 'system'}
                  theme={theme}
                  onLanguage={(value) => update({ lang: value })}
                  onTheme={(value) => update({ theme: value })}
                  onName={saveName}
                  onNotch={(value) => call('SetShowInNotch', value).then(setSettings)}
                  onPreview={(value) => call('SetNotchPreview', value).then(setSettings)}
                />
              </Modal>
            ) : null}
          </div>
        </div>

        {view === 'board' && drawer && !docked ? (
          <Modal title={t('drawer.title')} onClose={() => setDrawer(false)}>
            {drawerBody}
          </Modal>
        ) : null}
        {dialogCard ? (
          <Modal sheet title={t('card.edit.title')} onClose={close}>
            <CardSheet
              board={board}
              card={dialogCard}
              onSave={(edit) => call('EditCard', dialogCard.id, edit)}
              onMove={(columnId, index) => call('MoveCard', dialogCard.id, columnId, index)}
              onDelete={() => call('DeleteCard', dialogCard.id)}
              onClose={close}
            />
          </Modal>
        ) : null}
        {dialog?.kind === 'newCard' && shown ? (
          <Modal sheet title={t('card.new.title')} onClose={close}>
            <CardSheet
              board={board}
              columnId={dialog.columnId}
              onAdd={(columnId, input, index) => call('AddCard', columnId, input, index)}
              onClose={close}
            />
          </Modal>
        ) : null}
        {dialogColumn ? (
          <Modal sheet title={t('column.edit.title')} onClose={close}>
            <ColumnSheet
              board={board}
              column={dialogColumn}
              onRename={(title) => call('RenameColumn', dialogColumn.id, title)}
              onMove={(index) => call('MoveColumn', dialogColumn.id, index)}
              onDelete={() => call('DeleteColumn', dialogColumn.id)}
              onClose={close}
            />
          </Modal>
        ) : null}
        {dialog?.kind === 'newColumn' && shown ? (
          <Modal sheet title={t('column.new.title')} onClose={close}>
            <ColumnSheet board={board} onAdd={(title) => call('AddColumn', title, board.columns.length)} onClose={close} />
          </Modal>
        ) : null}
        {dialog?.kind === 'title' && shown ? (
          <Modal sheet title={t('board.rename')} onClose={close}>
            <TitleSheet board={board} onSave={(title) => call('RenameBoard', title)} onClose={close} />
          </Modal>
        ) : null}
        {dialog?.kind === 'invite' && admin ? (
          <Modal sheet title={t('invite.title')} onClose={close}>
            <InviteSheet
              ready={ready}
              network={network}
              onCreate={(options) => call('CreateInvite', options).then((made) => {
                refreshInvites()
                return made
              })}
              onCopy={(code) => copyText(code).then(() => setSpoken(t('invite.copied.live')))}
              onClose={close}
            />
          </Modal>
        ) : null}
        {dialogPerson ? (
          <Modal sheet title={t('remove.title')} onClose={close}>
            <ConfirmSheet
              lines={[t('remove.body', { name: safeText(dialogPerson.name) || t('person.unnamed') }), t('remove.keeps'), t('remove.after'), t('remove.told')]}
              action={t('remove.action')}
              onConfirm={() => call('RemoveMember', dialogPerson.key)}
              onClose={close}
            />
          </Modal>
        ) : null}
        {dialog?.kind === 'leave' && shown ? (
          <Modal sheet title={t('leave.title')} onClose={close}>
            <ConfirmSheet
              lines={[t(`leave.plan.${board?.leave || 'everyone'}`), state === 'removed' ? t('leave.removed') : null].filter(Boolean)}
              action={t('leave.action')}
              onConfirm={leave}
              onClose={close}
            />
          </Modal>
        ) : null}

        <p className="sr-only" role="status" aria-live="polite">{[spoken, ...toasts.map((toast) => toastText(toast.change))].join(' ')}</p>
      </div>
    </I18n.Provider>
  )
}

export default App

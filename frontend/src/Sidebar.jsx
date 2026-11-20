import { useI18n } from './context.js'
import { safeText, shortId } from './format.js'
import { GearIcon, SidebarIcon } from './Icons.jsx'
import { Mark } from './Ring.jsx'
import { Disc } from './parts.jsx'
import { boardLine, networkLine } from './sidebar.js'

/**
 * The sidebar, the full height of the window: the Kolo mark and the button that collapses it to a rail, the board this app
 * holds (one at a time), the network in one quiet line, and your card with the way to Settings.
 */
export function Sidebar({ board, empty, network, me, collapsed, settingsOpen, settingsReady, onToggle, onSelect, onSettings }) {
  const { t } = useI18n()
  const toggle = t(collapsed ? 'side.expand' : 'side.collapse')
  const net = networkLine(network)
  const netText = `${t(net.mode)} · ${t(net.state)}`
  const title = board ? safeText(board.title) || t('board.untitled') : ''
  const line = board ? boardLine(board).map((part) => t(part.key, part.vars)).join(' · ') : ''

  return (
    <nav className={`convs${collapsed ? ' is-collapsed' : ''}`} id="side-panel" aria-label={t('side.label')}>
      <div className="convs__brand">
        <Mark className="convs__logo" />
        <span className="convs__name">kolo</span>
        <button type="button" className="iconbtn iconbtn--side" aria-label={toggle} title={toggle} aria-expanded={!collapsed} aria-controls="side-panel" onClick={onToggle}>
          <SidebarIcon />
        </button>
      </div>
      {board || empty ? (
        <div className="convs__heading">
          <h2 className="sidebar__title">{t('side.title')}</h2>
        </div>
      ) : null}
      {empty ? <p className="note convs__empty">{t('side.none')}</p> : null}
      <ul className="convs__list">
        {board ? (
          <li>
            <button type="button" className="conv is-active" aria-current="true" aria-label={collapsed ? title : undefined} title={title} onClick={onSelect}>
              <Disc name={title} seed={board.id} size={44} />
              <span className="conv__text">
                <span className="conv__name">{title}</span>
                <span className="conv__line">{line}</span>
              </span>
            </button>
          </li>
        ) : null}
      </ul>
      <p className={`convs__net is-${net.tone}`} title={netText} role="status">
        <span className="convs__dot" aria-hidden="true" />
        <span className="convs__text">{netText}</span>
      </p>
      <div className="me">
        {me ? (
          <>
            <Disc name={me.name} seed={me.key} size={40} />
            <span className="me__text">
              <span className="me__name">{me.name || shortId(me.key)}</span>
              <span className="me__key" title={safeText(me.key)}>{shortId(me.key)}</span>
            </span>
          </>
        ) : <span className="me__text" />}
        <button type="button" className="iconbtn iconbtn--side" aria-pressed={settingsOpen} aria-label={t('nav.settings')} title={t('nav.settings')} disabled={!settingsReady} onClick={onSettings}>
          <GearIcon />
        </button>
      </div>
    </nav>
  )
}

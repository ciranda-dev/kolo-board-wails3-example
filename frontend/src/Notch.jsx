import { useCallback, useEffect, useState } from 'react'
import { call, on } from './service.js'
import { htmlLang, pickLocale, translate } from './i18n.js'
import { safeText } from './format.js'
import { changeSentence, fromChange, settledShare } from './board.js'
import { errorSentence } from './errors.js'
import { loadPrefs } from './prefs.js'
import { createGate } from './fresh.js'
import { Ring } from './Ring.jsx'
import { Icon } from './Icons.jsx'
import './board.css'
import './notch.css'

/**
 * The page of the macOS notch window, in one row: the latest change by another member, how many
 * cards are settled and syncing, and how many members are connected. The library draws the notch;
 * this page only fills its 660 x 92 area and shows nothing when there is no board. It reads Notch()
 * and its own event only, so it holds what the notch settings allow and nothing else.
 */
export function Notch() {
  const [locale, setLocale] = useState(() => pickLocale(loadPrefs().lang, window.navigator.languages ?? []))
  const [state, setState] = useState(null)
  const gate = useState(createGate)[0]
  const lang = htmlLang[locale]
  const t = useCallback((key, vars) => translate(locale, key, vars), [locale])

  useEffect(() => {
    document.documentElement.lang = lang
  }, [lang])

  useEffect(() => {
    const off = on('board:notch', (next) => {
      gate.event()
      if (next) {
        setState(next)
      }
    })
    // An event that arrived while Notch() was asked for is newer than its answer.
    const mark = gate.mark()
    call('Notch').then((now) => gate.fresh(mark) && setState(now)).catch(() => {})
    // The main window saves the language; follow it without a reload.
    const onStorage = () => setLocale(pickLocale(loadPrefs().lang, window.navigator.languages ?? []))
    window.addEventListener('storage', onStorage)
    return () => {
      off()
      window.removeEventListener('storage', onStorage)
    }
  }, [gate])

  if (!state?.active) {
    return <main className="notch" aria-label={t('app.title')} />
  }

  const removed = state.state === 'removed'
  const change = state.change ? fromChange(state.change) : null
  const said = change ? changeSentence({ ...change, who: safeText(change.who) || t('person.unnamed'), text: safeText(change.text), column: safeText(change.column) }) : null
  const title = removed ? t('notch.removed') : said ? t(said.key, said.vars) : safeText(state.title) || t('notch.quiet')
  const failure = state.error ? errorSentence(t, locale, state.error) : ''
  const detail = [
    state.unseen > 1 ? t('notch.more', { n: state.unseen - 1 }) : '',
    t('notch.online', { k: state.online, m: state.members }),
    t('sync.count.settled', { n: state.cardsSettled }),
    state.settling ? '' : t('notch.waiting'),
  ].filter(Boolean).join(' · ')

  return (
    <main className="notch" aria-label={t('app.title')}>
      <Ring
        value={settledShare(state.cardsSettled, state.cardsSyncing)}
        peers={Math.max(0, state.online - 1)}
        tone={state.error || removed ? 'failed' : state.settling ? '' : 'muted'}
        label={t('notch.ring', { k: state.online, m: state.members })}
      />
      <div className="notch__text">
        <p className="notch__name" title={title}>{title}</p>
        {failure
          ? <p className="notch__detail is-sentence is-failed" role="status" title={failure}>{failure}</p>
          : <p className="notch__detail" role="status">{detail}</p>}
      </div>
      {removed ? null : (
        <p className="notch__datum" aria-label={t('sync.count.syncing', { n: state.cardsSyncing })}>
          <span className="notch__number" aria-hidden="true">{state.cardsSyncing}</span>
          <span className="notch__unit" aria-hidden="true">{t('notch.syncing')}</span>
        </p>
      )}
      <button type="button" className="btn btn--ghost notch__action" onClick={() => call('NotchOpenApp').catch(() => {})}>{t('notch.open')}</button>
      <button type="button" className="notch__round notch__hide" aria-label={t('notch.hide')} title={t('notch.hide')} onClick={() => call('NotchHide').catch(() => {})}>
        <Icon name="chevron" />
      </button>
    </main>
  )
}

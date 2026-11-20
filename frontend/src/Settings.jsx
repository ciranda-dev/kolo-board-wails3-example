import { useState } from 'react'
import { useI18n } from './context.js'
import { formatWait, nodesToLines, safeText } from './format.js'
import { languageNames, locales } from './i18n.js'
import { relayList, relaySource } from './relays.js'

/**
 * The settings: what a person chooses in the window (name, language, theme, the notch), and what the
 * network runs with, read only. The network is set with flags when Kolo Board starts.
 */
export function SettingsPanel({ settings, info, network, language, theme, onLanguage, onTheme, onName, onNotch, onPreview }) {
  const { t, fail, lang } = useI18n()
  const [name, setName] = useState(settings?.displayName ?? '')
  const [nameError, setNameError] = useState(null)
  const [notchError, setNotchError] = useState(null)
  const [saved, setSaved] = useState(false)
  const wait = (ms) => (ms > 0 ? t('settings.wait.upTo', { time: formatWait(ms, lang) }) : t('settings.wait.none'))
  const linger = settings?.notchLingerMs > 0 ? t('settings.linger.set', { time: formatWait(settings.notchLingerMs, lang) }) : t('settings.linger.none')
  const mode = ['own', 'local'].includes(settings?.mode) ? settings.mode : 'public'
  const from = mode === 'own' && ['flag', 'env', 'first'].includes(settings?.entryNodesFrom) ? settings.entryNodesFrom : ''
  const nodes = nodesToLines(settings?.entryNodes).split('\n').filter(Boolean)
  const relays = relayList(network)
  const relaysFrom = relaySource(settings?.connectionRelaysFrom)

  const saveName = (ev) => {
    ev.preventDefault()
    setNameError(null)
    setSaved(false)
    onName(name)
      .then((next) => {
        setName(next?.displayName ?? name)
        setSaved(true)
      })
      .catch(setNameError)
  }

  return (
    <div className="settings">
      <section className="settings__group" aria-labelledby="settings-you">
        <h3 className="settings__title" id="settings-you">{t('settings.group.you')}</h3>
        <form onSubmit={saveName}>
          <label className="label" htmlFor="settings-name">{t('settings.name')}</label>
          <div className="form__row">
            <input
              id="settings-name"
              className="field"
              value={name}
              onChange={(ev) => setName(ev.target.value)}
              autoComplete="off"
              spellCheck={false}
              aria-invalid={nameError ? 'true' : undefined}
              aria-describedby="settings-name-note"
            />
            <button type="submit" className="btn btn--ghost" id="settings-save-name">{t('settings.name.save')}</button>
          </div>
          {nameError ? <p className="alert alert--plain field-note" role="alert">{fail(nameError)}</p> : null}
          <p className="field-note" id="settings-name-note" role="status">{saved ? t('settings.name.saved') : t('settings.name.note')}</p>
        </form>
        <div className="settings__grid">
          <div>
            <label className="label" htmlFor="settings-language">{t('settings.language')}</label>
            <select id="settings-language" className="field" value={language} onChange={(ev) => onLanguage(ev.target.value)}>
              <option value="system">{t('settings.language.system')}</option>
              {locales.map((code) => (
                <option key={code} value={code}>{languageNames[code]}</option>
              ))}
            </select>
          </div>
          <div>
            <label className="label" htmlFor="settings-theme">{t('settings.theme')}</label>
            <select id="settings-theme" className="field" value={theme} onChange={(ev) => onTheme(ev.target.value)}>
              <option value="system">{t('theme.system')}</option>
              <option value="light">{t('theme.light')}</option>
              <option value="dark">{t('theme.dark')}</option>
            </select>
          </div>
        </div>
      </section>

      <section className="settings__group" aria-labelledby="settings-notch">
        <h3 className="settings__title" id="settings-notch">{t('settings.group.notch')}</h3>
        <div>
          <label className="check">
            <input type="checkbox" id="settings-show-notch" checked={settings?.showInNotch ?? true} onChange={(ev) => onNotch(ev.target.checked).catch(setNotchError)} aria-describedby="settings-notch-note" />
            <span>{t('settings.notch')}</span>
          </label>
          <p className="field-note" id="settings-notch-note">{t('settings.notch.note')} {linger}</p>
        </div>
        <div>
          <label className="check">
            <input type="checkbox" id="settings-notch-preview" checked={settings?.notchPreview ?? true} disabled={!(settings?.showInNotch ?? true)} onChange={(ev) => onPreview(ev.target.checked).catch(setNotchError)} aria-describedby="settings-preview-note" />
            <span>{t('settings.preview')}</span>
          </label>
          <p className="field-note" id="settings-preview-note">{t('settings.preview.note')}</p>
        </div>
        {notchError ? <p className="alert alert--plain" role="alert">{fail(notchError)}</p> : null}
      </section>

      <section className="settings__group" aria-labelledby="settings-network">
        <h3 className="settings__title" id="settings-network">{t('settings.group.network')}</h3>
        <dl className="facts facts--plain">
          <div className="fact fact--wide">
            <dt className="label">{t('settings.mode')}</dt>
            <dd className="fact__value fact__value--text" id="settings-mode">{t(`settings.mode.${mode}`)}</dd>
          </div>
          {mode === 'public' ? null : (
            <div className="fact fact--wide">
              <dt className="label">{t('settings.nodes')}</dt>
              <dd className="fact__value fact__value--text" id="settings-nodes">
                {mode === 'local'
                  ? t('settings.local', { addr: safeText(settings?.localNode) })
                  : <><span className="fact__mono">{nodes.map(safeText).join(', ')}</span>{from ? <>{' '}<span className="fact__source">{t(`settings.from.${from}`)}</span></> : null}</>}
                {settings?.firstNode ? ` ${t('settings.firstNode', { addr: safeText(settings.firstNode) })}` : ''}
              </dd>
            </div>
          )}
          <div className="fact fact--wide">
            <dt className="label">{t('settings.relays')}</dt>
            <dd className="fact__value fact__value--text" id="settings-relays">
              {relays.length > 0 ? (
                <>
                  <span className="fact__source">{t(`settings.relays.from.${relaysFrom}`)}</span>
                  {relays.map((r) => (
                    <span key={r.addr} className="fact__line">
                      <span className="fact__mono">{r.addr}</span>: {t(`relay.state.${r.state}`)}{r.error ? `. ${fail(r.error)}` : ''}
                    </span>
                  ))}
                </>
              ) : t('settings.relays.none')}
            </dd>
          </div>
          <div className="fact">
            <dt className="label">{t('settings.find')}</dt>
            <dd className="fact__value fact__value--text" id="settings-find">{wait(settings?.findTimeoutMs)}</dd>
          </div>
          <div className="fact">
            <dt className="label">{t('settings.answer')}</dt>
            <dd className="fact__value fact__value--text" id="settings-answer">{wait(settings?.joinTimeoutMs)}</dd>
          </div>
          <div className="fact fact--wide">
            <dt className="label">{t('settings.lookup')}</dt>
            <dd className="fact__value fact__value--text" id="settings-lookup">{settings?.lookupIntervalMs > 0 ? t('settings.wait.every', { time: formatWait(settings.lookupIntervalMs, lang) }) : t('settings.none')}</dd>
          </div>
        </dl>
        <p className="field-note">{t('settings.flags')}</p>
        <p className="note settings__about">{t('settings.about', { v: safeText(info?.version) || t('settings.none'), dir: safeText(settings?.dataDir) || t('settings.none'), listen: safeText(settings?.listen) || t('settings.none') })}</p>
      </section>
    </div>
  )
}

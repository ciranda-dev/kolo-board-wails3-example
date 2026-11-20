import { Component, useEffect, useState } from 'react'
import { copyText } from './service.js'
import { htmlLang, pickLocale, translate } from './i18n.js'
import { loadPrefs } from './prefs.js'
import { onCrash, report, reportText } from './crash.js'

const words = () => {
  const locale = pickLocale(loadPrefs().lang, window.navigator.languages ?? [])
  return { t: (key) => translate(locale, key), lang: htmlLang[locale] }
}

/** The error of a window on screen with a Copy button, never a blank window. `onDismiss` when the window may go on. */
export function CrashPane({ text, onDismiss }) {
  const { t, lang } = words()
  const [copied, setCopied] = useState(false)
  return (
    <div className="crash" role="alert" lang={lang}>
      <div className="crash__box">
        <h1 className="crash__title">{t('crash.title')}</h1>
        <p className="crash__note">{t('crash.note')}</p>
        <pre className="crash__text">{text}</pre>
        <div className="crash__actions">
          <button type="button" className="btn" onClick={() => copyText(text).then(() => setCopied(true)).catch(() => {})}>{t(copied ? 'crash.copied' : 'crash.copy')}</button>
          {onDismiss ? <button type="button" className="btn btn--ghost" onClick={onDismiss}>{t('crash.dismiss')}</button> : null}
        </div>
      </div>
    </div>
  )
}

/** Shows an error that happened outside a render (an event handler, a promise) over the window, which may go on. */
function WindowErrors({ children }) {
  const [text, setText] = useState('')
  useEffect(() => onCrash(setText), [])
  return (
    <>
      {children}
      {text ? <CrashPane text={text} onDismiss={() => setText('')} /> : null}
    </>
  )
}

/** The root of a page: a render that throws shows the error pane instead of nothing, and the error goes to the log. */
export class CrashBoundary extends Component {
  constructor(props) {
    super(props)
    this.state = { text: '' }
  }

  static getDerivedStateFromError(error) {
    return { text: reportText(error) }
  }

  componentDidCatch(error, info) {
    const text = reportText(error, info?.componentStack ?? '')
    this.setState({ text })
    report(text)
  }

  render() {
    if (this.state.text) {
      return <CrashPane text={this.state.text} />
    }
    return <WindowErrors>{this.props.children}</WindowErrors>
  }
}

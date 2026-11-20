import { call } from './service.js'
import { reportText } from './report.js'

export { MAX_REPORT, reportText } from './report.js'

/** Sends the error to the Go log (the terminal that started the app). It holds no board text: only the error and the stack. */
export function report(text) {
  return call('ReportError', text).catch(() => {})
}

const listeners = new Set()

/** Calls fn with the text of every error the window did not handle (window.onerror, unhandledrejection). */
export function onCrash(fn) {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

/** Installs the window-wide handlers once: each unhandled error is reported and shown. */
export function installCrashHandlers() {
  const crash = (error) => {
    const text = reportText(error)
    report(text)
    listeners.forEach((fn) => fn(text))
  }
  window.addEventListener('error', (ev) => crash(ev.error ?? ev.message))
  window.addEventListener('unhandledrejection', (ev) => crash(ev.reason))
}

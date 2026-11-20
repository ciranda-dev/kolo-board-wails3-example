import { useEffect, useRef, useState } from 'react'
import { useI18n } from './context.js'
import { initial } from './format.js'
import { avatarColor } from './avatars.js'

/** A button that copies and says so for two seconds. `onCopy` does the copying. */
export function CopyButton({ label, onCopy, className = 'btn btn--wide' }) {
  const { t } = useI18n()
  const [copied, setCopied] = useState(false)
  const timer = useRef(0)
  useEffect(() => () => window.clearTimeout(timer.current), [])
  const copy = () => {
    Promise.resolve(onCopy()).then(() => {
      setCopied(true)
      window.clearTimeout(timer.current)
      timer.current = window.setTimeout(() => setCopied(false), 2000)
    }).catch(() => {})
  }
  return (
    <button type="button" className={className} onClick={copy}>
      {copied ? t('invite.copied') : label}
    </button>
  )
}

/**
 * A person as the first letter of the name in a circle. `dot` adds the mark of a connection; the
 * words for it are next to the name wherever it matters, so the circle itself is decoration.
 */
export function Avatar({ person, dot = false, size = '' }) {
  const former = person && !person.member
  return (
    <span className={`avatar${size ? ` avatar--${size}` : ''}${former ? ' is-former' : ''}${person && person.online === false ? ' is-away' : ''}`} aria-hidden="true">
      {initial(person?.name)}
      {dot && person?.online ? <span className="avatar__dot" /> : null}
    </span>
  )
}

/**
 * Settled or syncing, as a small mark and its word. `quiet` keeps the word for screen readers only. `stuck` says "not
 * syncing" instead: this member was removed, or its changes stopped settling.
 */
export function SyncMark({ syncing, stuck = false, quiet = false }) {
  const { t } = useI18n()
  const word = t(syncing ? (stuck ? 'sync.stuck' : 'sync.syncing') : 'sync.settled')
  return (
    <span className={`syncmark${syncing ? ' is-syncing' : ''}`}>
      <span className="syncmark__dot" aria-hidden="true" />
      <span className={quiet ? 'sr-only' : 'syncmark__word'}>{word}</span>
    </span>
  )
}

/** Whether a media query matches now. */
export function useMedia(query) {
  const [matches, setMatches] = useState(() => window.matchMedia(query).matches)
  useEffect(() => {
    const list = window.matchMedia(query)
    const onChange = () => setMatches(list.matches)
    onChange()
    list.addEventListener('change', onChange)
    return () => list.removeEventListener('change', onChange)
  }, [query])
  return matches
}

/** A round avatar of the sidebar and the header: the first letter of the name on a color of the seed. */
export function Disc({ name, seed, size = 36 }) {
  return (
    <span className="disc" style={{ width: size, height: size, background: avatarColor(seed || name), fontSize: Math.round(size * 0.42) }} aria-hidden="true">
      {initial(name)}
    </span>
  )
}

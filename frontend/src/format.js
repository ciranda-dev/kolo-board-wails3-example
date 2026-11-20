/** Turns whatever a rejected call threw into a string for the UI. */
export function errorText(err) {
  if (err instanceof Error && err.message) {
    return err.message
  }
  if (typeof err === 'string') {
    return err
  }
  if (err && typeof err === 'object' && typeof err.message === 'string') {
    return err.message
  }
  return String(err)
}

/** Entry nodes as the service keeps them (a comma list) to one per line. */
export function nodesToLines(list) {
  return String(list ?? '').split(',').map((s) => s.trim()).filter(Boolean).join('\n')
}

// Controls, format characters (bidi overrides and isolates, zero-width characters), line and
// paragraph separators, private use and lone surrogates: none of them has a visible shape.
const UNSEEN = /[\p{Cc}\p{Cf}\p{Zl}\p{Zp}\p{Co}\p{Cs}]/gu

/**
 * Text that came from another computer or from the system (a display name, a card title, a path, the
 * words of an error), made safe to show: every character that is invisible or that changes the
 * direction of the text around it is replaced by its code in brackets, so a name cannot pass for
 * another one on screen.
 */
export function safeText(value) {
  return String(value ?? '').replace(UNSEEN, (ch) => `[U+${ch.codePointAt(0).toString(16).toUpperCase().padStart(4, '0')}]`)
}

/** A description, which may have line breaks: each line is made safe and the breaks stay. */
export function safeLines(value) {
  return String(value ?? '').split(/\r\n|\r|\n/).map(safeText).join('\n')
}

/** The letter drawn in place of a picture: the first visible character of the safe name. */
export function initial(name) {
  const first = Array.from(safeText(name).trim())[0]
  return first ? first.toLocaleUpperCase() : '?'
}

/** An invite as the service reads it: spaces and line breaks do not count. */
export function cleanInvite(text) {
  return String(text ?? '').replace(/\s+/g, '')
}

/** Whether text can be an invite at all. The service decides the rest. */
export function looksLikeInvite(text) {
  return cleanInvite(text).startsWith('koloinv0')
}

/** The start and the end of a key or an ID, for a line where the whole value does not fit. */
export function shortId(value, head = 8, tail = 4) {
  const text = safeText(value)
  return text.length > head + tail + 1 ? `${text.slice(0, head)}…${tail > 0 ? text.slice(-tail) : ''}` : text
}

/** When an invite ends: the day and the time, in the language of the window. Empty for a bad date. */
export function formatExpiry(value, lang = 'en') {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return ''
  }
  return new Intl.DateTimeFormat(lang, { day: 'numeric', month: 'short', hour: 'numeric', minute: '2-digit' }).format(date)
}

/** A wait of the service in milliseconds as words, in the largest whole unit: "5 seconds", "2 minutes". */
export function formatWait(ms, lang = 'en') {
  const n = Math.max(0, Number(ms) || 0)
  const [unit, size] = n >= 3_600_000 && n % 3_600_000 === 0 ? ['hour', 3_600_000] : n >= 60_000 && n % 60_000 === 0 ? ['minute', 60_000] : ['second', 1000]
  return new Intl.NumberFormat(lang, { style: 'unit', unit, unitDisplay: 'long', maximumFractionDigits: 3 }).format(n / size)
}

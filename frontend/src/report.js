/** The longest report sent to the log: the text bound of a title on the Go side (names.go maxCardTitleRunes). */
export const MAX_REPORT = 8192

/** The words of an error for the log and the pane: its name, message and stack, cut to MAX_REPORT characters. */
export function reportText(error, componentStack = '') {
  const head = error instanceof Error ? `${error.name}: ${error.message}` : `Error: ${String(error)}`
  const stack = error instanceof Error && error.stack ? error.stack : ''
  // A stack starts with the name and the message in most engines: say them once.
  const parts = stack.startsWith(head) ? [stack, componentStack] : [head, stack, componentStack]
  return Array.from(parts.filter(Boolean).join('\n')).slice(0, MAX_REPORT).join('')
}

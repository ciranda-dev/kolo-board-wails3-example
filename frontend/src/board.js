import { safeText } from './format.js'

/** The label colors of a card, as the service names them. "" is no color. */
export const COLORS = ['red', 'orange', 'yellow', 'green', 'blue', 'purple']

/** The fields of a card that can change, in the order of the sheet. */
export const CARD_FIELDS = ['title', 'description', 'assignee', 'color']

/** The steps of a join, in the order they happen. */
export const JOIN_PHASES = ['finding', 'waiting', 'syncing']

/** How long an invite can last, in seconds: an hour, a day (the default), a week, 30 days. */
export const INVITE_TTLS = [3600, 86400, 604800, 2592000]

/** People by their key. */
export function byKey(people) {
  return new Map((people ?? []).map((p) => [p.key, p]))
}

/** The names that more than one person on the list uses: those are shown with the tag of the key. */
export function sharedNames(people) {
  const seen = new Map()
  for (const p of people ?? []) {
    seen.set(p.name, (seen.get(p.name) ?? 0) + 1)
  }
  return new Set([...seen].filter(([, n]) => n > 1).map(([name]) => name))
}

/** A person's name as it is shown: made safe, with the tag when another person has the same name. */
export function personName(person, shared, unnamed = '') {
  if (!person) {
    return unnamed
  }
  const name = safeText(person.name) || unnamed
  return shared?.has(person.name) && person.tag ? `${name} #${safeText(person.tag)}` : name
}

/** The members of a board, you first, then the others in the order of the service. Former members are left out. */
export function membersOf(people) {
  const members = (people ?? []).filter((p) => p.member)
  return [...members.filter((p) => p.you), ...members.filter((p) => !p.you)]
}

/** Where a card is: its column, its place in it, and the card. Null when it is not on the board. */
export function findCard(columns, id) {
  for (const column of columns ?? []) {
    const index = column.cards.findIndex((c) => c.id === id)
    if (index >= 0) {
      return { column, index, card: column.cards[index] }
    }
  }
  return null
}

/**
 * The index MoveCard takes for a drop: the place among the OTHER cards of the target column, where
 * `beforeId` is the card the dropped one lands above ("" or unknown: the bottom).
 */
export function dropIndex(cards, movedId, beforeId) {
  const others = (cards ?? []).filter((c) => c.id !== movedId)
  const at = others.findIndex((c) => c.id === beforeId)
  return at >= 0 ? at : others.length
}

/** Whether a drop would leave the card where it is. */
export function sameSpot(columns, id, columnId, index) {
  const now = findCard(columns, id)
  return Boolean(now) && now.column.id === columnId && now.index === index
}

/**
 * Where a card goes for a move by keyboard: up, down, top or bottom in its column, left or right
 * to the next column at the same height. Null when there is nowhere to go.
 */
export function keyboardMove(columns, id, direction) {
  const now = findCard(columns, id)
  if (!now) {
    return null
  }
  const others = now.column.cards.length - 1
  const at = columns.indexOf(now.column)
  const to = (columnId, index) => (sameSpot(columns, id, columnId, index) ? null : { columnId, index })
  switch (direction) {
    case 'up':
      return now.index > 0 ? to(now.column.id, now.index - 1) : null
    case 'down':
      return now.index < others ? to(now.column.id, now.index + 1) : null
    case 'top':
      return to(now.column.id, 0)
    case 'bottom':
      return to(now.column.id, others)
    case 'left':
    case 'right': {
      const next = columns[at + (direction === 'left' ? -1 : 1)]
      return next ? { columnId: next.id, index: Math.min(now.index, next.cards.length) } : null
    }
    default:
      return null
  }
}

/** The index MoveColumn takes to move a column one place left (-1) or right (1): among the other columns. Null at the edge. */
export function columnMove(columns, id, step) {
  const at = (columns ?? []).findIndex((c) => c.id === id)
  const next = at + step
  return at < 0 || next < 0 || next >= columns.length ? null : next
}

/** Only the fields of a card that changed, as EditCard takes them: null leaves a field as it is. Null when nothing changed. */
export function cardEdit(card, draft) {
  const edit = { title: null, description: null, assignee: null, color: null }
  let changed = false
  for (const field of CARD_FIELDS) {
    if ((draft[field] ?? '') !== (card[field] ?? '')) {
      edit[field] = draft[field] ?? ''
      changed = true
    }
  }
  return changed ? edit : null
}

/**
 * The sentence about settling, as a copy key and its numbers: stopped for good, waiting for
 * settlers, adding settlers, one settler, or nothing to say.
 */
export function syncNote(sync) {
  if (!sync) {
    return null
  }
  if (sync.halted) {
    return { key: 'sync.halted', tone: 'error' }
  }
  if (!sync.settling) {
    return { key: 'sync.waiting', vars: { n: sync.quorum, m: sync.seats, k: sync.seatsOnline }, tone: 'waiting' }
  }
  if (sync.pendingSeats > 0) {
    return { key: 'sync.adding', tone: 'plain' }
  }
  if (sync.singleSettler) {
    return { key: 'sync.single', tone: 'plain' }
  }
  return null
}

/** The share of the cards that are settled, 0 to 1 (1 for a board without cards). */
export function settledShare(settled, syncing) {
  const total = (Number(settled) || 0) + (Number(syncing) || 0)
  return total > 0 ? (Number(settled) || 0) / total : 1
}

const CARD_KINDS = ['card-added', 'card-edited', 'card-moved', 'card-deleted']
const NO_TEXT = ['board-created', 'name-changed', 'joined', 'removed']

/**
 * A change as the "board:change" event and the notch carry it, in the shape of an activity item:
 * `text` is the card title for a card, else the column title or the new board title.
 */
export function fromChange(change) {
  const card = CARD_KINDS.includes(change?.kind)
  return { kind: change?.kind ?? '', who: change?.who ?? '', text: card ? change?.card : change?.column, column: card ? change?.column : '' }
}

/**
 * The copy key of a change and what goes in it: `who` and `name` are names, `text` and `column`
 * are board text, `fields` the edited fields already in words, `n` the cards a column took with it.
 * Without board text (the notch with the preview off, or a title with nothing visible) it is the
 * plain form, which names no card and no column.
 */
export function changeSentence({ kind, who, name, text, column, fields, n }) {
  const vars = { who, name, text, column, fields, n }
  if (!NO_TEXT.includes(kind) && !text) {
    return { key: `plain.${kind}`, vars }
  }
  if ((kind === 'card-added' || kind === 'card-moved') && !column) {
    return { key: `change.${kind}.nocolumn`, vars }
  }
  if (kind === 'card-edited' && fields) {
    return { key: 'change.card-edited.fields', vars }
  }
  if (kind === 'column-deleted') {
    return n > 0 ? { key: 'change.column-deleted.cards', vars } : { key: 'change.column-deleted', vars }
  }
  return { key: `change.${kind}`, vars }
}

/** A board as the window reads it: every list is a list, also when a payload holds null for one. */
export function boardLists(b) {
  if (!b) {
    return b
  }
  return {
    ...b,
    people: Array.isArray(b.people) ? b.people : [],
    requests: Array.isArray(b.requests) ? b.requests : [],
    columns: (Array.isArray(b.columns) ? b.columns : []).map((c) => ({ ...c, cards: Array.isArray(c.cards) ? c.cards : [] })),
  }
}

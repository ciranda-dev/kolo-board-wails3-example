// A stand-in for the Go service, for design work in a browser: `npm run dev`, then open
// http://127.0.0.1:9245/?mock (a live simulation) or ?mock=<scene> (one frozen state).
// It has the shape of the generated bindings plus the two runtime objects the window uses.
// src/service.js only loads it in a dev server, so a production build never includes it.
// It reads no board and no network: everything here is sample data.

interface BoardError {
  kind: string
  reason: string
  detail: string
  cause: string
  message: string
}

interface Person {
  key: string
  name: string
  tag: string
  you: boolean
  member: boolean
  admin: boolean
  seat: boolean
  online: boolean
  syncing: boolean
}

interface Card {
  id: string
  title: string
  description: string
  assignee: string
  color: string
  createdBy: string
  lastBy: string
  syncing: boolean
}

interface Column {
  id: string
  title: string
  syncing: boolean
  cards: Card[]
}

interface SyncInfo {
  pending: number
  cardsSyncing: number
  cardsSettled: number
  columnsSyncing: number
  seats: number
  quorum: number
  seatsOnline: number
  settling: boolean
  pendingSeats: number
  singleSettler: boolean
  halted: BoardError | null
}

interface Board {
  id: string
  state: 'creating' | 'joining' | 'active' | 'removed' | 'ended'
  phase: string
  title: string
  self: string
  columns: Column[]
  people: Person[]
  requests: { request: string; invite: string; joiner: string; name: string; tag: string }[]
  sync: SyncInfo
  canEdit: boolean
  blocked: BoardError | null
  unseen: number
  error: BoardError | null
  seq?: number
}

interface InviteInfo {
  id: string
  code?: string
  created: string
  expires: string
  uses: number
  used: number
  requireApproval: boolean
  state: 'active' | 'expired' | 'used up' | 'revoked'
}

interface ActivityItem {
  seq: number
  kind: string
  who: string
  subject: string
  text: string
  column: string
  fields: string[]
  cards: number
  settled: boolean
}

const query = new URLSearchParams(window.location.search)
const scene = query.get('mock') || 'live'
const frozen = scene !== 'live'
const now = Date.now()

let counter = 0x1000
const hex = (n: number) => n.toString(16).padStart(8, '0')
// IDs and keys are 64 hex digits, as the service makes them.
const id64 = (seed: number) => Array.from({ length: 8 }, (_, i) => hex((seed * 2654435761 + i * 40503) >>> 0)).join('')
const nextId = () => id64(++counter)

// Every sentence of the service, by reason, so any error can be put on screen: ?mock=ended-<reason>.
const REASONS: Record<string, [string, string]> = {
  cancelled: ['cancelled', 'Cancelled.'],
  'invalid-invite': ['invalid-invite', 'This is not a board invite. Copy the whole invite, it starts with koloinv0.'],
  'inviter-not-found': ['not-found', 'Could not find the person who invited you. They must be online with Kolo Board open and use the same entry nodes.'],
  'inviter-lost': ['not-found', 'The connection to the person who invited you ended before they answered. Try again: asking twice is safe.'],
  'join-denied': ['refused', 'The invite was not accepted: the person who made it said no, or it is not theirs to give. Ask for a new invite.'],
  'invite-expired': ['expired', 'This invite has expired. Ask for a new one.'],
  'invite-used-up': ['expired', 'This invite has no uses left. Ask for a new one.'],
  'invite-revoked': ['expired', 'This invite was revoked. Ask for a new one.'],
  'join-sync-timeout': ['sync', 'You were let in, but the board did not arrive in time. Ask for a new invite and try again.'],
  'board-exists': ['busy', 'You have a board already. Leave it first to create or join another one.'],
  'busy-joining': ['busy', 'A join is in progress. Wait for it or cancel it first.'],
  'no-board': ['busy', 'You have no board. Create one or join one with an invite.'],
  'network-not-ready': ['network', 'The network is not ready yet. Wait a moment, or press Try again if it failed.'],
  'public-unreachable': ['network', 'Could not reach the public network: {cause}. Check your internet connection and press Try again. Or choose another network: start Kolo Board with -bootstrap and the entry nodes of your own network, or with -local to use this computer only.'],
  'bootstrap-failed': ['network', 'Could not reach the entry nodes ({detail}): {cause}. Check that they run and that -bootstrap names them. Or start Kolo Board without -bootstrap for the public network, or with -local to use this computer only.'],
  'local-port-taken': ['network', 'Another program uses {detail} on this computer, so the local network cannot start. Close that program and press Try again, or start Kolo Board without -local for the public network, or with -bootstrap and the entry nodes of your own network.'],
  'name-empty': ['name', 'Write a name other members will see.'],
  'name-too-long': ['name', 'The name is too long. Use at most {detail} characters.'],
  'board-title-empty': ['text', 'Write a title for the board.'],
  'board-title-too-long': ['text', 'The board title is too long. Use at most {detail} characters.'],
  'column-title-empty': ['text', 'Write a title for the column.'],
  'column-title-too-long': ['text', 'The column title is too long. Use at most {detail} characters.'],
  'card-title-empty': ['text', 'Write a title for the card.'],
  'card-title-too-long': ['text', 'The card title is too long. Use at most {detail} characters.'],
  'column-not-found': ['not-found', 'That column is not on the board any more. Someone may have deleted it.'],
  'card-not-found': ['not-found', 'That card is not on the board any more. Someone may have deleted it.'],
  'request-not-found': ['not-found', 'That request is gone: the person left or the invite ended.'],
  'reorder-no-room': ['limit', 'There is no room to put it there. Move it to the start or the end instead.'],
  'invite-uses-range': ['limit', 'An invite takes 1 to {detail} people.'],
  'invite-ttl-range': ['limit', 'Choose how long the invite lasts: at least 1 second.'],
  'activity-limit': ['limit', 'Ask for at least 1 item.'],
  'invite-not-found': ['not-found', 'That invite does not exist.'],
  'member-not-found': ['not-found', 'That person is not a member of the board.'],
  'not-admin': ['permission', 'Only an admin can do this.'],
  'last-admin': ['permission', 'The last admin cannot be removed. Make someone else an admin first.'],
  'you-were-removed': ['ended', 'You were removed from this board. You keep what you had received; nothing new reaches you.'],
  'strand-unconfirmed': ['sync', 'Waiting for another member to be online before you can edit. Your last changes are kept.'],
  'strand-lost': ['sync', 'Another copy of your data folder wrote to this board. This copy can no longer edit it. Never copy or run a copy of the data folder.'],
  'board-halted': ['sync', 'The board stopped on this computer: {cause}. Restart Kolo Board to try again.'],
  'settlement-halted': ['sync', 'A member signed two histories of its changes. Changes still reach everyone, but they cannot be settled any more.'],
  'board-open': ['sync', 'Could not open the board: {cause}.'],
  'data-dir-open': ['config', 'The data directory {detail} is open to other users. Make it private (chmod 700) or choose another one.'],
  internal: ['internal', 'Something went wrong: {cause}.'],
}

function boardErr(reason: string, detail = '', cause = ''): BoardError {
  const [kind, text] = REASONS[reason] ?? ['internal', `Something went wrong: ${reason}.`]
  return { kind, reason, detail, cause, message: text.replace('{detail}', detail).replace('{cause}', cause) }
}

function reject(error: BoardError): never {
  throw Object.assign(new Error(error.message), { cause: error })
}

// Names a hostile member could use: a right-to-left override that turns one name into another,
// zero-width characters, text that looks like markup, and a name as wide as it may be.
const SPOOF_NAME = 'Marta ‮ariervilO'
const HOSTILE_NAME = '<img src=x onerror=alert(1)>​⁦x⁩﻿'
const WIDE_NAME = 'WWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWWW'

const person = (seed: number, name: string, extra: Partial<Person> = {}): Person => {
  const key = id64(seed)
  return { key, name, tag: key.slice(0, 4), you: false, member: true, admin: true, seat: true, online: true, syncing: false, ...extra }
}
const ANA = person(1, 'Ana Ribeiro', { you: true })
const MARTA = person(2, 'Marta Oliveira')
const TOMAS = person(3, 'Tomás Herrera')
const YUKI = person(4, 'Yuki Tanaka', { admin: false })
const CHIDI = person(5, 'Chidi Okafor', { member: false, admin: false, seat: false, online: false })

const card = (title: string, who: Person | null, color = '', extra: Partial<Card> = {}): Card => ({
  id: nextId(), title, description: '', assignee: who?.key ?? '', color, createdBy: ANA.key, lastBy: who?.key ?? ANA.key, syncing: false, ...extra,
})

function sampleColumns(): Column[] {
  return [
    { id: nextId(), title: 'Backlog', syncing: false, cards: [
      card('Write the 2.4 release notes', ANA, 'blue', { description: 'Start from the changelog. Keep the list of fixes short and name the two breaking changes at the top.' }),
      card('Translate the new settings page to Spanish', YUKI, 'purple'),
      card('Decide the rollout order for Windows', null),
      card('Update the screenshots in the user guide', CHIDI, 'yellow', { createdBy: CHIDI.key, description: 'The export dialog and the sidebar changed.' }),
    ] },
    { id: nextId(), title: 'Doing', syncing: false, cards: [
      card('Fix the crash when the import file is empty', TOMAS, 'red', { syncing: true, description: 'Reported twice this week. The parser reads the header before checking the length.' }),
      card('Migration for the saved filters table', MARTA, 'orange', { createdBy: MARTA.key }),
      card('Keyboard shortcuts for the editor', ANA, 'blue'),
      card('Shrink the installer under 40 MB', TOMAS, '', { syncing: true, lastBy: MARTA.key }),
    ] },
    { id: nextId(), title: 'Review', syncing: false, cards: [
      card('Dark theme contrast on the export dialog', YUKI, 'purple', { createdBy: YUKI.key }),
      card('Rate limit for the public API', MARTA, 'orange', { syncing: true, description: '60 requests a minute per key, with a clear error.' }),
      card('Offline banner copy', ANA),
    ] },
    { id: nextId(), title: 'Done', syncing: false, cards: [
      card('Bump the version to 2.4.0', MARTA, '', { createdBy: MARTA.key }),
      card('Freeze the translation strings', YUKI, 'purple'),
      card('Smoke test on macOS 15', TOMAS, 'green', { createdBy: TOMAS.key }),
    ] },
  ]
}

function countSync(b: Board): Board {
  const cards = b.columns.flatMap((c) => c.cards)
  const syncing = cards.filter((c) => c.syncing).length
  const seats = b.people.filter((p) => p.member && p.seat)
  const online = seats.filter((p) => p.online).length
  const quorum = seats.length <= 1 ? 1 : seats.length === 3 ? 2 : Math.min(3, seats.length)
  const settling = online >= quorum && !b.sync.halted
  if (!settling) {
    cards.forEach((c) => { c.syncing = true })
  }
  return { ...b, sync: { ...b.sync, cardsSyncing: settling ? syncing : cards.length, cardsSettled: settling ? cards.length - syncing : 0, columnsSyncing: b.columns.filter((c) => c.syncing).length, pending: settling ? syncing + b.columns.filter((c) => c.syncing).length : cards.length + 3, seats: seats.length, quorum, seatsOnline: online, settling, singleSettler: seats.length === 1 } }
}

const NO_SYNC: SyncInfo = { pending: 0, cardsSyncing: 0, cardsSettled: 0, columnsSyncing: 0, seats: 1, quorum: 1, seatsOnline: 1, settling: true, pendingSeats: 0, singleSettler: true, halted: null }

function makeBoard(patch: Partial<Board> = {}): Board {
  return countSync({
    id: id64(77), state: 'active', phase: '', title: 'Release 2.4', self: ANA.key, columns: sampleColumns(), people: [ANA, MARTA, TOMAS, YUKI, CHIDI].map((p) => ({ ...p })),
    requests: [], sync: { ...NO_SYNC }, canEdit: true, blocked: null, unseen: 0, error: null, ...patch,
  })
}

const REQUESTS = [
  { request: '41', invite: id64(900), joiner: id64(21), name: 'Léa Fontaine', tag: id64(21).slice(0, 4) },
  { request: '42', invite: id64(900), joiner: id64(22), name: 'Marta Oliveira', tag: id64(22).slice(0, 4) },
]

function sceneBoard(): Board | null {
  if (scene.startsWith('ended-') || ['name', 'home', 'starting', 'local', 'net-failed', 'net-public-failed', 'fatal', 'flag'].includes(scene)) {
    return null
  }
  const joining = (phase: string): Board => ({ ...makeBoard({ columns: [], people: [{ ...ANA }] }), id: '', state: 'joining', phase, title: '' })
  const others = (patch: Partial<Person>) => [ANA, MARTA, TOMAS, YUKI, CHIDI].map((p) => (p.you || !p.member ? { ...p } : { ...p, ...patch }))
  switch (scene) {
    case 'joining-finding': return joining('finding')
    case 'joining-waiting': return joining('waiting')
    case 'joining-syncing': return joining('syncing')
    case 'board-empty': return makeBoard({ title: 'Team week', people: [{ ...ANA }], columns: ['To do', 'Doing', 'Done'].map((title) => ({ id: nextId(), title, syncing: false, cards: [] })) })
    case 'board-no-columns': return makeBoard({ title: 'Team week', people: [{ ...ANA }], columns: [] })
    case 'board-waiting': return makeBoard({ people: [{ ...ANA }, { ...MARTA, online: false }, { ...TOMAS, online: false }, { ...YUKI }, { ...CHIDI }] })
    case 'board-adding': { const b = makeBoard({ people: [{ ...ANA }, { ...MARTA, seat: false }, { ...TOMAS, seat: false, syncing: true }, { ...CHIDI }] }); return { ...b, sync: { ...b.sync, pendingSeats: 2 } } }
    case 'board-requests': return makeBoard({ requests: REQUESTS })
    case 'board-member': return makeBoard({ self: YUKI.key, people: [ANA, MARTA, TOMAS, YUKI, CHIDI].map((p) => ({ ...p, you: p.key === YUKI.key })), requests: [REQUESTS[0]] })
    case 'board-removed': return { ...makeBoard(), state: 'removed', canEdit: false, blocked: boardErr('you-were-removed') }
    case 'board-blocked': return makeBoard({ canEdit: false, blocked: boardErr('strand-unconfirmed'), people: others({ online: false }) })
    case 'board-lost': return makeBoard({ canEdit: false, blocked: boardErr('strand-lost') })
    case 'board-halted': { const b = makeBoard(); return countSync({ ...b, sync: { ...b.sync, halted: boardErr('settlement-halted') } }) }
    case 'board-error': return makeBoard({ canEdit: false, error: boardErr('board-halted', '', 'the store is full') })
    case 'board-net-failed':
    case 'board-toast':
    case 'board-same-names': {
      const b = makeBoard()
      if (scene === 'board-same-names') {
        b.people[1].name = 'Ana Ribeiro'
      }
      return b
    }
    case 'board-hostile': {
      const b = makeBoard()
      b.people[1].name = SPOOF_NAME
      b.people[2].name = HOSTILE_NAME
      b.people[3].name = WIDE_NAME
      b.title = 'Release ‮4.2 <b>bold</b>'
      b.columns[0].title = 'Back​log ‮gol'
      b.columns[1].cards[0].title = `${HOSTILE_NAME} Pneumonoultramicroscopicsilicovolcanoconiosisandthenanotherverylongwordwithoutanyspaces`
      b.columns[1].cards[0].description = 'line one\nline two ‮evil separator'
      b.columns[1].cards[1].title = ''
      b.requests = [{ ...REQUESTS[0], name: SPOOF_NAME }]
      return b
    }
    case 'board-many': {
      const b = makeBoard()
      const more = ['Design', 'Blocked', 'Next release'].map((title, i) => ({ id: nextId(), title, syncing: i === 1, cards: Array.from({ length: i === 0 ? 2 : 1 }, (_, k) => card(`${title} item ${k + 1}`, [MARTA, TOMAS, YUKI][k % 3])) }))
      b.columns[0].cards.push(...Array.from({ length: 30 }, (_, k) => card(`Triage report ${k + 101}`, [ANA, MARTA, TOMAS, YUKI][k % 4], ['', 'red', 'orange'][k % 3])))
      return countSync({ ...b, columns: [...b.columns, ...more] })
    }
    // A payload no window can draw: the error pane shows instead of a blank window.
    case 'crash': return { ...makeBoard(), sync: null as unknown as SyncInfo }
    default: return scene === 'live' ? null : makeBoard()
  }
}

type Handler = (event: { name: string; data: unknown }) => void
const listeners = new Map<string, Set<Handler>>()
function emit(name: string, data: unknown) {
  listeners.get(name)?.forEach((fn) => fn({ name, data }))
}

export const Events = {
  On(name: string, fn: Handler) {
    if (!listeners.has(name)) {
      listeners.set(name, new Set())
    }
    listeners.get(name)!.add(fn)
    return () => listeners.get(name)?.delete(fn)
  },
}

let clipboard = 'koloinv0qx7Zt2mVb9LkR4sWd8YcA1fGh6JnP3uTe5XoBi0MyKvNrCa7DwEzQ2lHs4FgUj9pSx6OtVb1ImYk8ZcRd3WnAe5LqTf0GhJu2MoPv7XyBi4KsNz6CaDwEr9lHt1FgUj3pSx8Ot'
export const Clipboard = {
  async SetText(text: string) {
    clipboard = text
  },
  async Text() {
    return clipboard
  },
}

// ?mock=flag and ?mock=board-flag are a network of your own, ?mock=local and ?mock=board-local are this computer only.
const own = scene === 'flag' || scene === 'board-flag'
const local = scene === 'local' || scene === 'board-local'
// ?limits: the app was started with -find-timeout, -join-timeout and -notch-linger.
const limits = query.has('limits') || scene === 'flag'
let settings = {
  // The sample network is the public one.
  mode: own ? 'own' : local ? 'local' : 'public',
  entryNodes: local ? '' : own ? '203.0.113.10:4000,203.0.113.11:4000' : '',
  entryNodesFrom: own ? 'flag' : local ? 'local' : 'public',
  firstNode: '',
  localNode: local ? '127.0.0.1:7451' : '',
  dataDir: '/Users/ana/Library/Application Support/kolo-board',
  listen: '0.0.0.0:0',
  showInNotch: query.get('notch') !== 'off',
  notchPreview: query.get('preview') !== 'off',
  displayName: scene === 'name' ? '' : 'Ana Ribeiro',
  findTimeoutMs: limits ? 120_000 : 0,
  joinTimeoutMs: limits ? 600_000 : 0,
  lookupIntervalMs: 5000,
  notchLingerMs: limits ? 8000 : 0,
}

// The service gives every network state and every board read a larger seq.
let netSeq = 0
let boardSeq = 0
const stamp = (b: Board | null) => (b ? { ...b, seq: ++boardSeq } : null)

let network = { mode: settings.mode, state: 'ready', message: '', error: null as BoardError | null, entryNodes: local ? 1 : 2, firstNode: '', localNode: settings.localNode, seq: 0 }
if (scene === 'starting' || scene === 'race') {
  network = { ...network, state: 'starting' }
} else if (scene === 'net-failed' || scene === 'board-net-failed') {
  const error = boardErr('bootstrap-failed', '203.0.113.10:4000', 'no answer after 10 s')
  network = { ...network, mode: 'own', state: 'failed', message: error.message, error }
} else if (scene === 'net-public-failed') {
  const error = boardErr('public-unreachable', '', 'no answer after 10 s')
  network = { ...network, state: 'failed', message: error.message, error }
}
network.seq = ++netSeq
function setNetwork(patch: Partial<typeof network>) {
  network = { ...network, ...patch, seq: ++netSeq }
  emit('board:network', network)
}

let board: Board | null = sceneBoard()
const endedReason = scene.startsWith('ended-') ? scene.slice(6) : ''
let ended: Board | null = endedReason ? { ...makeBoard({ columns: [], people: [] }), state: 'ended', title: '', error: boardErr(endedReason, endedReason === 'board-open' ? '' : '30', 'the store is damaged') } : null

let invites: InviteInfo[] = scene === 'board-empty' || scene === 'board-no-columns' ? [] : [
  { id: id64(900), created: new Date(now - 3600e3).toISOString(), expires: new Date(now + 20 * 3600e3).toISOString(), uses: 3, used: 1, requireApproval: true, state: 'active' },
  { id: id64(901), created: new Date(now - 7200e3).toISOString(), expires: new Date(now + 6 * 86400e3).toISOString(), uses: 1, used: 0, requireApproval: false, state: 'active' },
  { id: id64(902), created: new Date(now - 3 * 86400e3).toISOString(), expires: new Date(now - 2 * 86400e3).toISOString(), uses: 2, used: 2, requireApproval: true, state: 'used up' },
]

let activity: ActivityItem[] = []
function seedActivity() {
  if (!board || board.columns.length === 0) {
    return
  }
  const cols = board.columns
  const c = (i: number, k: number) => cols[i]?.cards[k]
  const rows: Partial<ActivityItem>[] = [
    { kind: 'board-created', who: ANA.key },
    { kind: 'column-added', who: ANA.key, text: 'Backlog' },
    { kind: 'joined', who: ANA.key, subject: MARTA.key },
    { kind: 'joined', who: ANA.key, subject: TOMAS.key },
    { kind: 'card-added', who: MARTA.key, text: c(3, 0)?.title, column: 'Doing' },
    { kind: 'joined', who: MARTA.key, subject: CHIDI.key },
    { kind: 'card-added', who: CHIDI.key, text: c(0, 3)?.title, column: 'Backlog' },
    { kind: 'board-renamed', who: ANA.key, text: board.title },
    { kind: 'joined', who: MARTA.key, subject: YUKI.key },
    { kind: 'removed', who: ANA.key, subject: CHIDI.key },
    { kind: 'name-changed', who: TOMAS.key, text: 'Tomás Herrera' },
    { kind: 'column-renamed', who: YUKI.key, text: 'Review' },
    { kind: 'column-deleted', who: MARTA.key, text: 'Ideas', cards: 2 },
    { kind: 'card-moved', who: MARTA.key, text: c(3, 0)?.title, column: 'Done' },
    { kind: 'card-edited', who: YUKI.key, text: c(2, 0)?.title, fields: ['title', 'color'] },
    { kind: 'card-deleted', who: TOMAS.key, text: 'Old installer script' },
    { kind: 'card-moved', who: TOMAS.key, text: c(3, 2)?.title, column: 'Done' },
    { kind: 'card-edited', who: MARTA.key, text: c(2, 1)?.title, fields: ['description'], settled: false },
    { kind: 'card-edited', who: MARTA.key, text: c(1, 3)?.title, fields: ['assignee'], settled: false },
    { kind: 'card-moved', who: TOMAS.key, text: c(1, 0)?.title, column: 'Doing', settled: false },
  ]
  activity = rows.map((r, i) => ({ seq: i + 1, kind: '', who: '', subject: '', text: '', column: '', fields: [], cards: 0, settled: board!.sync?.settling, ...r, ...(board!.sync?.settling ? {} : { settled: false }) }) as ActivityItem)
}
seedActivity()

let lastChange: { kind: string; who: string; whoKey: string; card: string; column: string } | null =
  board && board.state === 'active' && query.get('change') !== 'none' && board.people.length > 1
    ? { kind: 'card-moved', who: board.people[2]?.name ?? 'Tomás Herrera', whoKey: TOMAS.key, card: board.columns[1]?.cards[0]?.title ?? '', column: 'Doing' }
    : null
if (board && lastChange && query.get('unseen')) {
  board.unseen = Number(query.get('unseen'))
}

function notchState() {
  const empty = { active: false, state: '', title: '', members: 0, online: 0, cardsSettled: 0, cardsSyncing: 0, pending: 0, settling: false, unseen: 0, change: null, error: null as BoardError | null }
  if (!board || !settings.showInNotch || (board.state !== 'active' && board.state !== 'removed')) {
    return empty
  }
  const members = board.people.filter((p) => p.member)
  const preview = settings.notchPreview
  return {
    active: true, state: board.state, title: preview ? board.title : '', members: members.length, online: members.filter((p) => p.online).length,
    cardsSettled: board.sync.cardsSettled, cardsSyncing: board.sync.cardsSyncing, pending: board.sync.pending, settling: board.sync.settling, unseen: lastChange ? Math.max(1, board.unseen) : 0,
    change: lastChange && board.state === 'active' ? { kind: lastChange.kind, who: lastChange.who, card: preview ? lastChange.card : '', column: preview ? lastChange.column : '' } : null,
    error: board.error ?? (board.state === 'removed' ? boardErr('you-were-removed') : null),
  }
}

function publish() {
  if (board) {
    board = countSync(board)
  }
  emit('board:state', stamp(board))
  emit('board:notch', notchState())
}

const pause = (ms: number) => new Promise((resolve) => window.setTimeout(resolve, ms))

// In the live simulation a change settles a moment later, as it does with enough settlers online.
function settleSoon() {
  if (frozen) {
    return
  }
  window.setTimeout(() => {
    if (board?.state === 'active') {
      board.columns.forEach((c) => { c.syncing = false; c.cards.forEach((k) => { k.syncing = false }) })
      activity.forEach((a) => { a.settled = true })
      publish()
    }
  }, 1800)
}

function record(kind: string, text = '', column = '', extra: Partial<ActivityItem> = {}) {
  activity.push({ seq: activity.length + 1, kind, who: board!.self, subject: '', text, column, fields: [], cards: 0, settled: false, ...extra })
}

function editable(): Board {
  if (!board || board.state === 'joining') {
    reject(boardErr(board ? 'busy-joining' : 'no-board'))
  }
  if (board!.state === 'removed') {
    reject(boardErr('you-were-removed'))
  }
  if (!board!.canEdit && board!.blocked) {
    reject(board!.blocked)
  }
  return board!
}

const clean = (text: string) => String(text ?? '').replace(/[\p{Cc}\p{Cf}]/gu, '').replace(/\s+/g, ' ').trim()
function title(text: string, what: 'board' | 'column' | 'card', max: number) {
  const value = clean(text)
  if (!value) {
    reject(boardErr(`${what}-title-empty`))
  }
  if (Array.from(value).length > max) {
    reject(boardErr(`${what}-title-too-long`, String(max)))
  }
  return value
}

let started = false
// ?mock=race: something changes after Start, Board, Invites or Notch read their answer, and its event
// arrives before that answer, so the answer is older than what the window already has. Each change
// happens once, so the double effects of React's StrictMode in a dev server race too.
async function raced<T>(answer: T, change: () => void): Promise<T> {
  window.setTimeout(change, 100)
  await pause(300)
  return answer
}
const info = () => ({ dataDir: settings.dataDir, version: '0.1.0', network, keyId: ANA.key })

export async function Start() {
  if (scene === 'fatal') {
    reject(boardErr('data-dir-open', settings.dataDir))
  }
  if (scene === 'race') {
    return raced(info(), () => network.state === 'starting' && setNetwork({ state: 'ready' }))
  }
  if (scene === 'board-toast' && !started) {
    started = true
    window.setTimeout(() => {
      emit('board:change', { kind: 'card-moved', who: 'Tomás Herrera', whoKey: TOMAS.key, card: 'Fix the crash when the import file is empty', column: 'Doing' })
      emit('board:change', { kind: 'column-added', who: 'Marta Oliveira', whoKey: MARTA.key, card: '', column: 'Blocked' })
    }, 400)
  }
  return info()
}

export async function Info() {
  return info()
}

export async function Settings() {
  return settings
}

export async function Reconnect() {
  setNetwork({ state: 'starting', message: '', error: null })
  if (!frozen) {
    window.setTimeout(() => setNetwork({ state: 'ready' }), 1500)
  }
}

export async function SetDisplayName(name: string) {
  const value = clean(name)
  if (!value) {
    reject(boardErr('name-empty'))
  }
  if (Array.from(value).length > 48) {
    reject(boardErr('name-too-long', '48'))
  }
  settings = { ...settings, displayName: value }
  if (board) {
    board.people = board.people.map((p) => (p.you ? { ...p, name: value } : p))
    publish()
  }
  return settings
}

export async function SetShowInNotch(on: boolean) {
  settings = { ...settings, showInNotch: on }
  emit('board:notch', notchState())
  return settings
}

export async function SetNotchPreview(on: boolean) {
  settings = { ...settings, notchPreview: on }
  emit('board:notch', notchState())
  return settings
}

export async function Board() {
  const answer = stamp(board ?? ended)
  if (scene === 'race' && board) {
    return raced(answer, () => {
      if (board && board.title !== 'Release 2.4 final') {
        board.title = 'Release 2.4 final'
        publish()
      }
    })
  }
  return answer
}

export async function MarkSeen() {
  if (board && board.unseen > 0 && !frozen) {
    board.unseen = 0
    lastChange = null
    publish()
  }
}

export async function CreateBoard(boardTitle: string, columnTitles: string[] | null) {
  if (!settings.displayName) {
    reject(boardErr('name-empty'))
  }
  if (board) {
    reject(boardErr(board.state === 'joining' ? 'busy-joining' : 'board-exists'))
  }
  const value = title(boardTitle, 'board', 48)
  const names = (columnTitles?.length ? columnTitles : ['To do', 'Doing', 'Done']).map((t) => title(t, 'column', 48))
  ended = null
  board = makeBoard({ id: nextId(), title: value, people: [{ ...ANA, name: settings.displayName }], columns: names.map((t) => ({ id: nextId(), title: t, syncing: false, cards: [] })) })
  invites = []
  activity = [{ seq: 1, kind: 'board-created', who: ANA.key, subject: '', text: value, column: '', fields: [], cards: 0, settled: true }]
  publish()
  return stamp(board)
}

async function runJoin() {
  for (const phase of ['waiting', 'syncing']) {
    await pause(1600)
    if (board?.state !== 'joining') {
      return
    }
    board = { ...board, phase }
    // ?nulls: the payloads of a joiner before its roster arrives, as kolo-board 0.1.0 sent them (people: null).
    if (query.has('nulls') && phase === 'syncing') {
      emit('board:state', { ...stamp(board), people: null })
      continue
    }
    publish()
  }
  await pause(1600)
  if (board?.state !== 'joining') {
    return
  }
  board = makeBoard({ self: YUKI.key, people: [ANA, MARTA, TOMAS, YUKI, CHIDI].map((p) => ({ ...p, you: p.key === YUKI.key, name: p.key === YUKI.key ? settings.displayName : p.name })) })
  seedActivity()
  publish()
}

export async function JoinBoard(invite: string) {
  if (!String(invite).replace(/\s+/g, '').startsWith('koloinv0')) {
    reject(boardErr('invalid-invite'))
  }
  if (!settings.displayName) {
    reject(boardErr('name-empty'))
  }
  if (board) {
    reject(boardErr(board.state === 'joining' ? 'busy-joining' : 'board-exists'))
  }
  if (network.state !== 'ready') {
    reject(boardErr('network-not-ready'))
  }
  ended = null
  board = { ...makeBoard({ columns: [], people: [{ ...ANA, name: settings.displayName }] }), id: '', state: 'joining', phase: 'finding', title: '' }
  publish()
  if (!frozen) {
    runJoin()
  }
  return stamp(board)
}

export async function LeaveBoard() {
  if (!board && !ended) {
    reject(boardErr('no-board'))
  }
  await pause(frozen ? 0 : 300)
  const last = board ?? ended!
  board = null
  ended = null
  invites = []
  emit('board:state', stamp({ ...last, state: 'ended', error: null }))
  emit('board:notch', notchState())
}

export async function RenameBoard(text: string) {
  const b = editable()
  if (!b.people.find((p) => p.you)?.admin) {
    reject(boardErr('not-admin'))
  }
  b.title = title(text, 'board', 48)
  record('board-renamed', b.title)
  publish()
}

export async function AddColumn(text: string, index: number) {
  const b = editable()
  const column: Column = { id: nextId(), title: title(text, 'column', 48), syncing: true, cards: [] }
  b.columns.splice(Math.min(Math.max(index, 0), b.columns.length), 0, column)
  record('column-added', column.title)
  publish()
  settleSoon()
  return column.id
}

function columnOf(b: Board, id: string) {
  const column = b.columns.find((c) => c.id === id)
  if (!column) {
    reject(boardErr('column-not-found'))
  }
  return column!
}

function cardOf(b: Board, id: string) {
  for (const column of b.columns) {
    const index = column.cards.findIndex((c) => c.id === id)
    if (index >= 0) {
      return { column, index, card: column.cards[index] }
    }
  }
  return reject(boardErr('card-not-found'))
}

export async function RenameColumn(id: string, text: string) {
  const b = editable()
  const column = columnOf(b, id)
  column.title = title(text, 'column', 48)
  column.syncing = true
  record('column-renamed', column.title)
  publish()
  settleSoon()
}

export async function MoveColumn(id: string, index: number) {
  const b = editable()
  const column = columnOf(b, id)
  const others = b.columns.filter((c) => c.id !== id)
  others.splice(Math.min(Math.max(index, 0), others.length), 0, column)
  column.syncing = true
  b.columns = others
  record('column-moved', column.title)
  publish()
  settleSoon()
}

export async function DeleteColumn(id: string) {
  const b = editable()
  const column = columnOf(b, id)
  b.columns = b.columns.filter((c) => c.id !== id)
  record('column-deleted', column.title, '', { cards: column.cards.length })
  publish()
}

export async function AddCard(columnID: string, input: { title: string; description: string; assignee: string; color: string }, index: number) {
  const b = editable()
  const column = columnOf(b, columnID)
  const made: Card = { id: nextId(), title: title(input.title, 'card', 256), description: String(input.description ?? ''), assignee: input.assignee ?? '', color: input.color ?? '', createdBy: b.self, lastBy: b.self, syncing: true }
  column.cards.splice(Math.min(Math.max(index, 0), column.cards.length), 0, made)
  record('card-added', made.title, column.title)
  publish()
  settleSoon()
  return made.id
}

export async function EditCard(id: string, edit: { title: string | null; description: string | null; assignee: string | null; color: string | null }) {
  const b = editable()
  const { card: found } = cardOf(b, id)
  const fields: string[] = []
  if (edit.title !== null) {
    found.title = title(edit.title, 'card', 256)
    fields.push('title')
  }
  for (const field of ['description', 'assignee', 'color'] as const) {
    if (edit[field] !== null) {
      found[field] = edit[field]!
      fields.push(field)
    }
  }
  found.lastBy = b.self
  found.syncing = true
  record('card-edited', found.title, '', { fields })
  publish()
  settleSoon()
}

export async function MoveCard(id: string, columnID: string, index: number) {
  const b = editable()
  const from = cardOf(b, id)
  const to = columnOf(b, columnID)
  if (scene === 'board-many' && to.title === 'Blocked') {
    reject(boardErr('reorder-no-room'))
  }
  from.column.cards.splice(from.index, 1)
  to.cards.splice(Math.min(Math.max(index, 0), to.cards.length), 0, from.card)
  from.card.lastBy = b.self
  from.card.syncing = true
  record('card-moved', from.card.title, to.title)
  publish()
  settleSoon()
}

export async function DeleteCard(id: string) {
  const b = editable()
  const from = cardOf(b, id)
  from.column.cards.splice(from.index, 1)
  record('card-deleted', from.card.title)
  publish()
}

function admin(): Board {
  const b = editable()
  if (!b.people.find((p) => p.you)?.admin) {
    reject(boardErr('not-admin'))
  }
  return b
}

export async function RemoveMember(key: string) {
  const b = admin()
  const target = b.people.find((p) => p.key === key && p.member)
  if (!target) {
    reject(boardErr('member-not-found'))
  }
  if (scene === 'board-empty') {
    reject(boardErr('last-admin'))
  }
  b.people = b.people.map((p) => (p.key === key ? { ...p, member: false, admin: false, seat: false, online: false } : p))
  record('removed', '', '', { subject: key })
  publish()
}

export async function CreateInvite(options: { ttlSeconds: number; uses: number; requireApproval: boolean }) {
  admin()
  if (!(options.ttlSeconds >= 1)) {
    reject(boardErr('invite-ttl-range'))
  }
  const uses = options.uses || 1
  if (uses < 1 || uses > 100) {
    reject(boardErr('invite-uses-range', '100'))
  }
  const made: InviteInfo = { id: nextId(), created: new Date().toISOString(), expires: new Date(Date.now() + options.ttlSeconds * 1000).toISOString(), uses, used: 0, requireApproval: options.requireApproval, state: 'active' }
  invites = [...invites, made]
  emit('board:invites', invites)
  return { ...made, code: `koloinv0${Array.from({ length: 132 }, (_, i) => 'abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789'[(i * 37 + invites.length * 11) % 56]).join('')}` }
}

export async function Invites() {
  const answer = invites
  if (scene === 'race') {
    return raced(answer, () => {
      if (!invites.some((x) => x.id === id64(903))) {
        invites = [...invites, { id: id64(903), created: new Date(now).toISOString(), expires: new Date(now + 86400e3).toISOString(), uses: 1, used: 0, requireApproval: true, state: 'active' }]
        emit('board:invites', invites)
      }
    })
  }
  return answer
}

export async function RevokeInvite(id: string) {
  admin()
  if (!invites.some((x) => x.id === id)) {
    reject(boardErr('invite-not-found'))
  }
  invites = invites.map((x) => (x.id === id ? { ...x, state: 'revoked' } : x))
  emit('board:invites', invites)
}

function request(id: string) {
  const b = admin()
  const found = b.requests.find((r) => r.request === id)
  if (!found || id === '42') {
    reject(boardErr('request-not-found'))
  }
  b.requests = b.requests.filter((r) => r.request !== id)
  return { b, found: found! }
}

export async function ApproveRequest(id: string) {
  const { b, found } = request(id)
  b.people = [...b.people, { key: found.joiner, name: found.name, tag: found.tag, you: false, member: true, admin: false, seat: false, online: true, syncing: true }]
  record('joined', '', '', { subject: found.joiner })
  publish()
}

export async function DenyRequest(id: string) {
  request(id)
  publish()
}

export async function Activity(before: number, limit: number) {
  if (!(limit >= 1)) {
    reject(boardErr('activity-limit'))
  }
  const older = [...activity].reverse().filter((a) => before === 0 || a.seq < before)
  const page = older.slice(0, scene === 'board-many' ? Math.min(8, limit) : limit)
  return { items: page, people: board?.people ?? [], more: older.length > page.length }
}

export async function Notch() {
  const answer = notchState()
  if (scene === 'race') {
    return raced(answer, () => {
      if (lastChange?.card !== 'Check the race') {
        lastChange = { kind: 'card-added', who: 'Yuki Tanaka', whoKey: YUKI.key, card: 'Check the race', column: 'Backlog' }
        emit('board:notch', notchState())
      }
    })
  }
  return answer
}

export async function NotchHide() {
  document.body.classList.add('is-hidden')
}

export async function NotchShow() {
  document.body.classList.remove('is-hidden')
}

export async function NotchOpenApp() {}

// What the window reported to the log, for the tests: the error text only.
export async function ReportError(text: string) {
  ;((window as unknown as { koloReports?: string[] }).koloReports ??= []).push(text)
}

// The live simulation: another member moves a card every few seconds.
if (!frozen) {
  window.setInterval(() => {
    if (board?.state !== 'active' || board.columns.length < 2) {
      return
    }
    const from = board.columns.find((c) => c.cards.length > 0)
    const to = board.columns[board.columns.length - 1]
    if (!from || from === to) {
      return
    }
    const moved = from.cards.shift()!
    moved.syncing = true
    moved.lastBy = MARTA.key
    to.cards.push(moved)
    board.unseen++
    lastChange = { kind: 'card-moved', who: 'Marta Oliveira', whoKey: MARTA.key, card: moved.title, column: to.title }
    activity.push({ seq: activity.length + 1, kind: 'card-moved', who: MARTA.key, subject: moved.id, text: moved.title, column: to.title, fields: [], cards: 0, settled: false })
    publish()
    emit('board:change', lastChange)
    settleSoon()
  }, 9000)
}

/** The key under which the window remembers that the sidebar is collapsed. */
export const SIDEBAR_KEY = 'kolo-board.sidebar'

const store = () => {
  try {
    return globalThis.localStorage ?? null
  } catch {
    return null
  }
}

/** Whether the sidebar was left collapsed. Storage that is missing or throws means "expanded". */
export function readCollapsed(storage = store()) {
  try {
    return storage?.getItem(SIDEBAR_KEY) === 'collapsed'
  } catch {
    return false
  }
}

/** Remembers the choice; the window works the same when storage is missing or throws. */
export function writeCollapsed(collapsed, storage = store()) {
  try {
    storage?.setItem(SIDEBAR_KEY, collapsed ? 'collapsed' : 'expanded')
  } catch {
    // the choice is kept for this run only
  }
}

/** The network as the quiet line of the sidebar: a tone for its dot and the copy keys of the mode and of the state. */
export function networkLine(network) {
  const mode = ['own', 'local'].includes(network?.mode) ? network.mode : 'public'
  const state = network?.state === 'failed' ? 'error' : network?.state === 'starting' ? 'starting' : 'ready'
  return { tone: state === 'error' ? 'error' : state === 'starting' ? 'waiting' : 'live', mode: `net.${mode}`, state: `badge.${state}` }
}

/** The quiet line under the board in the sidebar: how many cards are settled and syncing, or that this member no longer syncs. */
export function boardLine(board) {
  if (board.state === 'removed' || board.sync.stuck) {
    return [{ key: 'sync.stuck' }]
  }
  return [{ key: 'sync.count.settled', vars: { n: board.sync.cardsSettled } }, { key: 'sync.count.syncing', vars: { n: board.sync.cardsSyncing } }]
}

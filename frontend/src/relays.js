import { safeText } from './format.js'

const sources = ['flag', 'file', 'flag-and-file', 'env']
const states = ['waiting', 'ready', 'failed']

/** Where the connection relays came from: a source of the settings, else none. */
export function relaySource(from) {
  return sources.includes(from) ? from : 'none'
}

/** The connection relays of a network event, never null: each with its address made safe and a known state. */
export function relayList(network) {
  const list = Array.isArray(network?.relays) ? network.relays : []
  return list.map((r) => ({
    addr: safeText(r?.addr),
    state: states.includes(r?.state) ? r.state : 'waiting',
    error: r?.state === 'failed' ? r?.error ?? null : null,
  }))
}

/** The first connection relay that failed, for the notice of every screen; null when none did. */
export function failedRelay(network) {
  return relayList(network).find((r) => r.state === 'failed') ?? null
}

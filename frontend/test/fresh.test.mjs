import test from 'node:test'
import assert from 'node:assert/strict'
import { createGate, createSeqGuard, newerNetwork } from '../src/fresh.js'

const starting = { state: 'starting', message: '', error: null, entryNodes: 1, firstNode: '', seq: 1 }
const ready = { ...starting, state: 'ready', seq: 2 }

/** What the window holds after these network payloads, in this order, from its first state. */
const held = (...payloads) => payloads.reduce(newerNetwork, { state: 'starting', seq: 0 })

test('the network snapshot of Start never hides a newer event', () => {
  // The event comes first, then the older snapshot that Start took.
  assert.equal(held(ready, starting).state, 'ready')
  // The snapshot comes first, then the event.
  assert.equal(held(starting, ready).state, 'ready')
  assert.equal(newerNetwork(ready, null), ready)
  // Try again after a failure: every step is newer than the one before.
  const failed = { ...starting, state: 'failed', seq: 2 }
  assert.equal(held(starting, failed, { ...starting, seq: 3 }, { ...ready, seq: 4 }).seq, 4)
  // A reload while ready: the new page starts at seq 0 and the snapshot is the only payload.
  assert.equal(held({ ...ready, seq: 9 }).state, 'ready')
})

/** What the window shows after these board payloads (an event or an answer), in this order. */
function shown(...payloads) {
  const guard = createSeqGuard()
  let board = null
  for (const next of payloads) {
    if (guard.take(next)) {
      board = next
    }
  }
  return board
}

test('the board of an answer never hides a newer event', () => {
  const joining = { state: 'joining', phase: 'finding', seq: 3 }
  const waiting = { state: 'joining', phase: 'waiting', seq: 4 }
  const active = { state: 'active', phase: '', seq: 5 }
  // The event comes first, then the older answer of Board() or JoinBoard().
  assert.equal(shown(active, joining), active)
  assert.equal(shown(waiting, joining).phase, 'waiting')
  // The answer comes first, then the event.
  assert.equal(shown(joining, waiting, active), active)
  // Equal seq: the same read, sent twice.
  assert.equal(shown(active, { ...active }).state, 'active')
  // No board: taken before anything else, ignored once a board was shown.
  assert.equal(shown(null), null)
  assert.equal(shown(active, null), active)
  // A reload while a board is open: the snapshot is the only payload, whatever its seq.
  assert.equal(shown({ ...active, seq: 812 }).seq, 812)
})

test('a board after the end of the one before is shown, a late event of the old one is not', () => {
  const old = { state: 'active', title: 'Old', seq: 7 }
  const end = { state: 'ended', error: null, seq: 9 }
  const fresh = { state: 'active', title: 'New', seq: 12 }
  const guard = createSeqGuard()
  assert.ok(guard.take(old))
  assert.ok(guard.take(end))
  assert.equal(guard.take({ ...old, seq: 8 }), false)
  assert.ok(guard.take(fresh))
})

test('an answer of a getter that crossed an event is dropped, in either order', () => {
  const gate = createGate()
  // The answer comes back before any event: it is used.
  let mark = gate.mark()
  assert.equal(gate.fresh(mark), true)
  // An event arrives while the getter is asked: its answer is older and dropped.
  mark = gate.mark()
  gate.event()
  assert.equal(gate.fresh(mark), false)
  // The event came before the getter was asked: the answer is newer and used.
  gate.event()
  mark = gate.mark()
  assert.equal(gate.fresh(mark), true)
})

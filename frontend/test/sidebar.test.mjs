import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { SIDEBAR_KEY, boardLine, networkLine, readCollapsed, writeCollapsed } from '../src/sidebar.js'
import { AVATAR_COLORS, avatarColor } from '../src/avatars.js'
import { messages, locales } from '../src/i18n.js'

const memory = () => {
  const data = new Map()
  return { getItem: (k) => data.get(k) ?? null, setItem: (k, v) => data.set(k, String(v)), data }
}
const throwing = { getItem: () => { throw new Error('blocked') }, setItem: () => { throw new Error('blocked') } }

test('the sidebar starts expanded and remembers a collapse', () => {
  const storage = memory()
  assert.equal(readCollapsed(storage), false)
  writeCollapsed(true, storage)
  assert.equal(storage.data.get(SIDEBAR_KEY), 'collapsed')
  assert.equal(readCollapsed(storage), true)
  writeCollapsed(false, storage)
  assert.equal(readCollapsed(storage), false)
})

test('storage that is missing or throws never breaks the sidebar', () => {
  assert.equal(readCollapsed(null), false)
  assert.equal(readCollapsed(throwing), false)
  assert.doesNotThrow(() => writeCollapsed(true, null))
  assert.doesNotThrow(() => writeCollapsed(true, throwing))
})

test('the network line is the mode and the state, and never an address', () => {
  assert.deepEqual(networkLine({ mode: 'public', state: 'ready' }), { tone: 'live', mode: 'net.public', state: 'badge.ready' })
  assert.deepEqual(networkLine({ mode: 'own', state: 'starting' }), { tone: 'waiting', mode: 'net.own', state: 'badge.starting' })
  assert.deepEqual(networkLine({ mode: 'local', state: 'failed' }), { tone: 'error', mode: 'net.local', state: 'badge.error' })
  assert.equal(networkLine(null).mode, 'net.public')
  assert.equal(networkLine({ mode: 'anything', state: 'ready' }).mode, 'net.public')
  for (const locale of locales) {
    for (const key of ['net.public', 'net.own', 'net.local']) {
      assert.doesNotMatch(messages[locale][key], /\d|:/, `${locale} ${key}`)
    }
  }
})

test('the line under the board says how many cards are settled and syncing', () => {
  const sync = { cardsSettled: 11, cardsSyncing: 3, stuck: false }
  assert.deepEqual(boardLine({ state: 'active', sync }), [{ key: 'sync.count.settled', vars: { n: 11 } }, { key: 'sync.count.syncing', vars: { n: 3 } }])
  assert.deepEqual(boardLine({ state: 'removed', sync }), [{ key: 'sync.stuck' }])
  assert.deepEqual(boardLine({ state: 'active', sync: { ...sync, stuck: true } }), [{ key: 'sync.stuck' }])
})

test('an avatar color depends only on its seed and comes from the palette', () => {
  assert.equal(avatarColor('abc'), avatarColor('abc'))
  assert.ok(AVATAR_COLORS.includes(avatarColor('anything')))
  assert.ok(AVATAR_COLORS.includes(avatarColor('')))
  assert.ok(new Set(['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h'].map(avatarColor)).size > 2)
})

test('the sidebar holds only what the app has: no search, call, file, emoji, camera or microphone control', () => {
  const sidebar = readFileSync(new URL('../src/Sidebar.jsx', import.meta.url), 'utf8')
  assert.equal([...sidebar.matchAll(/<button/g)].length, 3)
  assert.doesNotMatch(sidebar, /search|call|camera|microphone|emoji|attach|<input/i)
  assert.doesNotMatch(sidebar, /entryNodes|firstNode|localNode|relays/)
})

import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { failedRelay, relayList, relaySource } from '../src/relays.js'
import { locales, messages } from '../src/i18n.js'

const read = (name) => readFileSync(new URL(`../src/${name}`, import.meta.url), 'utf8')
const wrongKey = { kind: 'network', reason: 'relay-wrong-key', detail: '203.0.113.7:7448', cause: '', message: 'x' }

test('Settings says where the connection relays came from, in every language', () => {
  for (const from of ['flag', 'file', 'flag-and-file', 'env']) {
    assert.equal(relaySource(from), from)
  }
  for (const from of ['none', '', undefined, 'other']) {
    assert.equal(relaySource(from), 'none')
  }
  assert.equal(messages.en['settings.relays.from.flag'], '(from -relay)')
  assert.equal(messages.en['settings.relays.from.file'], '(from -relay-file)')
  assert.equal(messages.en['settings.relays.from.flag-and-file'], '(from -relay and -relay-file)')
  assert.equal(messages.en['settings.relays.from.env'], '(from KOLO_BOARD_RELAYS)')
  for (const locale of locales) {
    for (const from of ['flag', 'file', 'flag-and-file', 'env']) {
      assert.ok(messages[locale][`settings.relays.from.${from}`], `${locale} ${from}`)
    }
    for (const state of ['waiting', 'ready', 'failed']) {
      assert.ok(messages[locale][`relay.state.${state}`], `${locale} ${state}`)
    }
    // the app uses a relay to reach others before it holds a place there
    assert.doesNotMatch(messages[locale]['relay.state.waiting'], /not in use|sin usar|n[aã]o est[aá] em uso/i, locale)
  }
})

test('the relay list is never null, and an address never carries a hidden character to the window', () => {
  assert.deepEqual(relayList(null), [])
  assert.deepEqual(relayList({ relays: null }), [])
  const list = relayList({ relays: [
    { addr: 'relay‮.example:7448', key: 'ab', state: 'ready', error: wrongKey },
    { addr: '203.0.113.7:7448', state: 'odd' },
    { addr: '203.0.113.8:7448', state: 'failed', error: wrongKey },
  ] })
  assert.equal(list[0].addr, 'relay[U+202E].example:7448')
  assert.equal(list[0].error, null, 'a relay that holds a place has no error')
  assert.equal(list[1].state, 'waiting')
  assert.equal(list[2].error, wrongKey)
})

test('a relay that failed is the notice of every screen, a wrong key included', () => {
  assert.equal(failedRelay({ relays: [{ addr: 'a:1', state: 'ready' }] }), null)
  assert.equal(failedRelay({}), null)
  const relay = failedRelay({ relays: [{ addr: 'a:1', state: 'waiting' }, { addr: '203.0.113.7:7448', state: 'failed', error: wrongKey }] })
  assert.equal(relay.addr, '203.0.113.7:7448')
  assert.equal(relay.error.reason, 'relay-wrong-key')
  const app = read('App.jsx')
  assert.match(app, /failedRelay\(network\)/)
  assert.match(app, /<Joining [^>]*notice=\{notice\}/, 'a joiner sees it while it looks for the inviter')
  assert.match(read('Start.jsx'), /id="joining-notice"/)
  assert.match(read('Settings.jsx'), /relayList\(network\)/)
})

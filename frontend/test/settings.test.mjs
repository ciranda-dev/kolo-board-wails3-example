import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

// The network is set with flags when the app starts (see the README), never in a form of the window:
// Settings holds the name, the language, the theme and the two notch switches, and shows the rest.
const read = (name) => readFileSync(new URL(`../src/${name}`, import.meta.url), 'utf8')

test('Settings has no form for the network, only what a person chooses in the window', () => {
  const page = read('Settings.jsx')
  assert.doesNotMatch(page, /<textarea/)
  assert.doesNotMatch(page, /SaveSettings|onSave\b/)
  const ids = [...page.matchAll(/<(?:input|select)[^>]*\bid="([\w-]+)"/g)].map((m) => m[1]).sort()
  assert.deepEqual(ids, ['settings-language', 'settings-name', 'settings-notch-preview', 'settings-show-notch', 'settings-theme'])
  assert.match(page, /settings\.flags/)
})

test('no screen asks the service to change the network settings', () => {
  for (const name of ['App.jsx', 'Start.jsx', 'BoardView.jsx', 'Settings.jsx', 'Drawer.jsx', 'Sheets.jsx']) {
    assert.doesNotMatch(read(name), /call\('SaveSettings'/, name)
  }
})

test('the three networks are told in every language, with the flag of each', async () => {
  const { messages, locales } = await import('../src/i18n.js')
  for (const locale of locales) {
    const m = messages[locale]
    assert.match(m['settings.mode.public'], /Ciranda/, locale)
    assert.match(m['settings.mode.public'], /beta/, locale)
    assert.match(m['settings.mode.public'], /-bootstrap/, locale)
    assert.match(m['settings.mode.public'], /-local/, locale)
    assert.match(m['settings.mode.own'], /\S/, locale)
    assert.match(m['settings.mode.local'], /-local/, locale)
    assert.match(m['settings.flags'], /-bootstrap/, locale)
    assert.match(m['settings.flags'], /-local/, locale)
    // a network that cannot be reached names the other two ways out
    for (const key of ['error.public-unreachable', 'error.bootstrap-failed', 'error.local-port-taken']) {
      assert.match(m[key], /-bootstrap/, `${locale} ${key}`)
    }
    for (const key of ['error.public-unreachable', 'error.bootstrap-failed']) {
      assert.match(m[key], /-local/, `${locale} ${key}`)
    }
    assert.match(m['error.local-port-taken'], /-local/, locale)
    assert.equal(m['error.no-entry-nodes'], undefined, locale)
    assert.equal(m['badge.setup'], undefined, locale)
    // a relay is only for strict NATs, and the person runs it
    assert.match(m['settings.relays.none'], /-relay/, locale)
  }
})

test('Settings shows the network and the window has no state for missing entry nodes', () => {
  const page = read('Settings.jsx')
  assert.match(page, /id="settings-mode"/)
  for (const mode of ['public', 'own', 'local']) {
    assert.ok(page.includes(mode), mode)
  }
  assert.doesNotMatch(read('App.jsx'), /no-entry-nodes/)
})

test('in the public network Settings lists no entry node, whatever the payload carries', () => {
  const page = read('Settings.jsx')
  assert.match(page, /mode === 'public' \? null : \(\s*<div[^>]*>\s*<dt className="label">\{t\('settings\.nodes'\)\}/)
})

import test from 'node:test'
import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const src = fileURLToPath(new URL('../src/', import.meta.url))
const read = (name) => readFileSync(`${src}${name}`, 'utf8')
const sources = readdirSync(src).filter((name) => /\.(jsx?|css)$/.test(name))

test('the notch page calls the Notch methods only and never holds an invite', () => {
  const text = read('Notch.jsx')
  assert.ok(!/invite/i.test(text))
  assert.ok(!/copyText|pasteText|Clipboard/.test(text))
  const calls = [...text.matchAll(/call\('(\w+)'/g)].map((m) => m[1])
  assert.ok(calls.length >= 3)
  for (const name of calls) {
    assert.ok(/^Notch/.test(name), `the notch page calls ${name}`)
  }
  assert.deepEqual([...text.matchAll(/on\('([\w:]+)'/g)].map((m) => m[1]), ['board:notch'])
})

test('the invite code is shown in its sheet only, and nothing is stored but the preferences', () => {
  for (const name of sources) {
    const text = read(name)
    if (name !== 'Sheets.jsx') {
      assert.ok(!/\.code\b/.test(text), `${name} reads an invite code`)
    }
    assert.ok(!/localStorage|sessionStorage|indexedDB|document\.cookie/.test(text) || name === 'prefs.js' || name === 'sidebar.js', `${name} stores something`)
  }
  assert.ok(!/invite|code/i.test(read('prefs.js').replace('Never an invite', '')))
  // The only other thing kept is whether the sidebar is collapsed.
  assert.deepEqual([...read('sidebar.js').matchAll(/(?:get|set)Item\(([^,)]+)/g)].map((m) => m[1]), ['SIDEBAR_KEY', 'SIDEBAR_KEY'])
  assert.match(read('sidebar.js'), /SIDEBAR_KEY = 'kolo-board\.sidebar'/)
  const app = read('App.jsx')
  assert.ok(!/document\.title = .*(invite|code)|location\.|console\./.test(app))
  // The preferences the window saves are these three and nothing else.
  assert.deepEqual([...new Set([...app.matchAll(/update\(\{ (\w+):/g)].map((m) => m[1]))].sort(), ['drawerTab', 'lang', 'theme'])
})

test('nothing leaves the app, and no HTML is written from a string', () => {
  const mock = readFileSync(fileURLToPath(new URL('../dev/mock.ts', import.meta.url)), 'utf8')
  for (const [name, text] of [...sources.map((name) => [name, read(name)]), ['dev/mock.ts', mock]]) {
    assert.ok(!/\bfetch\(|XMLHttpRequest|WebSocket|sendBeacon|@import|url\(\s*['"]?https?:|https?:\/\/(?!127\.0\.0\.1[:/])/.test(text), `${name} reaches for the network`)
    assert.ok(!/innerHTML|outerHTML|dangerouslySetInnerHTML|insertAdjacentHTML/.test(text), `${name} writes HTML from a string`)
  }
})

test('names and board text reach the screen through safeText', () => {
  const fields = [
    ['BoardView.jsx', ['card.title', 'column.title', 'drag.title', 'p.title']],
    ['App.jsx', ['board.title', 'change.who', 'dialogPerson.name']],
    ['Notch.jsx', ['change.who', 'change.text', 'change.column', 'state.title']],
    ['Drawer.jsx', ['item.text', 'item.column', 'board.id', 'r.joiner', 'p.key', 'x.id']],
    ['Sheets.jsx', ['c.title', 'made.code']],
    ['Settings.jsx', ['settings.firstNode', 'settings?.listen', 'settings?.dataDir', 'info?.version']],
    ['board.js', ['person.name', 'person.tag']],
  ]
  for (const [name, list] of fields) {
    const text = read(name)
    for (const field of list) {
      const escaped = field.replace(/[.?]/g, '\\$&')
      const safe = text.match(new RegExp(`(safeText|safeLines|shortId|initial)\\(${escaped}[,)]`, 'g')) ?? []
      assert.ok(safe.length > 0, `${name} shows ${field} through safeText`)
      // Never as it came: not as a child, not as a title or a label, not inside a sentence.
      const raw = text.match(new RegExp(`((?<!key=|value=)\\{|title=\\{|label=\\{|\\$\\{|: )\\s*${escaped}\\s*\\}`, 'g')) ?? []
      assert.deepEqual(raw, [], `${name} shows ${field} as it came`)
    }
  }
})

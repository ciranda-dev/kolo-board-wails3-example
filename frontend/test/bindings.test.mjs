import test from 'node:test'
import assert from 'node:assert/strict'
import { existsSync, readdirSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

// Every method the window calls exists in the mock and in the generated bindings (when they are
// here), with as many arguments as the Go method takes: a call with one too few reaches Go as a
// zero (Activity without its limit is rejected with activity-limit).
const src = fileURLToPath(new URL('../src/', import.meta.url))
const bindings = fileURLToPath(new URL('../bindings/github.com/ciranda-dev/kolo-board-wails3-example/', import.meta.url))
const mock = readFileSync(fileURLToPath(new URL('../dev/mock.ts', import.meta.url)), 'utf8')

/** The number of top-level arguments after the method name in text, which starts just after `'Name'`. */
function argCount(text) {
  let depth = 0
  let count = 0
  let quote = ''
  for (let i = 0; i < text.length; i++) {
    const ch = text[i]
    if (quote) {
      if (ch === '\\') {
        i++
      } else if (ch === quote) {
        quote = ''
      }
    } else if (ch === "'" || ch === '"' || ch === '`') {
      quote = ch
    } else if ('([{'.includes(ch)) {
      depth++
    } else if (')]}'.includes(ch)) {
      if (depth === 0) {
        return count
      }
      depth--
    } else if (ch === ',' && depth === 0) {
      count++
    }
  }
  throw new Error('unclosed call')
}

const calls = []
for (const name of readdirSync(src).filter((n) => /\.jsx?$/.test(n))) {
  const text = readFileSync(`${src}${name}`, 'utf8')
  for (const m of text.matchAll(/\bcall\('(\w+)'/g)) {
    calls.push({ file: name, method: m[1], args: argCount(text.slice(m.index + m[0].length)) })
  }
}

/** The parameters of each exported function of a bindings or mock file, by name. */
function params(text, pattern) {
  return Object.fromEntries([...text.matchAll(pattern)].map((m) => [m[1], m[2].trim() === '' ? 0 : m[2].split(',').length]))
}

test('the window asks for a page of the activity with its size', () => {
  const activity = calls.filter((c) => c.method === 'Activity')
  assert.ok(activity.length > 0)
  for (const c of activity) {
    assert.equal(c.args, 2, `${c.file}: Activity(before, limit)`)
  }
})

test('every method the window calls is in the mock, with its arguments', () => {
  const mocked = params(mock, /export async function (\w+)\(([^)]*)\)/g)
  for (const c of calls) {
    assert.ok(c.method in mocked, c.method)
    assert.equal(c.args, mocked[c.method], `${c.file}: ${c.method} takes ${mocked[c.method]}, called with ${c.args}`)
  }
})

test('every method the window calls is in the generated bindings, with every argument', { skip: !existsSync(`${bindings}boardservice.js`) && 'bindings not generated' }, () => {
  const bound = params(readFileSync(`${bindings}boardservice.js`, 'utf8'), /export function (\w+)\(([^)]*)\)/g)
  for (const c of calls) {
    assert.ok(c.method in bound, c.method)
    assert.equal(c.args, bound[c.method], `${c.file}: ${c.method} takes ${bound[c.method]}, called with ${c.args}`)
  }
  const models = readFileSync(`${bindings}models.js`, 'utf8')
  for (const field of ['seq', 'findTimeoutMs', 'joinTimeoutMs', 'lookupIntervalMs', 'notchLingerMs', 'pendingSeats']) {
    assert.ok(models.includes(`"${field}"`), field)
  }
})

import test from 'node:test'
import assert from 'node:assert/strict'
import { boardLists } from '../src/board.js'

import { MAX_REPORT, reportText } from '../src/report.js'

test('a board with null lists, as a joiner got it before its roster, reads as empty lists', () => {
  const b = boardLists({ state: 'joining', phase: 'syncing', people: null, requests: null, columns: [{ id: 'c', cards: null }] })
  assert.deepEqual(b.people, [])
  assert.deepEqual(b.requests, [])
  assert.deepEqual(b.columns[0].cards, [])
  assert.deepEqual(boardLists({ columns: null }).columns, [])
  assert.equal(boardLists(null), null)
  const whole = { people: [{ key: 'a' }], requests: [], columns: [] }
  assert.deepEqual(boardLists(whole), whole)
})

test('the report of an error is its name, message and stack, cut to the bound', () => {
  const text = reportText(new TypeError('x is null'), '\n    at App')
  assert.match(text, /^TypeError: x is null/)
  assert.match(text, /at App/)
  assert.equal(text.split('TypeError: x is null').length, 2)
  assert.equal(reportText('plain'), 'Error: plain')
  assert.equal(Array.from(reportText(new Error('e'.repeat(MAX_REPORT * 2)))).length, MAX_REPORT)
})

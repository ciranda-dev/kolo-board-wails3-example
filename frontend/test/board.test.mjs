import test from 'node:test'
import assert from 'node:assert/strict'
import { cardEdit, changeSentence, columnMove, dropIndex, findCard, fromChange, keyboardMove, membersOf, sameSpot, settledShare, syncNote } from '../src/board.js'
import { locales, messages, translate } from '../src/i18n.js'

const cards = (...ids) => ids.map((id) => ({ id, title: id }))
const columns = [
  { id: 'todo', cards: cards('a', 'b', 'c') },
  { id: 'doing', cards: cards('d') },
  { id: 'done', cards: [] },
]

test('a drop gives the place among the other cards of the column', () => {
  assert.equal(dropIndex(columns[0].cards, 'x', 'a'), 0, 'from another column, above the first')
  assert.equal(dropIndex(columns[0].cards, 'x', ''), 3, 'from another column, at the bottom')
  assert.equal(dropIndex(columns[0].cards, 'a', 'c'), 1, 'a moved above c: b is the only card before it')
  assert.equal(dropIndex(columns[0].cards, 'c', 'a'), 0)
  assert.equal(dropIndex(columns[0].cards, 'a', ''), 2, 'to the bottom of its own column')
  assert.equal(dropIndex([], 'a', ''), 0)
  assert.equal(dropIndex(columns[0].cards, 'a', 'gone'), 2, 'an unknown card is the bottom')
})

test('a drop where the card already is changes nothing', () => {
  assert.ok(sameSpot(columns, 'b', 'todo', 1))
  assert.ok(!sameSpot(columns, 'b', 'todo', 0))
  assert.ok(!sameSpot(columns, 'b', 'doing', 1))
  assert.ok(!sameSpot(columns, 'gone', 'todo', 0))
})

test('the keyboard moves a card one place, to an end, or to the next column', () => {
  assert.deepEqual(keyboardMove(columns, 'b', 'up'), { columnId: 'todo', index: 0 })
  assert.deepEqual(keyboardMove(columns, 'b', 'down'), { columnId: 'todo', index: 2 })
  assert.equal(keyboardMove(columns, 'a', 'up'), null)
  assert.equal(keyboardMove(columns, 'c', 'down'), null)
  assert.deepEqual(keyboardMove(columns, 'c', 'top'), { columnId: 'todo', index: 0 })
  assert.equal(keyboardMove(columns, 'a', 'top'), null)
  assert.deepEqual(keyboardMove(columns, 'a', 'bottom'), { columnId: 'todo', index: 2 })
  assert.deepEqual(keyboardMove(columns, 'c', 'right'), { columnId: 'doing', index: 1 }, 'no lower than the bottom of the next column')
  assert.deepEqual(keyboardMove(columns, 'a', 'right'), { columnId: 'doing', index: 0 })
  assert.deepEqual(keyboardMove(columns, 'd', 'right'), { columnId: 'done', index: 0 })
  assert.deepEqual(keyboardMove(columns, 'd', 'left'), { columnId: 'todo', index: 0 })
  assert.equal(keyboardMove(columns, 'a', 'left'), null)
  assert.equal(keyboardMove(columns, 'gone', 'up'), null)
  assert.equal(keyboardMove(columns, 'a', 'sideways'), null)
})

test('a column moves one place among the others, and not past an edge', () => {
  assert.equal(columnMove(columns, 'todo', -1), null)
  assert.equal(columnMove(columns, 'todo', 1), 1)
  assert.equal(columnMove(columns, 'doing', -1), 0)
  assert.equal(columnMove(columns, 'done', 1), null)
  assert.equal(columnMove(columns, 'gone', 1), null)
})

test('an edit sends only the fields that changed', () => {
  const card = { title: 'A', description: '', assignee: 'k1', color: 'red' }
  assert.equal(cardEdit(card, { ...card }), null)
  assert.deepEqual(cardEdit(card, { ...card, title: 'B' }), { title: 'B', description: null, assignee: null, color: null })
  assert.deepEqual(cardEdit(card, { ...card, assignee: '', color: '' }), { title: null, description: null, assignee: '', color: '' })
})

test('findCard and membersOf read the board as the service sends it', () => {
  assert.equal(findCard(columns, 'd').column.id, 'doing')
  assert.equal(findCard(columns, 'zz'), null)
  const people = [{ key: 'a', member: true }, { key: 'b', member: false }, { key: 'c', member: true, you: true }]
  assert.deepEqual(membersOf(people).map((p) => p.key), ['c', 'a'])
})

test('the sentence about settling follows the contract', () => {
  const sync = { settling: true, quorum: 3, seats: 4, seatsOnline: 4, pendingSeats: 0, halted: null }
  assert.equal(syncNote(sync), null)
  assert.deepEqual(syncNote({ ...sync, settling: false, seatsOnline: 2 }), { key: 'sync.waiting', vars: { n: 3, m: 4, k: 2 }, tone: 'waiting' })
  assert.equal(syncNote({ ...sync, pendingSeats: 2 }).key, 'sync.adding')
  assert.equal(syncNote({ ...sync, settling: false, halted: { reason: 'settlement-halted' } }).key, 'sync.halted')
  assert.equal(syncNote({ ...sync, seats: 1, quorum: 1, singleSettler: true }).key, 'sync.single')
  assert.equal(settledShare(3, 1), 0.75)
  assert.equal(settledShare(0, 0), 1)
})

const KINDS = ['board-created', 'board-renamed', 'name-changed', 'joined', 'removed', 'column-added', 'column-renamed', 'column-moved', 'column-deleted', 'card-added', 'card-edited', 'card-moved', 'card-deleted']

test('every kind of change has its sentence, with and without board text, in every language', () => {
  for (const kind of KINDS) {
    const shapes = [
      { kind, who: 'Ana', name: 'Bia', text: 'Title', column: 'Doing', fields: 'title', n: 2 },
      { kind, who: 'Ana', name: 'Bia', text: 'Title', column: '', fields: '', n: 0 },
      { kind, who: 'Ana', name: 'Bia', text: '', column: '', fields: '', n: 0 },
    ]
    for (const shape of shapes) {
      const { key, vars } = changeSentence(shape)
      for (const locale of locales) {
        assert.ok(messages[locale][key] !== undefined || messages[locale][`${key}.other`] !== undefined, `${locale} ${key}`)
        const said = translate(locale, key, vars)
        assert.ok(said.includes('Ana'), `${locale} ${key}: ${said}`)
        assert.ok(!/\{\w+\}/.test(said), `${locale} ${key} leaves a place empty: ${said}`)
        assert.ok(!said.includes('undefined'), `${locale} ${key}: ${said}`)
      }
    }
  }
})

test('without board text a change names no card and no column', () => {
  for (const kind of KINDS.slice(5)) {
    const { key, vars } = changeSentence({ ...fromChange({ kind, who: 'Ana', card: '', column: '' }), who: 'Ana' })
    assert.ok(key.startsWith('plain.'), key)
    assert.ok(!/[“”]/.test(translate('en', key, vars)))
  }
})

test('a change event puts its text where the sentence expects it', () => {
  assert.deepEqual(fromChange({ kind: 'card-moved', who: 'Ana', card: 'Fix', column: 'Doing' }), { kind: 'card-moved', who: 'Ana', text: 'Fix', column: 'Doing' })
  assert.deepEqual(fromChange({ kind: 'board-renamed', who: 'Ana', card: '', column: 'New title' }), { kind: 'board-renamed', who: 'Ana', text: 'New title', column: '' })
  assert.equal(translate('en', changeSentence({ kind: 'card-moved', who: 'Ana', text: 'Fix', column: 'Doing' }).key, { who: 'Ana', text: 'Fix', column: 'Doing' }), 'Ana moved “Fix” to Doing')
  assert.equal(changeSentence({ kind: 'column-deleted', who: 'Ana', text: 'Ideas', n: 2 }).key, 'change.column-deleted.cards')
})

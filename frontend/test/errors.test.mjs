import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { locales, messages, translate } from '../src/i18n.js'
import { errorSentence, boardError } from '../src/errors.js'

// The reasons map of errors.go is the one error table: the window has its English sentence exactly,
// and a sentence in every other language with the same placeholders, so a new or reworded reason on
// the Go side fails here until it has its three sentences.
function goReasons() {
  const text = readFileSync(new URL('../../errors.go', import.meta.url), 'utf8')
  const start = text.indexOf('var reasons = map[string]')
  const block = text.slice(start, text.indexOf('\n}', start))
  return Object.fromEntries([...block.matchAll(/^\t"([\w-]+)":\s*\{kind\w+, "((?:[^"\\]|\\.)*)"\},?$/gm)].map((m) => [m[1], JSON.parse(`"${m[2]}"`)]))
}

const placeholders = (text) => [...text.matchAll(/\{\w+\}/g)].map((m) => m[0]).sort()

test('every reason of errors.go has its sentence, in every language', () => {
  const go = goReasons()
  const reasons = Object.keys(go)
  assert.ok(reasons.length >= 60)
  for (const reason of ['you-were-removed', 'strand-unconfirmed', 'settlement-halted', 'reorder-no-room']) {
    assert.ok(reasons.includes(reason), reason)
  }
  for (const reason of reasons) {
    assert.equal(messages.en[`error.${reason}`], go[reason], `en error.${reason}`)
    for (const locale of locales) {
      const text = messages[locale][`error.${reason}`]
      assert.equal(typeof text, 'string', `${locale} error.${reason}`)
      assert.deepEqual(placeholders(text), placeholders(go[reason]), `${locale} error.${reason} placeholders`)
      assert.ok(!/sorry|lo sentimos|perd[oó]n|desculp/i.test(text), `${locale} error.${reason} apologizes`)
    }
  }
  for (const locale of locales) {
    const extra = Object.keys(messages[locale]).filter((k) => k.startsWith('error.') && !reasons.includes(k.slice(6)))
    assert.deepEqual(extra, [], locale)
  }
})

const t = (locale) => (key, vars) => translate(locale, key, vars)
const halted = { kind: 'sync', reason: 'board-halted', detail: '', cause: 'disk full', message: 'The board stopped on this computer: disk full. Restart Kolo Board to try again.' }

test('an error is translated by its reason, with its detail and cause', () => {
  assert.equal(errorSentence(t('en'), 'en', halted), halted.message)
  assert.equal(errorSentence(t('es'), 'es', halted), 'El tablero se detuvo en este ordenador: disk full. Reinicia Kolo Board para intentarlo de nuevo.')
  assert.equal(errorSentence(t('pt'), 'pt', halted), 'O quadro parou neste computador: disk full. Reinicie o Kolo Board para tentar de novo.')
  const long = { kind: 'name', reason: 'name-too-long', detail: '48', cause: '', message: 'x' }
  assert.equal(errorSentence(t('en'), 'en', long), 'The name is too long. Use at most 48 characters.')
})

test('a rejected call carries the error as its cause, as an object or as JSON', () => {
  const thrown = Object.assign(new Error(halted.message), { cause: halted })
  assert.equal(boardError(thrown), halted)
  assert.equal(errorSentence(t('pt'), 'pt', thrown), 'O quadro parou neste computador: disk full. Reinicie o Kolo Board para tentar de novo.')
  const asJson = Object.assign(new Error(halted.message), { cause: JSON.stringify(halted) })
  assert.equal(boardError(asJson).reason, 'board-halted')
  assert.equal(boardError(Object.assign(new Error('x'), { cause: '{broken' })), null)
})

test('an unknown reason keeps the English sentence of the service', () => {
  const unknown = { kind: 'internal', reason: 'brand-new', detail: '', cause: '', message: 'A new sentence.' }
  assert.equal(errorSentence(t('pt'), 'pt', unknown), 'A new sentence.')
  assert.equal(errorSentence(t('pt'), 'pt', new Error('plain')), 'plain')
  assert.equal(errorSentence(t('pt'), 'pt', null, 'fallback'), 'fallback')
  assert.equal(errorSentence(t('es'), 'es', { key: 'join.needInvite' }), 'Pega primero la invitación.')
})

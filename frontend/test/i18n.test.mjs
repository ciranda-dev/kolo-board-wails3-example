import test from 'node:test'
import assert from 'node:assert/strict'
import { locales, languageNames, htmlLang, messages, pickLocale, translate } from '../src/i18n.js'

const places = (text) => (text.match(/\{\w+\}/g) ?? []).sort().join(' ')

test('the three languages have identical key sets, with the same places', () => {
  assert.deepEqual(locales, ['en', 'es', 'pt'])
  const keys = Object.keys(messages.en).sort()
  assert.ok(keys.length > 300)
  for (const locale of locales) {
    assert.deepEqual(Object.keys(messages[locale]).sort(), keys, `keys of ${locale}`)
    for (const key of keys) {
      assert.equal(places(messages[locale][key]), places(messages.en[key]), `${locale} ${key}`)
      assert.ok(messages[locale][key].trim() !== '', `${locale} ${key} is empty`)
    }
    assert.ok(languageNames[locale])
    assert.ok(htmlLang[locale])
  }
})

test('the copy has no em dash and no exclamation mark', () => {
  for (const locale of locales) {
    for (const [key, text] of Object.entries(messages[locale])) {
      assert.ok(!/[\u2014!\u00a1]/.test(text), `${locale} ${key}: ${text}`)
    }
  }
})

test('the copy makes no claim beyond the words of the contract', () => {
  const claims = /\b(secure|securely|private|privately|safe|anonymous|end-to-end|nobody can|no one can|for everyone|seguro|segura|privad[oa]|an[oó]nim[oa]|extremo a extremo|ponta a ponta|ningu[eé]m pode|nadie puede)\b/i
  for (const locale of locales) {
    for (const [key, text] of Object.entries(messages[locale])) {
      // The error sentences are the contract's own words, pinned in errors.test.mjs.
      if (key.startsWith('error.')) {
        continue
      }
      assert.ok(!claims.test(text.replaceAll('kolo-secure', '')), `${locale} ${key}: ${text}`)
    }
    // Only the contract's sentence about connections says "encrypted".
    const encrypted = Object.entries(messages[locale]).filter(([, text]) => /encrypt|cifrad|criptograf/i.test(text)).map(([key]) => key)
    assert.deepEqual(encrypted, ['claims.connections'], locale)
  }
})

test('the sentences the contract fixes are the contract\'s', () => {
  assert.equal(messages.en['sync.syncing'], 'Syncing')
  assert.equal(messages.en['sync.settled'], 'Settled')
  assert.equal(messages.en['sync.waiting'], 'Waiting for {n} of {m} settlers to be online')
  assert.equal(messages.en['sync.single'], 'One computer settles this board. When it is off, changes stay syncing.')
  assert.equal(messages.en['sync.halted'], 'Settling has stopped for this board (see the error).')
  assert.equal(messages.en['sync.adding'], 'Adding settlers: they take effect at the next change')
  assert.equal(messages.en['person.former'], 'Former member')
  assert.equal(messages.en['card.delete.note'], "The card goes from the board. Its text stays in every member's history.")
  assert.equal(messages.en['column.delete.note.other'], "The column and its {n} cards go from the board. Their text stays in every member's history.")
  assert.equal(messages.en['invite.open'], 'Anyone who gets this code can join until it expires or runs out.')
  assert.equal(messages.en['invite.firstAdmins'], 'The first members added become admins and settlers.')
  assert.ok(messages.en['remove.keeps'].startsWith('A removed member keeps what it had received'))
})

test('there is no chat, comment, attachment or due date copy', () => {
  for (const key of Object.keys(messages.en)) {
    assert.ok(!/^(chat|message|comment|file|attach|due)\./.test(key), key)
  }
})

test('pickLocale prefers the saved language, then the system, then English', () => {
  assert.equal(pickLocale('pt', ['es-ES']), 'pt')
  assert.equal(pickLocale(undefined, ['fr-FR', 'es-MX']), 'es')
  assert.equal(pickLocale('system', ['pt-BR']), 'pt')
  assert.equal(pickLocale(undefined, ['fr-FR']), 'en')
  assert.equal(pickLocale(undefined), 'en')
})

test('translate fills places and picks the form by n', () => {
  assert.equal(translate('en', 'notch.more', { n: 1 }), '+1 more change')
  assert.equal(translate('en', 'notch.more', { n: 3 }), '+3 more changes')
  assert.equal(translate('es', 'column.delete.note', { n: 2 }), 'La columna y sus 2 tarjetas salen del tablero. Su texto se queda en el historial de cada miembro.')
  assert.equal(translate('pt', 'head.connected', { n: 3 }), '3 conectados')
  assert.equal(translate('pt', 'head.members', { n: 1 }), '1 membro')
  assert.equal(translate('es', 'head.members', { n: 4 }), '4 miembros')
  assert.equal(translate('en', 'head.connected', { n: 1 }), '1 connected')
  assert.equal(translate('en', 'no.such.key'), 'no.such.key')
  assert.equal(translate('xx', 'leave.action'), 'Leave board')
  assert.equal(translate('en', 'settings.firstNode', {}), 'This app runs the first entry node on {addr}.')
})

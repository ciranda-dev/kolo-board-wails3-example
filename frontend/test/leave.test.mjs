import test from 'node:test'
import assert from 'node:assert/strict'
import { existsSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { locales, messages } from '../src/i18n.js'

// Board.leave says what LeaveBoard does; the leave dialog and the drawer show `leave.plan.<leave>`.
const goLeave = fileURLToPath(new URL('../../leave.go', import.meta.url))
const shown = ['everyone', 'alone', 'not-admin', 'last-admin', 'last-settler', 'local', 'removed']

test('every plan the window can show has its sentence, in every language', () => {
  for (const locale of locales) {
    for (const plan of shown) {
      assert.ok(messages[locale][`leave.plan.${plan}`], `${locale} leave.plan.${plan}`)
    }
  }
})

test('the plans are the ones of leave.go', { skip: !existsSync(goLeave) && 'leave.go is not here' }, () => {
  const plans = [...readFileSync(goLeave, 'utf8').matchAll(/^\tleave\w+\s*=\s*"([\w-]+)"$/gm)].map((m) => m[1])
  // a join in progress is cancelled from its own screen, never from the leave dialog
  assert.deepEqual(plans.filter((p) => p !== 'cancel').sort(), [...shown].sort())
})

test('leaving says that an admin is removed for all members first, and a member is not', () => {
  assert.match(messages.en['leave.plan.everyone'], /removes you from the board/)
  assert.match(messages.en['leave.plan.everyone'], /copy is deleted/)
  assert.match(messages.en['leave.plan.not-admin'], /ask an admin to remove you/)
  assert.match(messages.en['leave.plan.local'], /still list you/)
})

test('a member whose changes do not settle is told it may have been removed', () => {
  assert.equal(messages.en['sync.stuck'], 'Not syncing')
  assert.equal(messages.en['sync.stuck.note'], 'Your changes are not syncing. You may have been removed from this board. Ask an admin.')
  for (const locale of locales) {
    assert.notEqual(messages[locale]['sync.stuck'], messages[locale]['sync.syncing'], locale)
  }
})

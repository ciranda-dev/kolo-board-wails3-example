// Screenshots of every state against the dev server and the mock, through Chrome's DevTools protocol.
// Usage: node dev/shots.mjs <out-dir> [base-url] [filter]. Needs Google Chrome; nothing to install.
import { mkdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { FILLS, launch, sleep, until } from './chrome.mjs'

const out = process.argv[2]
const base = process.argv[3] || 'http://127.0.0.1:9245'
const filter = process.argv[4] || ''
if (!out) {
  console.error('usage: node dev/shots.mjs <out-dir> [base-url] [filter]')
  process.exit(2)
}
// name: width, height, pixel ratio. The two large sizes are where a screen that does not fill the window shows.
const SIZES = { default: [1000, 618, 2], min: [640, 520, 2], wide: [1440, 900, 1], xl: [2000, 1240, 1] }
const TRANSLATED = ['default', 'min']
// Checked without a picture: the largest window.
const HUGE = [2560, 1440]
const PREFS = 'kolo-board:prefs'
const SIDEBAR = 'kolo-board.sidebar'
const click = (selector) => `document.querySelector(${JSON.stringify(selector)}).click()`
const button = (text) => `[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === ${JSON.stringify(text)}).click()`
const type = (selector, text) => `(() => { const el = document.querySelector(${JSON.stringify(selector)}); const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype; Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, ${JSON.stringify(text)}); el.dispatchEvent(new Event('input', { bubbles: true })) })()`
const settings = click('.me .iconbtn')
const people = click('.pill--members')
const activity = click('.pill--activity')
const collapse = click('#side-panel .iconbtn')
const tab = (name) => click(`#tab-${name}`)
const firstCard = click('.kcard')
const INVITE = 'koloinv0qx7Zt2mVb9LkR4sWd8YcA1fGh6JnP3uTe5XoBi0MyKvNrCa7DwEzQ2lHs4FgUj9pSx6Ot'

// No text, tooltip or label on screen may hold a character without a visible shape.
const HIDDEN = `(() => { const bad = /[\\p{Cc}\\p{Cf}\\p{Zl}\\p{Zp}\\p{Co}\\p{Cs}]/u; const texts = [document.body.innerText.replace(/[\\n\\t]/g, ' ')]; for (const el of document.querySelectorAll('[title], [aria-label]')) { texts.push((el.getAttribute('title') ?? '').replace(/\\n/g, ' '), el.getAttribute('aria-label') ?? '') } return texts.some((x) => bad.test(x)) ? 'an invisible or direction-changing character is on screen' : '' })()`
// The invite may be on screen in the field it is pasted into and in the sheet that made it, and never in the title or the address.
const LEAK = `(() => { const open = document.querySelector('#invite-code'); const field = document.querySelector('#join-invite'); const text = document.body.innerText + document.title + location.href + [...document.querySelectorAll('[title], [aria-label]')].map((el) => (el.getAttribute('title') ?? '') + (el.getAttribute('aria-label') ?? '')).join(' ') + JSON.stringify({ ...localStorage }); return !open && !field && /koloinv0\\w{8,}/.test(text) ? 'an invite is on screen outside its sheet' : /koloinv0\\w{8,}/.test(document.title + location.href + JSON.stringify({ ...localStorage })) ? 'an invite is in the title, the address or the storage' : '' })()`
// No text may touch the edge of the surface it sits on.
const INSET = `(() => {
  for (const el of document.querySelectorAll('.mainhead *, .body *, .modal *')) {
    if (![...el.childNodes].some((n) => n.nodeType === 3 && n.textContent.trim() !== '')) continue
    if (el.closest('.sr-only, option, select, .avatar, .disc, .tag, .pill__badge, .segmented__count')) continue
    const r = el.getBoundingClientRect()
    if (r.width < 2 || r.height < 2) continue
    const box = el.closest('.kcard, .btn, .pill, .swatch, .segmented__tab, .lane__add, .row:not(.row--activity), .notice, .invite__value, .field, .lane, .strip, .syncbar, .drawer__section, .drawer__head, .modal__head, .sheet, .settings__group, [data-pane], .mainhead, .convs')
    if (!box || box === el) continue
    const b = box.getBoundingClientRect()
    const need = box.matches('.btn, .pill, .swatch, .segmented__tab, .syncbar__state') ? 4 : 8
    if (r.left - b.left < need || b.right - r.right < 4) return 'text touches the edge of its surface: ' + el.className + ' "' + el.textContent.trim().slice(0, 30) + '" in ' + box.className
  }
  return ''
})()`
const OVERFLOW = `(() => { if (document.documentElement.scrollWidth > window.innerWidth) return 'page scrolls sideways'; for (const el of document.querySelectorAll('.mainhead > *, .convs > *, .syncbar > *, .strip > *, .notch > *, .modal__box')) { const r = el.getBoundingClientRect(); if (r.width > 0 && (r.right > window.innerWidth + 0.5 || r.bottom > window.innerHeight + 0.5)) return 'something is outside the window: ' + el.className } return '' })()`

/** name, mock scene, steps to run after load, options: lang, only (sizes). */
const SHOTS = [
  ['name', 'name', []],
  ['name-empty', 'name', [click('button[type=submit]')]],
  ['home', 'home', []],
  ['home-starting', 'starting', []],
  ['home-public-failed', 'net-public-failed', []],
  ['home-network-failed', 'net-failed', []],
  ['home-create-empty', 'home', [click('#create-submit')]],
  ['home-join-empty', 'home', [click('#join-submit')]],
  ['home-join-invalid', 'home', [type('#join-invite', 'hello, are you there'), click('#join-submit')]],
  ['home-join-pasted', 'home', [button('Paste')]],
  ['fatal', 'fatal', []],
  ['joining-finding', 'joining-finding', []],
  ['joining-waiting', 'joining-waiting', []],
  ['joining-syncing', 'joining-syncing', []],
  ['joining-finding-limit', 'joining-finding&limits=1', []],
  ['joining-waiting-limit', 'joining-waiting&limits=1', []],
  ['board', 'board', []],
  ['board-empty', 'board-empty', []],
  ['board-no-columns', 'board-no-columns', []],
  ['board-waiting-settlers', 'board-waiting', []],
  ['board-adding-settlers', 'board-adding', []],
  ['board-member', 'board-member', []],
  ['board-removed', 'board-removed', []],
  ['board-removed-leave', 'board-removed', [button('Leave board')]],
  ['board-blocked', 'board-blocked', []],
  ['board-strand-lost', 'board-lost', []],
  ['board-halted', 'board-halted', []],
  ['board-error', 'board-error', []],
  ['board-network-failed', 'board-net-failed', []],
  ['board-hostile', 'board-hostile', []],
  ['board-hostile-people', 'board-hostile', [people]],
  ['board-many', 'board-many', []],
  ['board-toast', 'board-toast', []],
  ['board-focus', 'board', [`document.querySelector('.kcard').focus()`]],
  ['board-keyboard-move', 'board', [`(() => { const el = document.querySelector('.kcard'); el.focus(); el.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', altKey: true, bubbles: true })) })()`]],
  ['board-move-no-room', 'board-many', [`(() => { const el = [...document.querySelectorAll('.lane')][4].querySelector('.kcard'); el.focus(); el.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', altKey: true, bubbles: true })) })()`]],
  ['card-edit', 'board', [`document.querySelectorAll('.lane')[1].querySelector('.kcard').click()`]],
  ['card-edit-former', 'board', [`document.querySelectorAll('.kcard')[3].click()`]],
  ['card-delete', 'board', [firstCard, click('#card-delete')]],
  ['card-new', 'board', [click('.lane__add')]],
  ['card-new-empty', 'board', [click('.lane__add'), click('#card-save')]],
  ['card-readonly', 'board-removed', [firstCard]],
  ['column-edit', 'board', [click('.lane__head .iconbtn + .iconbtn')]],
  ['column-delete', 'board', [click('.lane__head .iconbtn + .iconbtn'), click('#column-delete')]],
  ['column-new', 'board', [click('#add-column')]],
  ['column-new-empty', 'board', [click('#add-column'), click('#column-save')]],
  ['board-rename', 'board', [click('.mainhead__rename')]],
  ['people', 'board', [people]],
  ['people-requests', 'board-requests', [people]],
  ['people-request-gone', 'board-requests', [people, `[...document.querySelectorAll('.row--request .btn')][2].click()`]],
  ['people-member', 'board-member', [people]],
  ['people-same-names', 'board-same-names', [people]],
  ['people-remove', 'board', [people, click('.row .link')]],
  ['people-remove-last-admin', 'board-empty', [people]],
  ['people-leave', 'board', [people, click('#drawer-leave-btn')]],
  ['invite', 'board', [button('Invite')]],
  ['invite-open', 'board', [button('Invite'), click('#invite-approval')]],
  ['invite-made', 'board', [button('Invite'), click('#invite-create')]],
  ['invite-uses-range', 'board', [button('Invite'), type('#invite-uses', '500'), click('#invite-create')]],
  ['invite-offline', 'board-net-failed', [button('Invite')]],
  ['activity', 'board', [activity]],
  ['activity-more', 'board-many', [activity]],
  ['activity-waiting', 'board-waiting', [activity]],
  ['sync-legend', 'board', [click('.syncbar__state')]],
  ['sync-waiting', 'board-waiting', [click('.syncbar__state')]],
  ['sync-single', 'board-empty', [click('.syncbar__state')]],
  ['sync-halted', 'board-halted', [click('.syncbar__state')]],
  ['ended-inviter-not-found', 'ended-inviter-not-found', []],
  ['ended-join-denied', 'ended-join-denied', []],
  ['ended-invite-expired', 'ended-invite-expired', []],
  ['ended-join-sync-timeout', 'ended-join-sync-timeout', []],
  ['ended-board-open', 'ended-board-open', []],
  ['settings', 'home', [settings]],
  ['settings-board', 'board', [settings]],
  ['settings-local', 'local', [settings]],
  ['settings-name-too-long', 'home', [settings, type('#settings-name', 'A name that goes on and on and on, far past what a card can hold'), click('#settings-save-name')]],
  ['settings-flag', 'flag', [settings]],
  ['settings-limits', 'home&limits=1', [settings]],
  ['side-collapsed', 'board', [collapse]],
  ['side-collapsed-members', 'board-requests', [collapse, people]],
  ['side-collapsed-home', 'home', [collapse]],
  ['side-collapsed-settings', 'board', [collapse, settings]],
  ['side-own-network', 'flag', []],
  ['side-own-network-board', 'board-flag', []],
  ['side-this-computer', 'local', []],
  ['side-this-computer-board', 'board-local', []],
  ['side-starting', 'starting', []],
  ['side-network-failed', 'board-net-failed', []],
  ['es-home', 'home', [], { lang: 'es' }],
  ['es-board', 'board', [], { lang: 'es' }],
  ['es-people', 'board-requests', [people], { lang: 'es' }],
  ['es-activity', 'board', [activity], { lang: 'es' }],
  ['es-sync', 'board-waiting', [click('.syncbar__state')], { lang: 'es' }],
  ['es-invite', 'board', [click('.pill--primary')], { lang: 'es' }],
  ['es-card', 'board', [firstCard], { lang: 'es' }],
  ['es-settings', 'home', [settings], { lang: 'es' }],
  ['es-ended', 'ended-join-denied', [], { lang: 'es' }],
  ['pt-home', 'home', [], { lang: 'pt' }],
  ['pt-board', 'board', [], { lang: 'pt' }],
  ['pt-people', 'board-requests', [people], { lang: 'pt' }],
  ['pt-activity', 'board', [activity], { lang: 'pt' }],
  ['pt-sync', 'board-waiting', [click('.syncbar__state')], { lang: 'pt' }],
  ['pt-invite', 'board', [click('.pill--primary')], { lang: 'pt' }],
  ['pt-removed', 'board-removed', [], { lang: 'pt' }],
  ['pt-joining', 'joining-waiting', [], { lang: 'pt' }],
  ['pt-settings', 'home', [settings], { lang: 'pt' }],
]

/** The notch page: name, mock scene, extra query. */
const NOTCH = [
  ['change', 'board', ''],
  ['change-more', 'board', '&unseen=4'],
  ['quiet', 'board', '&change=none'],
  ['no-preview', 'board', '&preview=off'],
  ['no-preview-quiet', 'board', '&preview=off&change=none'],
  ['waiting-settlers', 'board-waiting', ''],
  ['single', 'board-empty', ''],
  ['removed', 'board-removed', ''],
  ['error', 'board-error', ''],
  ['hostile', 'board-hostile', ''],
  ['off', 'board', '&notch=off'],
  ['joining', 'joining-waiting', ''],
  ['nothing', 'home', ''],
]

const { send, evaluate, errors, close } = await launch()
let count = 0
// The saved language is written before any script of the page runs, so one load is enough; the sidebar starts expanded.
let languageScript = null
const setLanguage = async (lang) => {
  if (languageScript) {
    await send('Page.removeScriptToEvaluateOnNewDocument', { identifier: languageScript })
  }
  languageScript = (await send('Page.addScriptToEvaluateOnNewDocument', { source: `localStorage.setItem(${JSON.stringify(PREFS)}, JSON.stringify({ lang: ${JSON.stringify(lang)} })); localStorage.removeItem(${JSON.stringify(SIDEBAR)})` })).identifier
}
const problems = []
const check = async (label, tests) => {
  for (const test of tests) {
    const found = await evaluate(test)
    if (found) {
      problems.push(`${label}: ${found}`)
    }
  }
  for (const text of errors.splice(0)) {
    problems.push(`${label}: console: ${text.slice(0, 200)}`)
  }
}
try {
  for (const [name, scene, steps, options = {}] of SHOTS) {
    if (filter && !name.includes(filter)) {
      continue
    }
    const lang = options.lang ?? 'en'
    for (const theme of ['dark', 'light']) {
      for (const [sizeName, [width, height, ratio]] of [...Object.entries(SIZES), ['huge', [...HUGE, 1]]]) {
        if (lang !== 'en' && !TRANSLATED.includes(sizeName)) {
          continue
        }
        if (sizeName === 'huge' && theme !== 'dark') {
          continue
        }
        await send('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: ratio, mobile: false })
        await send('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-color-scheme', value: theme }, { name: 'prefers-reduced-motion', value: 'reduce' }] })
        await setLanguage(lang)
        errors.splice(0)
        await send('Page.navigate', { url: `${base}/?mock=${scene}&v=${count}` })
        await until(evaluate, `!!document.querySelector('.split, .board')`, 3000)
        await sleep(250)
        for (const step of steps) {
          await evaluate(step)
          await sleep(200)
        }
        await sleep(scene === 'board-toast' ? 500 : 150)
        await check(`${name} ${theme} ${sizeName}`, [OVERFLOW, HIDDEN, LEAK, FILLS, INSET])
        if (sizeName !== 'huge') {
          const shot = await send('Page.captureScreenshot', { format: 'png' })
          const dir = join(out, theme, sizeName)
          mkdirSync(dir, { recursive: true })
          writeFileSync(join(dir, `${name}.png`), Buffer.from(shot.data, 'base64'))
        }
        count++
      }
    }
  }
  // The notch page: dark only, at the 660 x 92 content area of the notch window.
  for (const lang of ['en', 'es', 'pt']) {
    for (const [state, scene, extra] of NOTCH) {
      const name = `${lang === 'en' ? '' : `${lang}-`}${state}`
      if (filter && !`notch-${name}`.includes(filter)) {
        continue
      }
      await send('Emulation.setDeviceMetricsOverride', { width: 660, height: 92, deviceScaleFactor: 2, mobile: false })
      await send('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-color-scheme', value: 'light' }, { name: 'prefers-reduced-motion', value: 'reduce' }] })
      await setLanguage(lang)
      errors.splice(0)
      await send('Page.navigate', { url: `${base}/notch/?mock=${scene}${extra}&v=${count}` })
      await until(evaluate, `!!document.querySelector('.notch')`, 4000)
      await sleep(500)
      const tall = await evaluate(`document.documentElement.scrollHeight > window.innerHeight || document.documentElement.scrollWidth > window.innerWidth ? 'content is larger than the notch area' : ''`)
      if (tall) {
        problems.push(`notch ${name}: ${tall}`)
      }
      // The notch page must never receive an invite, and without the preview it holds no board text.
      const held = await evaluate(`/koloinv0/.test(document.documentElement.outerHTML) ? 'the notch page holds an invite' : ''`)
      if (held) {
        problems.push(`notch ${name}: ${held}`)
      }
      if (extra.includes('preview=off')) {
        const text = await evaluate(`/Release|import file|Doing/.test(document.body.innerText + [...document.querySelectorAll('[title]')].map((el) => el.title).join(' ')) ? 'board text is in the notch with the preview off' : ''`)
        if (text) {
          problems.push(`notch ${name}: ${text}`)
        }
      }
      await check(`notch ${name}`, [OVERFLOW, HIDDEN])
      const shot = await send('Page.captureScreenshot', { format: 'png' })
      mkdirSync(join(out, 'notch'), { recursive: true })
      writeFileSync(join(out, 'notch', `${name}.png`), Buffer.from(shot.data, 'base64'))
      count++
    }
  }
} finally {
  await close()
}
console.log(`${count} screenshots in ${out}`)
if (problems.length > 0) {
  console.log(problems.join('\n'))
  process.exitCode = 1
}

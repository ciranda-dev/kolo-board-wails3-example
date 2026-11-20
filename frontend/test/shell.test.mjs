import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { hasChrome, launch, sleep, until } from '../dev/chrome.mjs'

// The shell of the window in a headless Chrome against the mock on a loopback dev server: the sidebar and its rail, the
// choice kept in storage, Settings from your card, the members panel docked and as an overlay, and the board with the
// same actions as before.
const css = readFileSync(new URL('../src/board.css', import.meta.url), 'utf8')

test('the sidebar buttons show a focus ring', () => {
  for (const selector of ['.iconbtn:focus-visible', '.pill:focus-visible', '.conv:focus-visible', '.mainhead__rename:focus-visible']) {
    assert.ok(css.includes(selector), selector)
  }
})

test('the shell: sidebar, rail, storage, settings page, members overlay and the board actions', { skip: !hasChrome() && 'Google Chrome is not here', timeout: 180000 }, async () => {
  const { createServer } = await import('vite')
  const server = await createServer({ root: fileURLToPath(new URL('..', import.meta.url)), logLevel: 'silent', server: { host: '127.0.0.1', port: 9395, strictPort: false } })
  await server.listen()
  const base = `http://127.0.0.1:${server.httpServer.address().port}`
  const chrome = await launch()
  const { send, evaluate } = chrome
  let n = 0
  const size = (width, height) => send('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: 1, mobile: false })
  const open = async (scene) => {
    await send('Page.navigate', { url: `${base}/?mock=${scene}&v=${n++}` })
    assert.ok(await until(evaluate, `!!document.querySelector('.board, .split')`, 8000), `${scene} did not load`)
    await sleep(150)
  }
  const click = async (selector) => {
    await evaluate(`document.querySelector(${JSON.stringify(selector)}).click()`)
    await sleep(150)
  }
  const key = async (name, code) => {
    await send('Input.dispatchKeyEvent', { type: 'keyDown', key: name, code: name, windowsVirtualKeyCode: code })
    await send('Input.dispatchKeyEvent', { type: 'keyUp', key: name, code: name, windowsVirtualKeyCode: code })
    await sleep(150)
  }
  const railWidth = () => evaluate(`Math.round(document.querySelector('.convs').getBoundingClientRect().width)`)
  const sideways = () => evaluate(`document.documentElement.scrollWidth > window.innerWidth`)
  try {
    await send('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-reduced-motion', value: 'reduce' }] })
    await size(1440, 900)
    await open('board')

    // The sidebar holds what the app has: the board row (active), the network line, your card and three buttons.
    assert.equal(await evaluate(`document.querySelectorAll('.convs button').length`), 3)
    assert.equal(await evaluate(`document.querySelector('.conv').getAttribute('aria-current')`), 'true')
    assert.equal(await evaluate(`document.querySelector('.conv__name').textContent`), 'Release 2.4')
    assert.match(await evaluate(`document.querySelector('.conv__line').textContent`), /11 settled/)
    assert.equal(await evaluate(`document.querySelector('.convs__net').textContent`), 'Public network · Ready')
    assert.match(await evaluate(`document.querySelector('.me').textContent`), /Ana Ribeiro/)
    assert.doesNotMatch(await evaluate(`document.querySelector('.convs').innerText`), /\d+\.\d+\.\d+\.\d+/)
    assert.equal(await evaluate(`document.querySelector('.mainhead__sub').textContent`), '4 members · 4 connected')
    assert.ok(await evaluate(`document.querySelector('.convs').getBoundingClientRect().height >= window.innerHeight - 31`), 'the sidebar runs the full height')
    assert.ok(await evaluate(`document.querySelector('.convs').getBoundingClientRect().width > 200`))

    // Collapse: a rail, aria says so, the choice is in storage and survives a reload.
    await click('#side-panel .iconbtn')
    assert.equal(await evaluate(`document.querySelector('#side-panel .iconbtn').getAttribute('aria-expanded')`), 'false')
    assert.ok((await railWidth()) <= 80)
    assert.equal(await evaluate(`localStorage.getItem('kolo-board.sidebar')`), 'collapsed')
    assert.equal(await evaluate(`document.querySelector('.conv').getAttribute('aria-label')`), 'Release 2.4')
    await send('Page.reload')
    assert.ok(await until(evaluate, `!!document.querySelector('.board')`, 8000))
    await sleep(150)
    assert.ok((await railWidth()) <= 80, 'still a rail after a reload')
    await click('#side-panel .iconbtn')
    assert.ok((await railWidth()) > 200)
    assert.equal(await evaluate(`localStorage.getItem('kolo-board.sidebar')`), 'expanded')
    assert.equal(await sideways(), false)

    // Settings from your card: a page in the main column, the sidebar stays, Escape closes it and the focus goes back.
    await evaluate(`document.querySelector('.me .iconbtn').focus()`)
    await click('.me .iconbtn')
    assert.ok(await evaluate(`!!document.querySelector('.main .modal--page')`))
    assert.equal(await evaluate(`document.querySelector('.me .iconbtn').getAttribute('aria-pressed')`), 'true')
    assert.ok(await evaluate(`document.querySelector('.convs').getBoundingClientRect().width > 200`))
    await key('Escape', 27)
    assert.equal(await evaluate(`!!document.querySelector('.modal')`), false)
    assert.equal(await evaluate(`document.activeElement.closest('.me') !== null`), true)

    // The board still has its actions: a card opens its sheet, Invite opens its sheet, a card moves with Alt and an arrow.
    await click('.kcard')
    assert.ok(await evaluate(`!!document.querySelector('.modal--sheet [role="dialog"]')`))
    await key('Escape', 27)
    await click('.pill--primary')
    assert.match(await evaluate(`document.querySelector('.modal--sheet [role="dialog"]').getAttribute('aria-label')`), /nvite/)
    await key('Escape', 27)
    const counts = `[...document.querySelectorAll('.lane')].map((l) => l.querySelectorAll('[data-card]').length).join(',')`
    const before = await evaluate(counts)
    await evaluate(`(() => { const el = document.querySelector('.kcard'); el.focus(); el.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', altKey: true, bubbles: true })) })()`)
    await sleep(500)
    assert.notEqual(await evaluate(counts), before, 'the card did not move')

    // Members docked at 1440 px.
    await click('.pill--members')
    assert.ok(await evaluate(`!!document.querySelector('.drawer')`))
    assert.equal(await evaluate(`document.querySelector('.pill--members').getAttribute('aria-expanded')`), 'true')
    assert.equal(await sideways(), false)

    // 800 px: the sidebar shrinks, Members is an overlay, Escape closes it and the focus goes back to its button.
    await size(800, 600)
    await open('board-requests')
    assert.ok((await railWidth()) <= 240)
    assert.equal(await sideways(), false)
    await evaluate(`document.querySelector('.pill--members').focus()`)
    await click('.pill--members')
    assert.ok(await evaluate(`!!document.querySelector('.modal--side') && !document.querySelector('.drawer')`))
    assert.ok(await evaluate(`!!document.querySelector('.modal--side .row--request')`))
    await key('Escape', 27)
    assert.equal(await evaluate(`!!document.querySelector('.modal')`), false)
    assert.equal(await evaluate(`document.activeElement.classList.contains('pill--members')`), true)
    await click('.pill--members')
    await evaluate(`document.querySelector('.modal--side').dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))`)
    await sleep(150)
    assert.equal(await evaluate(`!!document.querySelector('.modal')`), false, 'a click outside closes the overlay')

    // 2000 px and the other screens: no sideways scroll, the sidebar is there without a board too.
    await size(2000, 1240)
    await open('board')
    assert.equal(await sideways(), false)
    await open('home')
    assert.equal(await evaluate(`document.querySelector('.convs__empty').textContent`), 'No board yet.')
    assert.equal(await evaluate(`document.querySelectorAll('.conv').length`), 0)
    await open('local')
    assert.equal(await evaluate(`document.querySelector('.convs__net').textContent`), 'This computer only · Ready')
    await open('flag')
    assert.equal(await evaluate(`document.querySelector('.convs__net').textContent`), 'Your own network · Ready')
    assert.doesNotMatch(await evaluate(`document.body.innerText`), /203\.0\.113/)

    // Storage that throws: the window works and starts expanded.
    const blocked = await send('Page.addScriptToEvaluateOnNewDocument', { source: `Object.defineProperty(window, 'localStorage', { get() { throw new Error('blocked') } })` })
    await open('board')
    assert.ok((await railWidth()) > 200)
    await click('#side-panel .iconbtn')
    assert.ok((await railWidth()) <= 80)
    await send('Page.removeScriptToEvaluateOnNewDocument', { identifier: blocked.identifier })
  } finally {
    await chrome.close()
    await server.close()
  }
})

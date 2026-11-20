import test from 'node:test'
import assert from 'node:assert/strict'
import { fileURLToPath } from 'node:url'
import { hasChrome, launch, sleep, until } from '../dev/chrome.mjs'

// The start race in the real window, against the mock on a loopback dev server: in ?mock=race the
// network gets ready while Start runs, the board is renamed while Board() runs, an invite is made
// while Invites() runs and a change reaches the notch while Notch() runs, and each event goes out
// before its answer. The window must keep the newer state every time, and a reload while ready
// (only snapshots, no event) must show it too.
test('a snapshot that arrives after a newer event never undoes it', { skip: !hasChrome() && 'Google Chrome is not here', timeout: 120000 }, async () => {
  const { createServer } = await import('vite')
  const server = await createServer({ root: fileURLToPath(new URL('..', import.meta.url)), logLevel: 'silent', server: { host: '127.0.0.1', port: 9393, strictPort: false } })
  await server.listen()
  const base = `http://127.0.0.1:${server.httpServer.address().port}`
  const chrome = await launch()
  const text = (selector) => chrome.evaluate(`document.querySelector(${JSON.stringify(selector)})?.textContent ?? ''`)
  try {
    await chrome.send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false })

    // The event before the answer.
    await chrome.send('Page.navigate', { url: `${base}/?mock=race` })
    assert.ok(await until(chrome.evaluate, `!!document.querySelector('.board')`, 8000), 'the board did not load')
    // Every answer of the scene comes 300 ms after its call: wait until the last one is in.
    await sleep(1000)
    assert.match(await text('.mainhead__sub'), /connected/)
    assert.match(await text('.convs__net'), /Ready/)
    assert.equal(await text('.mainhead__titletext'), 'Release 2.4 final')
    await chrome.evaluate(`document.querySelector('.pill--members').click()`)
    await sleep(200)
    assert.equal(await chrome.evaluate(`document.querySelectorAll('.row--invite').length`), 4)

    // The notch page: the change that arrived while Notch() ran stays.
    await chrome.send('Emulation.setDeviceMetricsOverride', { width: 660, height: 92, deviceScaleFactor: 1, mobile: false })
    await chrome.send('Page.navigate', { url: `${base}/notch/?mock=race&v=1` })
    assert.ok(await until(chrome.evaluate, `!!document.querySelector('.notch__name')`, 8000), 'the notch did not load')
    await sleep(800)
    assert.match(await text('.notch__name'), /Check the race/)

    // The answer before the event, and a reload while ready: a plain scene has only snapshots.
    await chrome.send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false })
    for (const v of [2, 3]) {
      await chrome.send('Page.navigate', { url: `${base}/?mock=board&v=${v}` })
      assert.ok(await until(chrome.evaluate, `!!document.querySelector('.board')`, 8000), 'the board did not load')
      await sleep(200)
      assert.doesNotMatch(await text('.convs__net'), /Starting/)
      assert.equal(await text('.mainhead__titletext'), 'Release 2.4')
    }
    await chrome.send('Page.reload')
    assert.ok(await until(chrome.evaluate, `!!document.querySelector('.board')`, 8000), 'the board did not load after a reload')
    await sleep(200)
    assert.doesNotMatch(await text('.convs__net'), /Starting/)
    // A join whose answer comes first: the events that follow move it on.
    await chrome.send('Page.navigate', { url: `${base}/?mock=live&v=4` })
    assert.ok(await until(chrome.evaluate, `!!document.querySelector('#join-invite')`, 8000), 'home did not load')
    await chrome.evaluate(`(() => { const el = document.querySelector('#join-invite'); Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(el, 'koloinv0abcdefgh'); el.dispatchEvent(new Event('input', { bubbles: true })) })()`)
    await sleep(100)
    await chrome.evaluate(`document.querySelector('#join-submit').click()`)
    assert.ok(await until(chrome.evaluate, `document.querySelector('.step.is-now')?.textContent.includes('Waiting for the answer')`, 8000), 'the join did not move on to waiting')
    // The whole join from the empty state, with the joiner's payloads before its roster arrives (people: null): a pane is
    // on screen at every step and the board shows at the end.
    await chrome.send('Page.navigate', { url: `${base}/?mock=live&nulls&v=5` })
    assert.ok(await until(chrome.evaluate, `!!document.querySelector('#join-invite')`, 8000), 'home did not load')
    await chrome.evaluate(`(() => { const el = document.querySelector('#join-invite'); Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(el, 'koloinv0abcdefgh'); el.dispatchEvent(new Event('input', { bubbles: true })) })()`)
    await chrome.evaluate(`document.querySelector('#join-submit').click()`)
    let seen = ''
    for (let i = 0; i < 140 && !seen.includes('board'); i++) {
      const now = await chrome.evaluate(`document.querySelector('.board') ? 'board' : document.querySelector('.convs') && document.querySelector('[data-pane]') ? 'pane' : 'blank'`)
      assert.notEqual(now, 'blank', 'the window went blank during the join')
      seen = now
      await sleep(50)
    }
    assert.equal(seen, 'board', 'the joined board never showed')

    // A render that throws shows the error pane with a Copy button, and the error goes to the log (ReportError).
    await chrome.send('Page.navigate', { url: `${base}/?mock=crash&v=6` })
    assert.ok(await until(chrome.evaluate, `!!document.querySelector('.crash')`, 8000), 'no error pane for a render that throws')
    assert.match(await text('.crash__text'), /TypeError/)
    assert.ok(await chrome.evaluate(`[...document.querySelectorAll('.crash button')].some((b) => b.textContent === 'Copy the error')`))
    assert.ok(await until(chrome.evaluate, `(window.koloReports ?? []).some((r) => r.includes('TypeError'))`, 4000), 'the error was not reported to the log')
    // An error outside a render (a handler, a promise) is shown over a window that goes on, and reported.
    await chrome.send('Page.navigate', { url: `${base}/?mock=board&v=7` })
    assert.ok(await until(chrome.evaluate, `!!document.querySelector('.board')`, 8000), 'the board did not load')
    await chrome.evaluate(`setTimeout(() => { throw new Error('boom in a handler') }, 0); Promise.reject(new Error('boom in a promise')); true`)
    assert.ok(await until(chrome.evaluate, `!!document.querySelector('.crash') && (window.koloReports ?? []).length >= 2`, 4000), 'no pane or no report for a window error')
    assert.match(await text('.crash__text'), /boom/)
    await chrome.evaluate(`[...document.querySelectorAll('.crash button')].find((b) => b.textContent === 'Dismiss').click()`)
    await sleep(100)
    assert.ok(await chrome.evaluate(`!document.querySelector('.crash') && !!document.querySelector('.board')`), 'the window did not go on after Dismiss')
  } finally {
    await chrome.close()
    await server.close()
  }
})

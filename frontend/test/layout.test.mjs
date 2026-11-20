import test from 'node:test'
import assert from 'node:assert/strict'
import { fileURLToPath } from 'node:url'
import { FILLS, hasChrome, launch, sleep, until } from '../dev/chrome.mjs'

// The owner's rule: every screen uses the whole window. This loads the screens from a dev server
// with the mock (sample data, loopback only) in a headless Chrome and measures them: at 1440 px and
// wider the panes of a screen span at least 90% of the width and of the height under the header,
// the columns of a board span 90% of the board, and no large bordered card floats on the page.
const SCREENS = [
  ['name', []],
  ['home', []],
  ['joining-waiting', []],
  ['ended-join-denied', []],
  ['fatal', []],
  ['board', []],
  ['board-empty', []],
  ['board-no-columns', []],
  ['board-many', []],
  ['board-removed', []],
  ['board-requests', [`document.querySelector('.pill--members').click()`]],
  ['board', [`document.querySelector('.pill--activity').click()`]],
  ['home', [`document.querySelector('.me .iconbtn').click()`]],
  ['board', [`document.querySelector('#side-panel .iconbtn').click()`]],
]
const LARGE = [[1440, 900], [2000, 1240], [2560, 1440]]
const SMALL = [[640, 520], [860, 560], [1000, 618]]

test('every screen fills the window at 1440 px and wider, and fits the smallest one', { skip: !hasChrome() && 'Google Chrome is not here', timeout: 180000 }, async () => {
  const { createServer } = await import('vite')
  const server = await createServer({ root: fileURLToPath(new URL('..', import.meta.url)), logLevel: 'silent', server: { host: '127.0.0.1', port: 9391, strictPort: false } })
  await server.listen()
  const base = `http://127.0.0.1:${server.httpServer.address().port}`
  const chrome = await launch()
  const problems = []
  try {
    await chrome.send('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-reduced-motion', value: 'reduce' }] })
    let n = 0
    for (const [width, height] of [...LARGE, ...SMALL]) {
      await chrome.send('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: 1, mobile: false })
      for (const [scene, steps] of SCREENS) {
        await chrome.send('Page.navigate', { url: `${base}/?mock=${scene}&v=${n++}` })
        assert.ok(await until(chrome.evaluate, `!!document.querySelector('.split, .board')`, 8000), `${scene} did not load`)
        await sleep(120)
        for (const step of steps) {
          await chrome.evaluate(step)
          await sleep(150)
        }
        const label = `${scene}${steps.length ? ' +panel' : ''} at ${width}x${height}`
        const fills = await chrome.evaluate(FILLS)
        if (fills) {
          problems.push(`${label}: ${fills}`)
        }
        const sideways = await chrome.evaluate(`document.documentElement.scrollWidth > window.innerWidth || document.documentElement.scrollHeight > window.innerHeight`)
        if (sideways) {
          problems.push(`${label}: the page is larger than the window`)
        }
      }
    }
    // The measure itself must be able to fail: a board squeezed into a third of the window is caught.
    await chrome.send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false })
    await chrome.send('Page.navigate', { url: `${base}/?mock=board&v=${n++}` })
    await until(chrome.evaluate, `!!document.querySelector('.board')`, 8000)
    await chrome.evaluate(`document.querySelector('.board').style.maxWidth = '480px'`)
    assert.match(await chrome.evaluate(FILLS), /% of the width of the main column/)
  } finally {
    await chrome.close()
    await server.close()
  }
  assert.deepEqual(problems, [])
})

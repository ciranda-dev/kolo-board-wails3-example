import test from 'node:test'
import assert from 'node:assert/strict'
import { fileURLToPath } from 'node:url'
import { hasChrome, launch, sleep, until } from '../dev/chrome.mjs'
import { housingOf, showHousing } from '../src/housing.js'

// The notch area the page fills below the housing.
const WIDTH = 660
const HEIGHT = 92

// The notch page as Go opens it (taller by the housing, ?top=&housing= in its address), measured in a
// headless Chrome with the mock: nothing of the page lies under the housing, in the middle of the top
// edge, and below it the page is laid out as on a screen without one.
const BOXES = `JSON.stringify([...document.querySelectorAll('.notch *')].map((el) => {
  const r = el.getBoundingClientRect()
  return { name: el.getAttribute('class') || el.tagName, left: r.left, top: r.top, right: r.right, bottom: r.bottom }
}).filter((b) => b.right > b.left && b.bottom > b.top))`

test('the content of the notch page never lies under the housing', { skip: !hasChrome() && 'Google Chrome is not here', timeout: 60000 }, async () => {
  const { createServer } = await import('vite')
  const server = await createServer({ root: fileURLToPath(new URL('..', import.meta.url)), logLevel: 'silent', server: { host: '127.0.0.1', port: 9392, strictPort: false } })
  await server.listen()
  const base = `http://127.0.0.1:${server.httpServer.address().port}`
  const chrome = await launch()
  const problems = []
  try {
    let plain = null
    for (const [top, width] of [[0, 0], [32, 200], [38, 185]]) {
      await chrome.send('Emulation.setDeviceMetricsOverride', { width: WIDTH, height: HEIGHT + top, deviceScaleFactor: 1, mobile: false })
      const query = top > 0 ? `&top=${top}&housing=${width}` : ''
      await chrome.send('Page.navigate', { url: `${base}/notch/?mock=board${query}&v=${top}` })
      assert.ok(await until(chrome.evaluate, `!!document.querySelector('.notch__name')`, 8000), `top ${top}: the notch did not load`)
      await sleep(120)
      const boxes = JSON.parse(await chrome.evaluate(BOXES))
      plain ??= boxes
      assert.equal(boxes.length, plain.length, `top ${top}: another page`)
      const housing = { left: (WIDTH - width) / 2, right: (WIDTH + width) / 2, bottom: top }
      boxes.forEach((b, i) => {
        if (b.left < housing.right && housing.left < b.right && b.top < housing.bottom) {
          problems.push(`top ${top}, housing ${width}: ${b.name} is under the housing`)
        }
        if (Math.abs(b.top - top - plain[i].top) > 0.5 || Math.abs(b.left - plain[i].left) > 0.5) {
          problems.push(`top ${top}, housing ${width}: ${b.name} is not where it is without a housing, ${top} px lower`)
        }
      })
    }
  } finally {
    await chrome.close()
    await server.close()
  }
  assert.deepEqual(problems, [])
})

test('anything but two counts of pixels is no housing', () => {
  for (const search of ['', '?top=32', '?housing=200', '?top=0&housing=200', '?top=-3&housing=200', '?top=3.5&housing=200', '?top=x&housing=200', '?top=32&housing=99999', '?top=1e2&housing=200']) {
    assert.deepEqual(housingOf(search), { top: 0, width: 0 }, search)
  }
  assert.deepEqual(housingOf('?mock=board&top=38&housing=185'), { top: 38, width: 185 })
})

test('the styles get the housing as CSS variables', () => {
  const set = {}
  showHousing({ style: { setProperty: (k, v) => { set[k] = v } } }, { top: 32, width: 200 })
  assert.deepEqual(set, { '--housing-top': '32px', '--housing-width': '200px' })
})

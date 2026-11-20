import test from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { existsSync, readFileSync } from 'node:fs'
import { extname, join, normalize } from 'node:path'
import { fileURLToPath } from 'node:url'
import { hasChrome, launch, sleep, until } from '../dev/chrome.mjs'

// The built pages (dist, after `npm run build`) carry a content security policy in a meta tag. This
// serves them on loopback, loads both in a headless Chrome and checks that the policy is there,
// that it blocks nothing the pages need (they render and their stylesheet applies), and that the
// build has no dev mock, no inline script or style and no address outside the app.
const dist = fileURLToPath(new URL('../dist/', import.meta.url))
const built = existsSync(join(dist, 'index.html')) && existsSync(join(dist, 'notch', 'index.html'))
const POLICY = "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'"
const TYPES = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml' }

test('the built pages carry the policy and no inline code', { skip: !built && 'run npm run build first' }, () => {
  for (const page of ['index.html', 'notch/index.html']) {
    const html = readFileSync(join(dist, page), 'utf8')
    assert.ok(html.includes(`<meta http-equiv="Content-Security-Policy" content="${POLICY}"`), `${page} has the policy`)
    assert.ok(!/<script(?![^>]*\bsrc=)|<style|\sstyle=|\son\w+=/.test(html), `${page} has inline code`)
    assert.ok(!/https?:\/\//.test(html), `${page} names an address`)
  }
  assert.ok(existsSync(join(dist, '.gitkeep')), 'dist/.gitkeep is kept')
})

test('the build has no dev mock and reaches for nothing outside the app', { skip: !built && 'run npm run build first' }, () => {
  const html = readFileSync(join(dist, 'index.html'), 'utf8') + readFileSync(join(dist, 'notch', 'index.html'), 'utf8')
  const files = [...new Set([...html.matchAll(/(?:src|href)="\/(assets\/[^"]+\.(?:js|css))"/g)].map((m) => m[1]))]
  assert.ok(files.length >= 3)
  for (const file of files) {
    const text = readFileSync(join(dist, file), 'utf8')
    assert.ok(!/Triage report|Marta Oliveira|koloinv0qx7/.test(text), `${file} holds the sample data of the mock`)
    assert.ok(!/@import|url\(\s*['"]?https?:|fonts\.googleapis|cdn\./.test(text), `${file} loads something from outside`)
  }
})

test('the built pages render under their policy without a violation', { skip: (!built && 'run npm run build first') || (!hasChrome() && 'Google Chrome is not here'), timeout: 60000 }, async () => {
  const server = createServer((req, res) => {
    const path = normalize(decodeURIComponent(new URL(req.url, 'http://x').pathname))
    const file = join(dist, path.endsWith('/') ? `${path}index.html` : path)
    if (!file.startsWith(dist) || !existsSync(file) || !TYPES[extname(file)]) {
      res.writeHead(404).end()
      return
    }
    res.writeHead(200, { 'content-type': TYPES[extname(file)] }).end(readFileSync(file))
  })
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve))
  const base = `http://127.0.0.1:${server.address().port}`
  const chrome = await launch()
  try {
    await chrome.send('Page.addScriptToEvaluateOnNewDocument', { source: `window.__blocked = []; document.addEventListener('securitypolicyviolation', (e) => window.__blocked.push(e.violatedDirective + ' ' + e.blockedURI))` })
    for (const [page, root] of [['/', '.app'], ['/notch/', '.notch']]) {
      chrome.errors.splice(0)
      await chrome.send('Page.navigate', { url: `${base}${page}` })
      assert.ok(await until(chrome.evaluate, `!!document.querySelector(${JSON.stringify(root)})`, 8000), `${page} rendered`)
      await sleep(500)
      assert.equal(await chrome.evaluate(`document.querySelector('meta[http-equiv="Content-Security-Policy"]').content`), POLICY)
      assert.deepEqual(await chrome.evaluate(`window.__blocked`) ?? [], [], `${page}: the policy blocked something`)
      assert.deepEqual(chrome.errors.filter((text) => /Content Security Policy|Refused to/i.test(text)), [], page)
      // The stylesheet applied: the page has the background of the theme, not the browser's white.
      assert.match(await chrome.evaluate(`getComputedStyle(document.documentElement).backgroundColor`), /rgb\((7, 11, 18|243, 247, 252|0, 0, 0)\)/, page)
      // The policy is live: an inline script added now is refused.
      await chrome.evaluate(`(() => { const s = document.createElement('script'); s.textContent = 'window.__ran = true'; document.head.appendChild(s) })()`)
      assert.equal(await chrome.evaluate(`window.__ran === true`), false, `${page}: an inline script ran`)
    }
  } finally {
    await chrome.close()
    await new Promise((resolve) => server.close(resolve))
  }
})

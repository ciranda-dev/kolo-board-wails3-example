// A headless Chrome driven through its DevTools protocol, with no dependency: for the screenshots
// and for the tests that need a real layout. Needs Google Chrome (or CHROME=<path>).
import { spawn } from 'node:child_process'
import { existsSync, mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

export const CHROME = process.env.CHROME || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
export const hasChrome = () => existsSync(CHROME)
export const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

/** Starts Chrome on a blank page. Returns `send` (a protocol call), `evaluate` (an expression in the page), the console errors seen, and `close`. */
export async function launch() {
  const profile = mkdtempSync(join(tmpdir(), 'kolo-board-chrome-'))
  const chrome = spawn(CHROME, ['--headless=new', '--remote-debugging-port=0', `--user-data-dir=${profile}`, '--no-first-run', '--hide-scrollbars', '--deny-permission-prompts', 'about:blank'], { stdio: ['ignore', 'ignore', 'pipe'] })
  const wsUrl = await new Promise((resolve, reject) => {
    let text = ''
    chrome.stderr.on('data', (chunk) => {
      text += chunk
      const m = text.match(/DevTools listening on (ws:\/\/\S+)/)
      if (m) {
        resolve(m[1])
      }
    })
    chrome.on('exit', () => reject(new Error('Chrome stopped before it was ready')))
  })
  const port = new URL(wsUrl).port
  const targets = await (await fetch(`http://127.0.0.1:${port}/json`)).json()
  const ws = new WebSocket(targets.find((x) => x.type === 'page').webSocketDebuggerUrl)
  await new Promise((resolve) => ws.addEventListener('open', resolve))
  let seq = 0
  const waiting = new Map()
  const errors = []
  ws.addEventListener('message', (event) => {
    const msg = JSON.parse(event.data)
    if (msg.id && waiting.has(msg.id)) {
      waiting.get(msg.id)(msg)
      waiting.delete(msg.id)
    } else if (msg.method === 'Runtime.exceptionThrown') {
      errors.push(msg.params.exceptionDetails.exception?.description ?? msg.params.exceptionDetails.text)
    } else if (msg.method === 'Runtime.consoleAPICalled' && msg.params.type === 'error') {
      errors.push(msg.params.args.map((a) => a.value ?? a.description ?? '').join(' '))
    } else if (msg.method === 'Log.entryAdded' && msg.params.entry.level === 'error') {
      errors.push(msg.params.entry.text)
    }
  })
  const send = (method, params = {}) =>
    new Promise((resolve, reject) => {
      const id = ++seq
      waiting.set(id, (msg) => (msg.error ? reject(new Error(`${method}: ${msg.error.message}`)) : resolve(msg.result)))
      ws.send(JSON.stringify({ id, method, params }))
    })
  const evaluate = async (expression) => {
    const result = await send('Runtime.evaluate', { expression, awaitPromise: true })
    if (result.exceptionDetails) {
      throw new Error(`${expression.slice(0, 80)}: ${result.exceptionDetails.exception?.description ?? result.exceptionDetails.text}`)
    }
    return result.result.value
  }
  await send('Page.enable')
  await send('Runtime.enable')
  await send('Log.enable')
  await send('Emulation.setFocusEmulationEnabled', { enabled: true })
  const close = async () => {
    ws.close()
    chrome.kill()
    await sleep(300)
    rmSync(profile, { recursive: true, force: true })
  }
  return { send, evaluate, errors, close }
}

/** Waits until an expression is true in the page, for at most `ms`. */
export async function until(evaluate, expression, ms = 4000) {
  for (let waited = 0; waited < ms; waited += 50) {
    if (await evaluate(expression)) {
      return true
    }
    await sleep(50)
  }
  return false
}

// At 1440 px and wider a screen must fill the window: the sidebar runs the full height under the title strip, the panes of
// the main column together span at least 90% of its width and of the height under the header, the columns of a board
// span 90% of the board, and no large rounded, bordered card sits on the page.
export const FILLS = `(() => {
  if (window.innerWidth < 1440) return ''
  const side = document.querySelector('.convs')
  if (side) {
    const r = side.getBoundingClientRect()
    if (r.left > 0.5 || r.bottom < window.innerHeight - 1 || r.top > 40) return 'the sidebar does not run the full height of the window'
  }
  const page = document.querySelector('.modal--page')
  const main = document.querySelector('.main').getBoundingClientRect()
  const room = (page ? page.querySelector('.modal__body') : document.querySelector('.main__body')).getBoundingClientRect()
  const rects = [...document.querySelectorAll(page ? '.modal--page .settings__group' : '.main__body [data-pane]')].map((el) => el.getBoundingClientRect()).filter((r) => r.width > 0 && r.height > 0)
  if (rects.length === 0) return 'no pane on screen'
  const width = Math.max(...rects.map((r) => r.right)) - Math.min(...rects.map((r) => r.left))
  const height = Math.max(...rects.map((r) => r.bottom)) - Math.min(...rects.map((r) => r.top))
  if (width < 0.9 * main.width) return 'the content is ' + Math.round(100 * width / main.width) + '% of the width of the main column'
  if (height < 0.9 * room.height) return 'the content is ' + Math.round(100 * height / room.height) + '% of the height it has'
  const lanes = [...document.querySelectorAll('.lane')].map((el) => el.getBoundingClientRect())
  if (lanes.length > 0) {
    const board = document.querySelector('.board').getBoundingClientRect()
    const span = Math.min(board.right, Math.max(...lanes.map((r) => r.right))) - Math.min(...lanes.map((r) => r.left))
    if (span < 0.9 * board.width) return 'the columns are ' + Math.round(100 * span / board.width) + '% of the board width'
    if (lanes[0].bottom < window.innerHeight - 1) return 'the columns stop ' + Math.round(window.innerHeight - lanes[0].bottom) + 'px above the bottom of the window'
  }
  for (const el of document.querySelectorAll('.main__body *, .modal--page .modal__body *')) {
    if (el.matches('textarea')) continue
    const style = getComputedStyle(el)
    const r = el.getBoundingClientRect()
    if (parseFloat(style.borderTopWidth) > 0 && parseFloat(style.borderTopLeftRadius) >= 12 && r.width > 480 && r.height > 240) return 'a card inside the page: ' + el.className
  }
  return ''
})()`

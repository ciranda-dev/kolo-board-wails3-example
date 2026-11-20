// WCAG 2.x contrast of every text and background pair of src/board.css, in both themes.
// Exits 1 when a pair is under its minimum, or when the two copies of the light theme differ.
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

const css = readFileSync(join(fileURLToPath(import.meta.url), '..', '..', 'src', 'board.css'), 'utf8')
const tokens = (block) => Object.fromEntries([...block.matchAll(/--([\w-]+):\s*(#[0-9a-fA-F]{6});/g)].map((m) => [m[1], m[2].toLowerCase()]))
const dark = tokens(css.match(/:root\s*\{([^}]*)\}/)[1])
const lightMedia = tokens(css.match(/:root:not\(\[data-theme="dark"\]\)\s*\{([^}]*)\}/)[1])
const light = tokens(css.match(/:root\[data-theme="light"\]\s*\{([^}]*)\}/)[1])

const lin = (c) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4)
const lum = (hex) => {
  const [r, g, b] = [1, 3, 5].map((i) => lin(parseInt(hex.slice(i, i + 2), 16) / 255))
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}
const ratio = (a, b) => {
  const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

// text, background, where, minimum
const pairs = [
  ['text', 'bg', 'text on the window, column titles', 4.5],
  ['text', 'panel', 'text on cards, header, drawer and dialogs', 4.5],
  ['text', 'soft', 'the initial on an avatar, a column while a card is dragged over it', 4.5],
  ['muted', 'bg', 'hints, counts, an empty column, rows of the drawer', 4.5],
  ['muted', 'panel', 'labels, card descriptions, names on cards', 4.5],
  ['muted', 'soft', 'a card count in a column that is a drop target', 4.5],
  ['on-accent', 'accent', 'primary button, request count', 4.5],
  ['on-accent', 'accent-2', 'primary button on hover', 4.5],
  ['danger', 'panel', 'error text on strips and dialogs', 4.5],
  ['danger', 'bg', 'error text on the window', 4.5],
  ['accent', 'panel', 'the word Syncing on a card, ring arc', 4.5],
  ['accent', 'bg', 'the syncing count, a syncing item in the activity list', 4.5],
  ['accent', 'soft', 'the drop slot, a syncing mark over a drop target', 3],
  ['accent-2', 'panel', 'focus ring on cards and dialogs', 3],
  ['accent-2', 'bg', 'focus ring on the window', 3],
  ['line2', 'panel', 'the dashed border of a syncing card is not its only sign: the word is', 1],
  ['text', 'active', 'the board row of the sidebar', 4.5],
  ['muted', 'active', 'the quiet line of the board row', 4.5],
  ['text', 'hover', 'a row of the sidebar under the pointer', 4.5],
  ['online', 'panel', 'the connected count under the board name, the network dot', 4.5],
  ['muted', 'panel', 'the network line and the quiet lines of the sidebar', 4.5],
  ['label-red', 'panel', 'label color on a card', 3],
  ['label-orange', 'panel', 'label color on a card', 3],
  ['label-yellow', 'panel', 'label color on a card', 3],
  ['label-green', 'panel', 'label color on a card', 3],
  ['label-blue', 'panel', 'label color on a card', 3],
  ['label-purple', 'panel', 'label color on a card', 3],
]

let failed = false
if (JSON.stringify(lightMedia) !== JSON.stringify(light)) {
  console.error('FAIL: the light tokens in the media query and in [data-theme="light"] differ')
  failed = true
}
for (const [name, theme] of [['dark', dark], ['light', { ...dark, ...light }], ['notch (dark on black)', { ...dark, bg: '#000000', panel: '#000000', soft: '#000000' }]]) {
  console.log(`\n${name}`)
  for (const [a, b, where, need] of pairs) {
    const r = ratio(theme[a], theme[b])
    const ok = r >= need
    failed ||= !ok
    console.log(`  ${a.padEnd(10)} ${theme[a]} on ${b.padEnd(10)} ${theme[b]}  ${r.toFixed(2).padStart(5)}:1  ${ok ? 'pass' : 'FAIL'} (${need})  ${where}`)
  }
}

// The dark ink on every avatar color of the sidebar and the header.
const avatars = readFileSync(join(fileURLToPath(import.meta.url), '..', '..', 'src', 'avatars.js'), 'utf8')
console.log('\navatars')
for (const [color] of avatars.matchAll(/#[0-9a-fA-F]{6}/g)) {
  const r = ratio(dark['disc-ink'], color.toLowerCase())
  failed ||= r < 4.5
  console.log(`  disc-ink ${dark['disc-ink']} on ${color}  ${r.toFixed(2).padStart(5)}:1  ${r >= 4.5 ? 'pass' : 'FAIL'} (4.5)`)
}
process.exit(failed ? 1 : 0)

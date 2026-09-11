import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { test } from '@playwright/test'

test('矢量图标统一留白、无需外部素材，并生成可检查的图标样张', async ({ page }, testInfo) => {
  const icons = []
  for (const file of ['AppIcon.vue', 'ProviderIcon.vue', 'GameIcon.vue']) {
    const source = await readFile(new URL('../src/components/' + file, import.meta.url), 'utf8')
    assert(source.includes('viewBox="0 0 24 24"'))
    assert(source.includes('aria-hidden="true"'))
    assert(!/<image|foreignObject|<script[^>]*src=/i.test(source))
    for (const match of source.matchAll(/^\s+(\w+): '([^']+)'/gm)) icons.push({ name: match[1], path: match[2] })
  }
  assert(icons.length >= 45)
  await page.setViewportSize({ width: 1000, height: 760 })
  await page.setContent('<style>body{margin:0;padding:25px;background:#f8f5ef;color:#65775a;font:12px system-ui}.icons{display:grid;grid-template-columns:repeat(8,1fr);gap:12px}.icon{display:grid;place-items:center;gap:14px;background:#fffefa;padding:18px 6px;border:1px solid #e8e2d7;border-radius:14px}svg{overflow:visible}</style><div class="icons"></div>')
  const boxes = await page.evaluate(icons => {
    const root = document.querySelector('.icons')
    return icons.map(icon => {
      const wrapper = document.createElement('div'); wrapper.className = 'icon'
      const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg')
      for (const [key, value] of Object.entries({ width: '24', height: '24', viewBox: '0 0 24 24', stroke: 'currentColor', fill: 'none', 'stroke-width': '1.65', 'stroke-linejoin': 'round', 'stroke-linecap': 'round' })) svg.setAttribute(key, value)
      const path = document.createElementNS(svg.namespaceURI, 'path'); path.setAttribute('d', icon.path); svg.append(path)
      const label = document.createElement('span'); label.textContent = icon.name
      wrapper.append(svg, label); root.append(wrapper)
      const b = path.getBBox(); return { name: icon.name, x: b.x, y: b.y, right: b.x + b.width, bottom: b.y + b.height, width: b.width, height: b.height }
    })
  }, icons)
  for (const b of boxes) assert(b.x >= .9 && b.y >= .9 && b.right <= 23.1 && b.bottom <= 23.1 && b.width > 0 && b.height > 0, 'clipped or empty glyph: ' + b.name)
  await page.screenshot({ path: testInfo.outputPath('svg-icon-sheet.png'), fullPage: true })
  const favicon = await readFile(new URL('../public/favicon.svg', import.meta.url), 'utf8')
  assert(Buffer.byteLength(favicon) < 600)
  assert(!/script|foreignObject|href=/i.test(favicon))
  for (const name of ['favicon.ico', 'apple-touch-icon.png']) assert((await readFile(new URL('../public/' + name, import.meta.url))).length < 10000)
})

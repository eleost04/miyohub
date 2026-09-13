import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { expect, test } from '@playwright/test'

for (const width of [320, 390, 1440]) test(`品牌图标 ${width}px：加载、登录、注册及初始化异常保持固定尺寸`, async ({ page }, testInfo) => {
  const origin = 'http://127.0.0.1:4177'
  await page.setViewportSize({ width, height: 900 })
  let releaseBootstrap
  const bootstrapGate = new Promise(resolve => { releaseBootstrap = resolve })
  let response = { status: 200, json: { ok: true, data: { auth: { need_auth: true, has_admin: true, registration_mode: 'closed' } } } }
  const unexpectedRequests = []
  await page.route('**/*', async route => {
    const url = new URL(route.request().url())
    if (url.origin !== origin) { unexpectedRequests.push(url.origin); await route.abort(); return }
    if (url.pathname === '/api/v1/bootstrap') {
      await bootstrapGate
      await route.fulfill(response)
      return
    }
    if (url.pathname.startsWith('/api/')) { unexpectedRequests.push(url.pathname); await route.abort(); return }
    await route.continue()
  })
  const logo = page.locator('.auth-card .brand-symbol')
  const checkSize = async () => {
    await expect(logo).toBeVisible()
    const box = await logo.boundingBox()
    expect({ width: box.width, height: box.height }).toEqual({ width: 34, height: 34 })
    await expect(logo).toHaveAttribute('width', '34')
    await expect(logo).toHaveAttribute('height', '34')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  }
  try {
    await page.goto(origin)
    await expect(page.getByText('正在加载…', { exact: true })).toBeVisible()
    await checkSize()
    await page.screenshot({ path: testInfo.outputPath(`brand-loading-${width}.png`), fullPage: true })
  } finally { releaseBootstrap() }
  await expect(page.getByRole('heading', { name: '登录 MiyoHub', exact: true })).toBeVisible()
  await checkSize()
  await page.screenshot({ path: testInfo.outputPath(`brand-login-${width}.png`), fullPage: true })
  await page.getByRole('button', { name: '还没有账号？创建一个', exact: true }).click()
  await expect(page.getByRole('heading', { name: '注册站点账号', exact: true })).toBeVisible()
  await checkSize()
  response.json.data.auth.has_admin = false
  await page.reload()
  await expect(page.getByRole('heading', { name: '创建管理员账号', exact: true })).toBeVisible()
  await checkSize()
  response = { status: 503, json: { ok: false, error: '测试初始化失败，请刷新后重试。' } }
  await page.reload()
  await expect(page.getByRole('alert')).toHaveText('测试初始化失败，请刷新后重试。')
  await checkSize()
  expect(unexpectedRequests).toEqual([])
})

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

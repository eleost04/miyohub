import assert from 'node:assert/strict'
import { expect, test } from './api-test.mjs'
import { choose, dismissGuide } from './ui.mjs'

for (const width of [320, 390, 1440]) test(`活动与自定义日历 ${width}px`, async ({ browser }, testInfo) => {
  const origin = 'http://127.0.0.1:5897', headers = { 'Content-Type': 'application/json', 'X-MiyoHub-Request': '1' }
  const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: 'reduce' })
  async function request(method, path, data) {
    const response = await context.request.fetch(origin + path, { method, headers, ...(data ? { data } : {}) })
    const body = await response.json(); assert(response.ok() && body.ok, body.error || path); return body.data
  }
  try {
    await request('POST', '/api/v1/auth/setup', { username: 'calendar-owner', password: 'local-test-only-123' })
    await dismissGuide(context)
    await request('POST', '/api/v1/accounts', { name: '日历测试账号', cookie: 'stuid=103001;cookie_token=calendar-fixture', disabled: false })
    const effects = [], errors = []; let reads = 0
    await context.route('**/*', route => {
      const url = new URL(route.request().url())
      if (url.origin !== origin || /^\/api\/v1\/(run|login\/|accounts\/check|push\/|shop\/)/.test(url.pathname)) { effects.push(url.pathname); return route.abort() }
      if (url.pathname === '/api/v1/game-record/calendar') {
        reads++
        return route.fulfill({ json: { ok: true, data: { game: 'genshin', roles: [], status: 'ok', observed_at: new Date().toISOString(), refresh_at: new Date(Date.now() + 1800000).toISOString(), cached: false, stale: false, calendar: { events: [{ id: 'official_fixture', title: '示例限定祈愿与较长活动标题', kind: 'pool', source: 'official', start_at: new Date(Date.now() - 86400000).toISOString(), end_at: new Date(Date.now() + 86400000).toISOString(), finished: null }], skipped: 0 } } } })
      }
      return route.continue()
    })
    const page = await context.newPage(); page.on('pageerror', e => errors.push(e.message))
    await page.goto(origin + '/#calendar')
    await expect(page.getByRole('heading', { name: '活动日历', exact: true }).last()).toBeVisible()
    assert.equal(reads, 0)
    await page.getByRole('button', { name: '添加自定义日程', exact: true }).click()
    const editor = page.getByRole('dialog', { name: '添加自定义日程', exact: true })
    await editor.getByLabel('日程名称', { exact: true }).fill('示例版本更新时间')
    await editor.getByRole('button', { name: '日程开始时间', exact: true }).click()
    const date = page.getByRole('dialog', { name: '日程开始时间', exact: true })
    await date.getByRole('button', { name: '应用时间', exact: true }).click()
    await editor.getByRole('button', { name: '保存日程', exact: true }).click()
    await expect(editor).toHaveCount(0)
    await expect(page.locator('.calendar-event').filter({ hasText: '示例版本更新时间' })).toBeVisible()
    assert.equal(reads, 0)
    await page.getByRole('button', { name: '读取官方活动', exact: true }).click()
    await expect(page.locator('.calendar-event')).toHaveCount(2)
    await choose(page, '日程类型筛选', 'pool')
    await expect(page.locator('.calendar-event')).toHaveCount(1)
    await choose(page, '日程类型筛选', 'all')
    await page.screenshot({ path: testInfo.outputPath('calendar.png') })
    await page.getByRole('button', { name: '添加自定义日程', exact: true }).click()
    await editor.getByRole('button', { name: '日程开始时间', exact: true }).click()
    await page.goBack(); await expect(date).toHaveCount(0); await expect(editor).toBeVisible()
    await page.goBack(); await expect(editor).toHaveCount(0)
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
    assert.equal(reads, 1); assert.deepEqual(effects, []); assert.deepEqual(errors, [])
  } finally { await context.close() }
})

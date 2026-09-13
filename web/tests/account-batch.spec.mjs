import assert from 'node:assert/strict'
import { expect, test } from './api-test.mjs'
import { choose, dismissGuide } from './ui.mjs'

for (const width of [320, 390, 1440]) test(`账号分组与批量时间 ${width}px`, async ({ browser }, testInfo) => {
  const origin = 'http://127.0.0.1:5897', headers = { 'Content-Type': 'application/json', 'X-MiyoHub-Request': '1' }
  const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: 'reduce' })
  async function request(method, path, data) {
    const response = await context.request.fetch(origin + path, { method, headers, ...(data ? { data } : {}) })
    const body = await response.json(); assert(response.ok() && body.ok, body.error || path); return body.data
  }
  try {
    await request('POST', '/api/v1/auth/setup', { username: 'batch-owner', password: 'local-test-only-123' })
    await dismissGuide(context)
    for (const [name, uid] of [['甲账号', '401001'], ['乙账号', '401002']]) await request('POST', '/api/v1/accounts', { name, cookie: 'stuid=' + uid + ';cookie_token=batch-fixture', disabled: true })
    const effects = [], errors = []
    await context.route('**/*', route => {
      const url = new URL(route.request().url())
      if (url.origin !== origin || /^\/api\/v1\/(run|login\/|accounts\/check|push\/|shop\/)/.test(url.pathname)) { effects.push(url.pathname); return route.abort() }
      return route.continue()
    })
    const page = await context.newPage(); page.on('pageerror', e => errors.push(e.message))
    await page.goto(origin)
    await page.getByRole('button', { name: '分组与批量', exact: true }).click()
    let dialog = page.getByRole('dialog', { name: '分组与批量操作', exact: true })
    await dialog.getByRole('checkbox', { name: /甲账号/ }).check()
    await dialog.getByLabel('分组名称', { exact: false }).fill('每日组')
    await dialog.getByRole('button', { name: '应用到所选账号', exact: true }).click()
    await expect(dialog).toHaveCount(0)
    await page.getByRole('group', { name: '账号分组', exact: true }).getByRole('button', { name: '每日组', exact: true }).click()
    await expect(page.locator('.account-card')).toHaveCount(1)
    await page.getByRole('button', { name: '分组与批量', exact: true }).click()
    dialog = page.getByRole('dialog', { name: '分组与批量操作', exact: true })
    await dialog.getByRole('button', { name: '选择当前分组', exact: true }).click()
    await choose(page, '批量操作', 'schedule')
    await choose(page, '批量签到时间 · 小时', '07')
    await choose(page, '批量签到时间 · 分钟', '25')
    await dialog.getByRole('button', { name: '应用到所选账号', exact: true }).click()
    await expect(dialog).toHaveCount(0)
    const accounts = await request('GET', '/api/v1/accounts')
    assert.equal(accounts.find(a => a.name === '甲账号').task_settings.schedule.time, '07:25')
    assert.equal(accounts.find(a => a.name === '乙账号').task_settings.schedule, undefined)
    assert(accounts.every(a => a.disabled))
    await page.getByRole('button', { name: '分组与批量', exact: true }).click()
    await page.goBack()
    await expect(page.getByRole('dialog')).toHaveCount(0)
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
    await page.screenshot({ path: testInfo.outputPath('account-groups.png') })
    assert.deepEqual(effects, []); assert.deepEqual(errors, [])
  } finally { await context.close() }
})

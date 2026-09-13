import assert from 'node:assert/strict'
import { expect, test } from './api-test.mjs'
import { choose, dismissGuide } from './ui.mjs'

for (const width of [320, 390, 1440]) test(`日历提醒明确订阅与取消 ${width}px`, async ({ browser }, testInfo) => {
  const origin = 'http://127.0.0.1:5897', headers = { 'Content-Type': 'application/json', 'X-MiyoHub-Request': '1' }
  const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: 'reduce' })
  async function request(method, path, data) {
    const response = await context.request.fetch(origin + path, { method, headers, ...(data ? { data } : {}) })
    const body = await response.json(); assert(response.ok() && body.ok, body.error || path); return body.data
  }
  try {
    await request('POST', '/api/v1/auth/setup', { username: 'reminder-owner', password: 'local-test-only-123' })
    await dismissGuide(context)
    await request('POST', '/api/v1/accounts', { name: '提醒测试账号', cookie: 'stuid=113001;cookie_token=reminder-fixture', disabled: false })
    const [account] = await request('GET', '/api/v1/accounts')
    await request('POST', '/api/v1/calendar/custom', { account_id: account.id, game: 'genshin', kind: 'version', title: '隔离测试版本日程', start_at: new Date(Date.now() + 86400000).toISOString() })
    const effects = [], errors = []
    await context.route('**/*', route => {
      const url = new URL(route.request().url())
      if (url.origin !== origin || /^\/api\/v1\/(run|login\/|accounts\/check|shop\/|game-record\/|push\/test|push\/qr)/.test(url.pathname)) { effects.push(url.pathname); return route.abort() }
      return route.continue()
    })
    const page = await context.newPage(); page.on('pageerror', e => errors.push(e.message))
    await page.goto(origin + '/#calendar')
    await page.getByRole('button', { name: '设置提醒', exact: true }).click()
    const modal = page.getByRole('dialog', { name: '设置日历提醒', exact: true })
    await expect(modal.getByRole('combobox', { name: '提醒节点', exact: true })).toContainText('开始时间')
    await choose(page, '提前提醒时间', 10)
    await expect(modal.getByText(/保存提醒不会替你开启推送/)).toBeVisible()
    await modal.getByRole('button', { name: '保存提醒', exact: true }).click()
    await expect(modal).toHaveCount(0)
    await expect(page.locator('.calendar-reminder')).toHaveCount(1)
    await expect(page.locator('.calendar-reminder .pill')).toHaveText('待提醒')
    const config = await request('GET', '/api/v1/push/config')
    assert.equal(config.enable, false); assert.equal(config.calendar, false)
    const path = '/api/v1/calendar/reminders?account_id=' + account.id + '&game=genshin'
    const state = await request('GET', path)
    assert.equal(state.reminders.length, 1); assert.equal(state.reminders[0].status, 'pending')
    await page.getByRole('button', { name: '取消提醒', exact: true }).click()
    let cancel = page.getByRole('dialog', { name: '取消日历提醒', exact: true })
    await page.goBack(); await expect(cancel).toHaveCount(0)
    await page.getByRole('button', { name: '取消提醒', exact: true }).click()
    cancel = page.getByRole('dialog', { name: '取消日历提醒', exact: true })
    await cancel.getByRole('button', { name: '确认取消提醒', exact: true }).click()
    await expect(cancel).toHaveCount(0)
    await expect(page.locator('.calendar-reminder .pill')).toHaveText('已取消')
    await page.screenshot({ path: testInfo.outputPath('calendar-reminder.png') })
    await page.getByRole('button', { name: '清理已结束的提醒记录', exact: true }).click()
    await page.getByRole('dialog', { name: '清理提醒记录', exact: true }).getByRole('button', { name: '确认清理记录', exact: true }).click()
    await expect(page.locator('.calendar-reminder')).toHaveCount(0)
    await page.getByRole('button', { name: '去消息推送配置', exact: true }).click()
    const checkbox = page.getByRole('checkbox', { name: '日历提醒', exact: true })
    await expect(checkbox).not.toBeChecked(); await checkbox.check()
    await expect.poll(async () => (await request('GET', '/api/v1/push/config')).calendar).toBe(true)
    assert.equal((await request('GET', '/api/v1/push/config')).enable, false)
    assert.deepEqual(await request('GET', '/api/v1/push/history'), [])
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
    assert.deepEqual(effects, []); assert.deepEqual(errors, [])
  } finally { await context.close() }
})

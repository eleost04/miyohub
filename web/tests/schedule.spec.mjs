import assert from 'node:assert/strict'
import { expect, test } from './api-test.mjs'
import { choose, dismissGuide } from './ui.mjs'

test('账号自定义时间使用适配选择器，保存后仍保留独立游戏和自动开关', async ({ browser }, testInfo) => {
  const origin = 'http://127.0.0.1:5897', headers = { 'Content-Type': 'application/json', 'X-MiyoHub-Request': '1' }
  const context = await browser.newContext({ viewport: { width: 390, height: 844 } })
  try {
    const status = await (await context.request.get(origin + '/api/v1/auth/status')).json()
    await context.request.post(origin + (status.data.has_admin ? '/api/v1/auth/login' : '/api/v1/auth/setup'), { headers, data: { username: 'smoke-admin', password: 'local-test-only-123' } })
    await dismissGuide(context)
    await context.request.post(origin + '/api/v1/accounts', { headers, data: { name: '时间测试', cookie: 'stuid=909090;cookie_token=schedule-fixture' } })
    const accounts = (await (await context.request.get(origin + '/api/v1/accounts')).json()).data
    const account = accounts.find(a => a.name === '时间测试')
    await context.request.put(origin + '/api/v1/accounts/tasks', { headers, data: { id: account.id, task_settings: { ...account.task_settings, automatic: false } } })
    const page = await context.newPage()
    await page.goto(origin)
    await page.locator('.account-card').filter({ hasText: '时间测试' }).getByRole('button', { name: '签到设置', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: '时间测试 · 签到设置' })
    await choose(page, '签到时间来源', 'custom')
    await choose(page, '每天签到时间 · 小时', '03')
    await choose(page, '每天签到时间 · 分钟', '17')
    await choose(page, '签到时区', 'UTC')
    await expect.poll(async () => (await (await context.request.get(origin + '/api/v1/accounts')).json()).data.find(a => a.id === account.id).task_settings.schedule).toEqual({ time: '03:17', timezone: 'UTC' })
    const saved = (await (await context.request.get(origin + '/api/v1/accounts')).json()).data.find(a => a.id === account.id).task_settings
    assert.equal(saved.automatic, false)
    assert.deepEqual(saved.games.enabled, account.task_settings.games.enabled)
    await page.screenshot({ path: testInfo.outputPath('personal-schedule-390.png'), fullPage: true })
    await dialog.getByRole('button', { name: '完成', exact: true }).click()
    await expect(dialog).toBeHidden()
    const accountBefore = (await (await context.request.get(origin + '/api/v1/accounts')).json()).data.find(a => a.id === account.id)
    await context.request.put(origin + '/api/v1/accounts/tasks', { headers, data: { id: account.id, task_settings: { ...accountBefore.task_settings, automatic: true } } })
    await page.getByRole('button', { name: '刷新数据', exact: true }).click()
    await expect(page.locator('.schedule-panel').getByRole('heading', { name: '我的自动签到', exact: true })).toBeVisible()
    await expect(page.locator('.schedule-panel .schedule-time')).toHaveText('11:17')
    await expect(page.locator('.schedule-panel .pill')).toContainText('个账号参与')
  } finally { await context.close() }
})

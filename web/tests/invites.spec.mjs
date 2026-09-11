import assert from 'node:assert/strict'
import { expect, test } from './api-test.mjs'
import { choose, dismissGuide } from './ui.mjs'

test('邀请码在手机适配弹窗中生成随机值，预设权限随注册生效', async ({ browser }, testInfo) => {
  const origin = 'http://127.0.0.1:5897', headers = { 'Content-Type': 'application/json', 'X-MiyoHub-Request': '1' }
  const context = await browser.newContext({ viewport: { width: 390, height: 844 } })
  const member = await browser.newContext()
  try {
    const status = await (await context.request.get(origin + '/api/v1/auth/status')).json()
    const login = await context.request.post(origin + (status.data.has_admin ? '/api/v1/auth/login' : '/api/v1/auth/setup'), { headers, data: { username: 'smoke-admin', password: 'local-test-only-123' } })
    assert.equal(login.status(), 200)
    await dismissGuide(context)
    const page = await context.newPage()
    await page.goto(origin + '/#admin')
    await page.getByRole('button', { name: '创建邀请码', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: '创建邀请码', exact: true })
    await expect(dialog.getByRole('checkbox', { name: '邀请码授予兑换权限', exact: true })).toBeChecked()
    await expect(dialog.getByRole('checkbox', { name: '邀请码授予站点打码权限', exact: true })).toBeChecked()
    await expect(dialog.getByLabel('注册后默认选择站点打码')).toBeChecked()
    await expect(page.locator('input[type="datetime-local"]')).toHaveCount(0)
    await choose(page, '邀请码有效期', 'custom')
    await dialog.getByRole('button', { name: '邀请码失效时间', exact: true }).click()
    const calendar = page.getByRole('dialog', { name: '邀请码失效时间', exact: true })
    await page.screenshot({ path: testInfo.outputPath('invite-expiry-390.png') })
    await calendar.getByRole('button', { name: '应用时间' }).click()
    await dialog.getByRole('checkbox', { name: '邀请码授予兑换权限', exact: true }).uncheck()
    await dialog.getByLabel('备注', { exact: true }).fill('邀请集成测试')
    await dialog.getByRole('button', { name: '生成随机邀请码' }).click()
    await expect(dialog).toBeHidden()
    const codes = (await (await context.request.get(origin + '/api/v1/admin/invite-codes')).json()).data
    const invite = codes.find(c => c.note === '邀请集成测试')
    assert.match(invite.code, /^MYH-[A-F0-9]{48}$/)
    assert.equal(invite.max_uses, 1)
    assert.equal(invite.permissions.exchange, false)
    assert.equal(invite.permissions.site_captcha, true)
    const register = await member.request.post(origin + '/api/v1/auth/register', { headers, data: { username: 'invited-browser', password: 'local-test-only-123', invite_code: invite.code, role: 'admin', permissions: { exchange: true } } })
    assert.equal(register.status(), 200)
    const user = (await register.json()).data.user
    assert.equal(user.role, 'user')
    assert.equal(user.permissions.exchange, false)
    const captcha = (await (await member.request.get(origin + '/api/v1/captcha/config')).json()).data
    assert.equal(captcha.source, 'site')
    assert.equal(captcha.site_allowed, true)
    await page.screenshot({ path: testInfo.outputPath('invites-390.png'), fullPage: true })
  } finally { await context.close(); await member.close() }
})

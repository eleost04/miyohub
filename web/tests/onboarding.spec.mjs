import assert from 'node:assert/strict'
import { expect, test } from './api-test.mjs'
import { choose } from './ui.mjs'

for (const width of [390, 1440]) test(`新用户引导 ${width}px：可跳过、关闭后记住、可重新打开且不启动外部任务`, async ({ browser }, testInfo) => {
  const origin = 'http://127.0.0.1:5897', headers = { 'Content-Type': 'application/json', 'X-MiyoHub-Request': '1' }
  const admin = await browser.newContext(), context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: 'reduce' })
  const errors = [], effects = []
  try {
    const status = (await (await admin.request.get(origin + '/api/v1/auth/status')).json()).data
    await admin.request.post(origin + (status.has_admin ? '/api/v1/auth/login' : '/api/v1/auth/setup'), { headers, data: { username: 'smoke-admin', password: 'local-test-only-123' } })
    const name = 'guide-user-' + width
    const created = await admin.request.post(origin + '/api/v1/admin/users', { headers, data: { username: name, password: 'local-test-only-123', role: 'user' } })
    assert.equal(created.status(), 201)
    await context.request.post(origin + '/api/v1/auth/login', { headers, data: { username: name, password: 'local-test-only-123' } })
    await context.request.post(origin + '/api/v1/accounts', { headers, data: { name: '引导测试账号', cookie: 'stuid=' + (300000 + width) + ';cookie_token=guide-fixture', disabled: true } })
    await context.route('**/*', route => {
      const request = route.request(), url = new URL(request.url())
      if (url.origin !== origin || /\/api\/v1\/(run|login\/|accounts\/check|shop\/)/.test(url.pathname) || /\/api\/v1\/(captcha\/test|push\/test|push\/qr)/.test(url.pathname) && request.method() !== 'GET') { effects.push(url.pathname); return route.abort() }
      return route.continue()
    })
    const page = await context.newPage()
    page.on('pageerror', e => errors.push(e.message))
    await page.goto(origin)
    let guide = page.getByRole('dialog', { name: '快速配置', exact: true })
    await expect(guide).toBeVisible()
    await page.screenshot({ path: testInfo.outputPath('onboarding-start.png') })
    await guide.getByRole('button', { name: '短信验证码绑定', exact: false }).click()
    const bind = page.getByRole('dialog', { name: '绑定米游社账号', exact: true })
    await expect(bind.getByPlaceholder('输入绑定米游社的手机号')).toBeVisible()
    await page.goBack()
    await expect(bind).toBeHidden()
    await expect(guide).toBeVisible()
    await guide.getByRole('button', { name: '下一步', exact: true }).click()
    await guide.getByRole('button', { name: '配置签到 引导测试账号', exact: true }).click()
    const tasks = page.getByRole('dialog', { name: '引导测试账号 · 签到设置', exact: true })
    await tasks.getByLabel('参加每日自动签到', { exact: true }).uncheck()
    await choose(page, '签到时间来源', 'custom')
    await choose(page, '每天签到时间 · 小时', '03')
    await choose(page, '每天签到时间 · 分钟', '25')
    await expect(tasks.getByText('已保存', { exact: true })).toBeVisible()
    await tasks.getByRole('button', { name: '完成', exact: true }).click()
    await guide.getByRole('button', { name: '关闭弹窗', exact: true }).click()
    await expect(guide).toBeHidden()
    await expect.poll(async () => (await (await context.request.get(origin + '/api/v1/auth/me')).json()).data.onboarding_status).toBe('dismissed')
    await page.reload()
    await expect(page.locator('.metrics')).toBeVisible()
    await expect(guide).toHaveCount(0)
    await page.getByRole('button', { name: '个人账号', exact: true }).first().click()
    await page.getByRole('button', { name: '重新打开配置引导', exact: true }).click()
    guide = page.getByRole('dialog', { name: '快速配置', exact: true })
    await guide.getByRole('button', { name: '下一步', exact: true }).click()
    await guide.getByRole('button', { name: '下一步', exact: true }).click()
    await expect(guide.getByRole('radio', { name: '暂不使用打码', exact: true })).toBeChecked()
    await expect(guide.getByRole('radio', { name: '使用站点打码服务', exact: true })).toBeDisabled()
    await guide.getByRole('button', { name: '下一步', exact: true }).click()
    await expect(guide.getByLabel('启用自动推送', { exact: true })).not.toBeChecked()
    await page.screenshot({ path: testInfo.outputPath('onboarding-push.png') })
    await guide.getByRole('button', { name: '完成引导', exact: true }).click()
    await expect(guide).toBeHidden()
    await expect.poll(async () => (await (await context.request.get(origin + '/api/v1/auth/me')).json()).data.onboarding_status).toBe('complete')
    assert.deepEqual(effects, [])
    assert.deepEqual(errors, [])
  } finally { await context.close(); await admin.close() }
})

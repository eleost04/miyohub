import assert from 'node:assert/strict'
import { expect, test } from './api-test.mjs'
import { addCaptcha, dismissGuide } from './ui.mjs'

test('偏好自动保存：慢请求不覆盖新输入，密钥脱敏，失败不循环重试', async ({ browser }) => {
  const origin = 'http://127.0.0.1:5897', headers = { 'Content-Type': 'application/json', 'X-MiyoHub-Request': '1' }
  const admin = await browser.newContext(), context = await browser.newContext({ viewport: { width: 390, height: 844 } })
  const external = [], errors = []
  async function request(owner, method, path, data) {
    const response = await owner.request.fetch(origin + path, { method, headers, ...(data ? { data } : {}) })
    const result = await response.json()
    assert(response.ok() && result.ok, result.error || path)
    return result.data
  }
  try {
    const status = await request(admin, 'GET', '/api/v1/auth/status')
    await request(admin, 'POST', status.has_admin ? '/api/v1/auth/login' : '/api/v1/auth/setup', { username: 'smoke-admin', password: 'local-test-only-123' })
    await request(admin, 'POST', '/api/v1/admin/users', { username: 'autosave-user', password: 'local-test-only-123', role: 'user' })
    await request(context, 'POST', '/api/v1/auth/login', { username: 'autosave-user', password: 'local-test-only-123' })
    await dismissGuide(context)
    await request(context, 'POST', '/api/v1/accounts', { name: '自动保存测试号', cookie: 'stuid=202001;cookie_token=isolated-autosave' })
    const push = await request(context, 'GET', '/api/v1/push/config')
    await request(context, 'PUT', '/api/v1/push/config', { ...push, channels: [{ provider: 'webhook', name: '测试通知', enable: true, webhook: 'https://notify.example/hook' }] })
    await context.addInitScript(() => localStorage.setItem('miyohub:auto-save', 'true'))
    await context.route('**/*', async route => {
      const url = new URL(route.request().url())
      if (url.origin !== origin || /^\/api\/v1\/(run|login\/|accounts\/check|push\/(test|qr)|shop\/)/.test(url.pathname)) { external.push(url.pathname); return route.abort() }
      return route.continue()
    })
    const page = await context.newPage()
    page.on('pageerror', error => errors.push(error.message))
    let release, calls = 0
    const gate = new Promise(resolve => { release = resolve })
    await page.route('**/api/v1/accounts/tasks', async route => { calls++; if (calls === 1) await gate; await route.continue() })
    await page.goto(origin)
    await page.getByRole('button', { name: '签到设置', exact: true }).click()
    await page.getByLabel('参加每日自动签到', { exact: true }).uncheck()
    await expect.poll(() => calls).toBe(1)
    await page.getByLabel('星穹铁道签到', { exact: true }).uncheck()
    release()
    await expect.poll(() => calls).toBe(2)
    await expect.poll(async () => (await request(context, 'GET', '/api/v1/accounts'))[0].task_settings.games.enabled.includes('starrail')).toBe(false)
    assert.equal((await request(context, 'GET', '/api/v1/accounts'))[0].task_settings.automatic, false)
    await expect(page.getByRole('dialog').getByText('已保存', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: '快速保存签到设置', exact: true })).toBeHidden()
    await page.getByRole('dialog').getByRole('button', { name: '完成', exact: true }).click()
    await page.getByRole('dialog').waitFor({ state: 'hidden' })
    assert.equal(calls, 2, 'closing an autosaved editor submitted it again')
    await page.getByRole('navigation', { name: '移动导航' }).getByRole('button', { name: '更多', exact: true }).click()
    await page.getByRole('navigation', { name: '更多导航' }).getByRole('button', { name: '打码服务', exact: true }).click()
    await page.getByRole('radio', { name: '使用自己的打码服务', exact: true }).check()
    await addCaptcha(page, page.locator('.personal-captcha-panel'), 'custom', async editor => {
      await editor.getByLabel('接口地址', { exact: true }).fill('https://solver.example/pass_nine')
      await editor.getByLabel(/Bearer Token/).fill('AUTOSAVE_TEST_SECRET')
      await editor.getByLabel('启用这个渠道', { exact: true }).check()
    })
    await expect.poll(async () => (await request(context, 'GET', '/api/v1/captcha/config')).channels.length).toBe(1)
    const saved = await request(context, 'GET', '/api/v1/captcha/config')
    assert.equal(saved.channels[0].token, '')
    assert(saved.channels[0].configured.includes('token'))
    let probes = 0
    await page.route('**/api/v1/captcha/test', route => { if (route.request().method() === 'POST') probes++; return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ ok: true, data: { probe: { id: 'mock-probe', status: 'succeeded', started_at: new Date().toISOString(), retry_at: new Date(Date.now() + 60000).toISOString(), message: '模拟接口已返回校验参数，未执行账号任务。', duration_ms: 100 } } }) }) })
    await page.getByRole('button', { name: '测试自定义打码服务', exact: true }).click()
    assert.equal(probes, 0, 'probe started before confirmation')
    await page.getByRole('dialog').getByRole('button', { name: '开始测试', exact: true }).click()
    await expect(page.getByText(/模拟接口已返回校验参数/)).toBeVisible()
    assert.equal(probes, 1)
    await page.getByRole('navigation', { name: '移动导航' }).getByRole('button', { name: '消息推送', exact: true }).click()
    let pushCalls = 0
    await page.route('**/api/v1/push/config', async route => {
      if (route.request().method() !== 'PUT') return route.continue()
      pushCalls++
      if (pushCalls === 1) return route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ ok: false, error: '模拟保存失败' }) })
      return route.continue()
    })
    await page.getByLabel('启用自动推送', { exact: true }).check()
    await expect(page.getByText('模拟保存失败', { exact: true })).toBeVisible()
    await page.waitForTimeout(2200)
    assert.equal(pushCalls, 1)
    await page.getByRole('button', { name: '快速保存推送配置', exact: true }).click()
    await expect.poll(async () => (await request(context, 'GET', '/api/v1/push/config')).enable).toBe(true)
    assert.equal(pushCalls, 2)
    assert.deepEqual(external, [])
    assert.deepEqual(errors, [])
  } finally { await context.close(); await admin.close() }
})

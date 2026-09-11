import assert from 'node:assert/strict'
import { expect, test } from './api-test.mjs'
import { dismissGuide } from './ui.mjs'

test('短信人机验证在隔离页面手动完成，未发送不显示成功且不加载首屏第三方脚本', async ({ browser }, testInfo) => {
  const origin = 'http://127.0.0.1:5897', headers = { 'Content-Type': 'application/json', 'X-MiyoHub-Request': '1' }
  const context = await browser.newContext({ viewport: { width: 320, height: 780 } })
  const errors = [], unexpected = []
  let scripts = 0, sends = 0, answers = 0, verifies = 0
  try {
    const status = await (await context.request.get(origin + '/api/v1/auth/status')).json()
    const auth = await context.request.post(origin + (status.data.has_admin ? '/api/v1/auth/login' : '/api/v1/auth/setup'), { headers, data: { username: 'smoke-admin', password: 'local-test-only-123' } })
    assert(auth.ok())
    await dismissGuide(context)
    const state = { status: 'captcha_required', message: '请先完成人机验证，短信尚未发送。', phone: '138****8000', retry_at: new Date(Date.now() + 60000).toISOString(), expires_at: new Date(Date.now() + 600000).toISOString(), challenge: { id: 'manual-id', gt: 'public-gt', challenge: 'public-challenge', new_captcha: true, operation: 'send', expires_at: new Date(Date.now() + 120000).toISOString() } }
    await context.route('**/*', async route => {
      const url = new URL(route.request().url())
      if (url.href === 'https://static.geetest.com/static/tools/gt.js') {
        scripts++
        return route.fulfill({ contentType: 'application/javascript', body: `window.initGeetest = (options, callback) => { let success; const widget = { appendTo(selector) { const button = document.createElement('button'); button.textContent = '完成模拟验证'; button.onclick = () => success(); document.querySelector(selector).append(button) }, onReady(fn) { fn() }, onError() {}, onSuccess(fn) { success = fn }, getValidate() { return { geetest_challenge: options.challenge, geetest_validate: 'manual-validation' } } }; callback(widget) }` })
      }
      if (url.origin !== origin) { unexpected.push(url.origin); return route.abort() }
      const ok = data => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ ok: true, data }) })
      if (url.pathname === '/api/v1/login/sms/send') { sends++; assert.equal(route.request().postDataJSON().captcha_mode, 'manual'); return ok(state) }
      if (url.pathname === '/api/v1/login/sms/captcha') { answers++; assert.deepEqual(route.request().postDataJSON(), { id: 'manual-id', challenge: 'public-challenge', validate: 'manual-validation' }); return ok({ ...state, status: 'sent', challenge: undefined, message: '短信验证码已发送至 138****8000' }) }
      if (url.pathname === '/api/v1/login/sms/verify') { verifies++; return ok({ status: 'verified' }) }
      if (url.pathname === '/api/v1/login/sms/cancel') return ok({})
      if (/^\/api\/v1\/(run|shop\/|push\/test|captcha\/test)/.test(url.pathname)) { unexpected.push(url.pathname); return route.abort() }
      return route.continue()
    })
    const page = await context.newPage()
    page.on('pageerror', error => errors.push(error.message))
    await page.goto(origin)
    await page.getByRole('button', { name: '绑定账号', exact: true }).click()
    assert.equal(scripts, 0)
    const login = page.getByRole('dialog', { name: '绑定米游社账号', exact: true })
    await login.getByLabel('账号名称', { exact: true }).fill('手动验证测试')
    await login.getByRole('button', { name: '短信登录', exact: true }).click()
    await login.getByPlaceholder('输入绑定米游社的手机号').fill('13800138000')
    await login.getByRole('button', { name: '获取验证码', exact: true }).click()
    const human = page.getByRole('dialog', { name: '米游社安全验证', exact: true })
    await expect(human).toBeVisible()
    await expect(login.getByPlaceholder('输入验证码')).toBeDisabled()
    await expect(login.getByText(/短信验证码已发送/)).toHaveCount(0)
    await page.evaluate(() => window.postMessage({ type: 'miyohub:captcha:solution', id: 'manual-id', challenge: 'public-challenge', validate: 'fake' }, '*'))
    assert.equal(answers, 0)
    await page.frameLocator('iframe[title="米游社人机验证"]').getByRole('button', { name: '完成模拟验证' }).click()
    await expect(human).toBeHidden()
    await expect(login.getByText('短信验证码已发送至 138****8000', { exact: true })).toBeVisible()
    await login.getByPlaceholder('输入验证码').fill('123456')
    await page.screenshot({ path: testInfo.outputPath('sms-manual-320.png'), fullPage: true })
    await login.getByRole('button', { name: '验证并绑定账号', exact: true }).click()
    await expect(login).toBeHidden()
    assert.deepEqual([scripts, sends, answers, verifies], [1, 1, 1, 1])
    assert.deepEqual(unexpected, [])
    assert.deepEqual(errors, [])
  } finally { await context.close() }
})

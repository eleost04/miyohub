import assert from 'node:assert/strict'
import { expect, test } from './api-test.mjs'
import { dismissGuide } from './ui.mjs'

for (const width of [320, 1440]) test(`站点查询重试可配置、无额外任务请求 ${width}px`, async ({ browser }) => {
  const origin = 'http://127.0.0.1:5897', headers = { 'Content-Type': 'application/json', 'X-MiyoHub-Request': '1' }
  const context = await browser.newContext({ viewport: { width, height: 900 } })
  await context.addInitScript(() => localStorage.setItem('miyohub:auto-save', 'false'))
  const blocked = []
  try {
    const status = await (await context.request.get(origin + '/api/v1/auth/status')).json()
    const login = await context.request.post(origin + (status.data.has_admin ? '/api/v1/auth/login' : '/api/v1/auth/setup'), { headers, data: { username: 'smoke-admin', password: 'local-test-only-123' } })
    assert(login.ok())
    await dismissGuide(context)
    await context.route('**/*', route => {
      const url = new URL(route.request().url())
      if (url.origin !== origin || /^\/api\/v1\/(run|shop\/(?!status|plans$)|push\/(test|qr)|login\/)/.test(url.pathname)) { blocked.push(url.pathname); return route.abort() }
      return route.continue()
    })
    const page = await context.newPage()
    await page.goto(origin + '/#settings')
    const retries = page.getByLabel('米游币状态查询重试次数', { exact: true })
    await expect(retries).toBeVisible()
    await retries.fill('7')
    const saves = page.getByRole('button', { name: '保存设置', exact: true })
    await saves.last().click()
    await expect(page.getByText('设置已保存并生效', { exact: true })).toBeVisible()
    const config = await (await context.request.get(origin + '/api/v1/config')).json()
    assert.equal(config.data.network.bbs_state_retries, 7)
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
    await retries.fill('11')
    assert.equal(await retries.evaluate(el => el.checkValidity()), false)
    assert.deepEqual(blocked, [])
  } finally { await context.close() }
})

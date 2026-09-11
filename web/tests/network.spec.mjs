import assert from 'node:assert/strict'
import { expect, test } from './api-test.mjs'
import { dismissGuide } from './ui.mjs'

for (const width of [320, 1440]) test(`站点查询重试可配置、无额外任务请求 ${width}px`, async ({ browser }, testInfo) => {
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
    await page.getByRole('button', { name: '配置代理', exact: true }).click()
    let dialog = page.getByRole('dialog', { name: '米游社出站代理', exact: true })
    await dialog.getByLabel('启用米游社代理', { exact: true }).check()
    await dialog.getByLabel('代理地址', { exact: true }).fill('socks5://proxy.example:1080')
    await dialog.getByLabel('代理用户名（可选）', { exact: true }).fill('fixture-user')
    await dialog.getByLabel('代理密码（可选）', { exact: true }).fill('fixture-proxy-password')
    let saved = await (await context.request.get(origin + '/api/v1/config')).json()
    assert.equal(saved.data.network.proxy.enable, false, 'editing must not enable a half-configured proxy')
    await dialog.getByRole('button', { name: '应用代理配置', exact: true }).last().click()
    await expect(dialog).toBeHidden()
    await saves.last().click()
    await expect(page.getByText('设置已保存并生效', { exact: true })).toBeVisible()
    saved = await (await context.request.get(origin + '/api/v1/config')).json()
    assert.equal(saved.data.network.proxy.enable, true)
    assert.equal(saved.data.network.proxy.password, '')
    assert.equal(saved.data.network.proxy.has_password, true)
    assert(!await page.locator('input[type=password]').count(), 'saved credentials remained in the page')
    await page.getByRole('button', { name: '配置代理', exact: true }).click()
    dialog = page.getByRole('dialog', { name: '米游社出站代理', exact: true })
    await expect(dialog.getByLabel(/^代理密码/)).toHaveValue('')
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
    await page.screenshot({ path: testInfo.outputPath(`proxy-dialog-${width}.png`), fullPage: true })
    await dialog.getByRole('button', { name: '取消', exact: true }).click()
    await retries.fill('11')
    assert.equal(await retries.evaluate(el => el.checkValidity()), false)
    assert.deepEqual(blocked, [])
  } finally { await context.close() }
})

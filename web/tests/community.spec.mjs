import assert from 'node:assert/strict'
import { expect, test } from './api-test.mjs'
import { choose, dismissGuide } from './ui.mjs'

for (const width of [320, 1440]) test(`社区执行方式 ${width}px：主动选择、自动保存、账号隔离与返回`, async ({ browser }, testInfo) => {
  const origin = 'http://127.0.0.1:5897', headers = { 'Content-Type': 'application/json', 'X-MiyoHub-Request': '1' }
  const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: 'reduce', ...(width < 640 ? { isMobile: true, hasTouch: true } : {}) })
  const errors = [], effects = []
  async function request(method, path, data) {
    const response = await context.request.fetch(origin + path, { method, headers, ...(data ? { data } : {}) })
    const result = await response.json()
    assert(response.ok() && result.ok, result.error || path)
    return result.data
  }
  try {
    const status = await request('GET', '/api/v1/auth/status')
    await request('POST', status.has_admin ? '/api/v1/auth/login' : '/api/v1/auth/setup', { username: 'smoke-admin', password: 'local-test-only-123' })
    await dismissGuide(context)
    for (const [name, uid] of [['社区测试账号', '302001'], ['保留原设置', '302002']]) await request('POST', '/api/v1/accounts', { name, cookie: 'stuid=' + uid + ';cookie_token=community-fixture', disabled: true })
    const before = await request('GET', '/api/v1/accounts')
    assert(before.every(a => !a.task_settings.bbs.run_all_selected))
    const other = before.find(a => a.name === '保留原设置')
    await context.route('**/*', route => {
      const url = new URL(route.request().url())
      if (url.origin !== origin || /^\/api\/v1\/(run|login\/|accounts\/check|push\/(test|qr)|shop\/)/.test(url.pathname)) { effects.push(url.pathname); return route.abort() }
      return route.continue()
    })
    const page = await context.newPage()
    page.on('pageerror', e => errors.push(e.message))
    await page.goto(origin)
    const open = async () => {
      await page.locator('.account-card').filter({ hasText: '社区测试账号' }).getByRole('button', { name: '签到设置', exact: true }).click()
      const dialog = page.getByRole('dialog', { name: '社区测试账号 · 签到设置', exact: true })
      await dialog.getByRole('button', { name: '米游币', exact: true }).click()
      return dialog
    }
    let dialog = await open()
    await expect(dialog.getByRole('combobox', { name: '米游币执行方式', exact: true })).toContainText('按奖励进度执行')
    await expect(dialog.getByRole('combobox', { name: '米游币执行方式', exact: true })).toBeDisabled()
    await dialog.getByLabel('启用米游币任务', { exact: true }).check()
    await choose(page, '米游币执行方式', 'selected')
    await expect(dialog.getByText(/不保证获得米游币；手动再次运行会重新执行/)).toBeVisible()
    await dialog.getByLabel('点赞', { exact: true }).check()
    await expect.poll(async () => (await request('GET', '/api/v1/accounts')).find(a => a.name === '社区测试账号').task_settings.bbs.run_all_selected).toBe(true)
    await expect(dialog.getByText('已保存', { exact: true })).toBeVisible()
    const after = await request('GET', '/api/v1/accounts'), current = after.find(a => a.name === '社区测试账号')
    assert.equal(current.task_settings.bbs.like, true)
    assert.equal(current.task_settings.bbs.share, false)
    assert.deepEqual(after.find(a => a.id === other.id).task_settings, other.task_settings)
    await page.screenshot({ path: testInfo.outputPath('community-mode.png') })
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
    await page.goBack()
    await expect(dialog).toHaveCount(0)
    await page.reload()
    dialog = await open()
    await expect(dialog.getByRole('combobox', { name: '米游币执行方式', exact: true })).toContainText('按所选项目执行')
    await choose(page, '米游币执行方式', 'missions')
    await expect.poll(async () => (await request('GET', '/api/v1/accounts')).find(a => a.name === '社区测试账号').task_settings.bbs.run_all_selected).toBe(false)
    assert.deepEqual(effects, [])
    assert.deepEqual(errors, [])
  } finally { await context.close() }
})

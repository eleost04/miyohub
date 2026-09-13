import assert from 'node:assert/strict'
import { expect, test } from './api-test.mjs'
import { dismissGuide } from './ui.mjs'

for (const width of [320, 390, 1440]) test(`加密账号迁移 ${width}px`, async ({ browser }) => {
  const origin = 'http://127.0.0.1:5897', headers = { 'Content-Type': 'application/json', 'X-MiyoHub-Request': '1' }
  const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: 'reduce', acceptDownloads: true })
  async function request(method, path, data) {
    const response = await context.request.fetch(origin + path, { method, headers, ...(data ? { data } : {}) })
    const body = await response.json(); assert(response.ok() && body.ok, body.error || path); return body.data
  }
  try {
    await request('POST', '/api/v1/auth/setup', { username: 'transfer-owner', password: 'local-test-only-123' })
    await dismissGuide(context)
    await request('POST', '/api/v1/accounts', { name: '迁移测试账号', group: '日常', cookie: 'stuid=502001;cookie_token=transfer-fixture', disabled: true })
    const [original] = await request('GET', '/api/v1/accounts')
    const effects = [], errors = []
    await context.route('**/*', route => {
      const url = new URL(route.request().url())
      if (url.origin !== origin || /^\/api\/v1\/(run|login\/|accounts\/check|push\/|shop\/)/.test(url.pathname)) { effects.push(url.pathname); return route.abort() }
      return route.continue()
    })
    const page = await context.newPage(); page.on('pageerror', e => errors.push(e.message))
    await page.goto(origin); await page.locator('.avatar').click()
    await page.getByRole('button', { name: '迁移账号配置', exact: true }).click()
    let dialog = page.getByRole('dialog', { name: '加密迁移账号配置', exact: true })
    await dialog.getByLabel('本站登录密码', { exact: true }).fill('local-test-only-123')
    await dialog.getByLabel('迁移密码', { exact: true }).fill('local-transfer-only-456')
    await dialog.getByLabel('确认迁移密码', { exact: true }).fill('local-transfer-only-456')
    const downloadPromise = page.waitForEvent('download')
    await dialog.getByRole('button', { name: '生成加密文件', exact: true }).click()
    const stream = await (await downloadPromise).createReadStream(), chunks = []
    for await (const chunk of stream) chunks.push(chunk)
    const buffer = Buffer.concat(chunks)
    assert(!buffer.includes('transfer-fixture') && !buffer.includes('迁移测试账号'))
    assert.equal(JSON.parse(buffer.toString()).cipher, 'AES-256-GCM')
    await expect(dialog).toHaveCount(0)
    // Only delete this isolated fixture account to simulate a fresh destination.
    await request('DELETE', '/api/v1/accounts?id=' + encodeURIComponent(original.id))
    await page.getByRole('button', { name: '迁移账号配置', exact: true }).click()
    dialog = page.getByRole('dialog', { name: '加密迁移账号配置', exact: true })
    await dialog.getByRole('tab', { name: '导入', exact: true }).click()
    await dialog.getByLabel('选择加密文件', { exact: true }).setInputFiles({ name: 'transfer.miyohub', mimeType: 'application/json', buffer })
    await dialog.getByLabel('迁移密码', { exact: true }).fill('local-transfer-only-456')
    await dialog.getByRole('button', { name: '解析并预览', exact: true }).click()
    await expect(dialog.getByText('可新增 1 个账号，跳过 0 个已绑定或重名账号。', { exact: true })).toBeVisible()
    await expect(dialog.getByRole('button', { name: '确认导入', exact: true })).toBeDisabled()
    await dialog.getByRole('checkbox').check()
    await dialog.getByRole('button', { name: '确认导入', exact: true }).click()
    await expect(dialog).toHaveCount(0)
    const [imported] = await request('GET', '/api/v1/accounts')
    assert(imported.id !== original.id && imported.disabled && !imported.task_settings.automatic)
    assert.equal(imported.group, original.group)
    await page.getByRole('button', { name: '迁移账号配置', exact: true }).click()
    await page.goBack(); await expect(page.getByRole('dialog')).toHaveCount(0)
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
    assert.deepEqual(effects, []); assert.deepEqual(errors, [])
  } finally { await context.close() }
})

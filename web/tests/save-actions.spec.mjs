import assert from 'node:assert/strict'
import { expect, test } from '@playwright/test'

const origin = 'http://127.0.0.1:4177'

for (const width of [320, 390, 1440]) test(`保存入口 ${width}px：按需出现、同屏不重复、弹窗隔离与表单校验`, async ({ browser }, testInfo) => {
  const mobile = width <= 640
  const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: 'reduce', ...(mobile ? { hasTouch: true, isMobile: true } : {}) })
  await context.addInitScript(() => localStorage.setItem('miyohub:auto-save', 'false'))
  const page = await context.newPage(), errors = [], saves = []
  const user = { id: 'save-owner', username: '保存测试', role: 'admin', status: 'active' }
  const config = { enabled: true, accounts: [], features: { game_checkin: true, cloud_game_checkin: false, bbs_tasks: true }, games: { enabled: ['genshin'], black_list: {} }, cloud_games: { enabled: [] }, bbs: { forums: [2], checkin: true, read: true, like: true, share: true, cancel_like: false, post_limit: 5, delay_seconds: [1, 3] }, schedule: { enable: false, time: '09:00', timezone: 'Asia/Shanghai', jitter_minutes: 5, run_on_start: false }, captcha: { max_retries: 2, channels: [] }, push: { channels: [], error_only: false }, shop_exchange: { enable: true, retry_seconds: 10, retry_interval: .5, plans: [] } }
  page.on('pageerror', e => errors.push(e.message))
  await context.route('**/*', async route => {
    const url = new URL(route.request().url()), path = url.pathname
    if (url.origin !== origin) { errors.push('External request blocked'); return route.abort() }
    if (!path.startsWith('/api/')) return route.continue()
    const ok = data => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ ok: true, data }) })
    if (path === '/api/v1/bootstrap') return ok({ auth: { has_admin: true, need_auth: true }, user, config, status: { running: false, logs: [] } })
    if (path === '/api/v1/status') return ok({ running: false, logs: [] })
    if (path === '/api/v1/config' && route.request().method() === 'PUT') {
      const sent = route.request().postDataJSON()
      saves.push(sent)
      Object.assign(config, { ...sent, shop_exchange: { ...config.shop_exchange, ...sent.shop_exchange } })
      return ok(config)
    }
    errors.push('Unexpected API: ' + path)
    return route.fulfill({ status: 500, contentType: 'application/json', body: '{"ok":false,"error":"Unmocked API denied"}' })
  })
  const floating = page.locator('.settings-form > .floating-save')
  const inline = page.locator('.settings-save-bar [data-save-inline]')
  const top = () => page.evaluate(() => window.scrollTo({ top: 0, behavior: 'instant' }))
  try {
    await page.goto(origin + '/#settings')
    await expect(inline).toBeDisabled()
    await expect(floating).toBeHidden()
    await page.getByLabel('开启站点每日调度', { exact: true }).check()
    await top()
    if (mobile) await expect(floating).toBeVisible()
    else await expect(floating).toBeHidden()
    await page.screenshot({ path: testInfo.outputPath('settings-pending.png') })
    await inline.scrollIntoViewIfNeeded()
    await expect(inline).toBeInViewport()
    await expect(floating).toBeHidden()
    await page.screenshot({ path: testInfo.outputPath('settings-inline-action.png') })

    await page.getByRole('button', { name: '添加验证码渠道', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: '添加验证码渠道', exact: true })
    await expect(dialog).toBeVisible()
    await expect(floating).toBeHidden()
    await dialog.getByRole('combobox', { name: '验证码渠道类型', exact: true }).click()
    await expect(page.locator('.floating-save:visible')).toHaveCount(0)
    await page.keyboard.press('Escape')
    await expect(dialog).toBeVisible()
    assert.equal(await page.evaluate(() => document.body.style.overflow), 'hidden')
    await dialog.getByLabel('启用这个渠道', { exact: true }).check()
    if (mobile) await page.setViewportSize({ width, height: 480 })
    await dialog.evaluate(el => { el.scrollTop = 0 })
    const apply = mobile ? dialog.getByRole('button', { name: '快速应用渠道配置', exact: true }) : dialog.locator('[data-save-inline]')
    await expect(apply).toBeVisible()
    await apply.click()
    await expect(dialog).toBeVisible()
    assert.equal(await dialog.getByLabel('接口地址', { exact: true }).evaluate(el => el.validity.valueMissing), true)
    assert.equal(saves.length, 0, 'floating submit bypassed required validation')
    await dialog.getByLabel('接口地址', { exact: true }).fill('https://solver.example/pass_nine')
    await dialog.evaluate(el => { el.scrollTop = 0 })
    await apply.click()
    await expect(dialog).toHaveCount(0)
    assert.equal(saves.length, 0, 'applying a draft unexpectedly persisted it')
    await page.setViewportSize({ width, height: 900 })
    await top()
    if (mobile) { await expect(floating).toBeVisible(); await floating.click() }
    else await inline.click()
    await expect.poll(() => saves.length).toBe(1)
    await expect(floating).toBeHidden()
    await expect(inline).toBeDisabled()
    assert.equal(saves[0].captcha.channels[0].endpoint, 'https://solver.example/pass_nine')

    await page.getByLabel('自动保存配置', { exact: true }).check()
    await page.getByLabel('默认调度随机延迟上限（分钟）', { exact: true }).fill('8')
    await top()
    if (mobile) await expect(floating).toBeVisible()
    await expect.poll(() => saves.length).toBe(2)
    await expect(floating).toBeHidden()
    await expect(page.locator('.autosave-status').getByText('已保存', { exact: true })).toBeVisible()
    await page.screenshot({ path: testInfo.outputPath('settings-autosaved.png') })
    assert.equal(saves[1].schedule.jitter_minutes, 8)
    assert.equal(await page.evaluate(() => document.body.style.overflow), '')
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
    assert.deepEqual(errors, [])
  } finally { await context.close() }
})

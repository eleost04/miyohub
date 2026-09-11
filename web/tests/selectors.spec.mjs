import assert from 'node:assert/strict'
import { expect, test } from '@playwright/test'
import { choose } from './ui.mjs'

const origin = 'http://127.0.0.1:4177'
const physicalName = '【崩坏：星穹铁道】叽米的会客室系列 毛绒挂件-三月小鸟'

async function workspace(browser, width) {
  const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: 'reduce', timezoneId: 'Asia/Shanghai', ...(width <= 640 ? { isMobile: true, hasTouch: true } : {}) })
  const page = await context.newPage(), errors = [], writes = [], catalogs = []
  const user = { id: 'selector-owner', username: '选择器测试', role: 'admin', status: 'active' }
  const accounts = ['a1', 'a2'].map((id, index) => ({ id, user_id: user.id, name: index ? '备用兑换账号' : '主要兑换账号', disabled: false, status: 'valid', has_cookie: true, has_stoken: true }))
  const config = { enabled: true, accounts, features: { game_checkin: true, cloud_game_checkin: false, bbs_tasks: true }, games: { enabled: ['starrail'], black_list: {} }, cloud_games: { enabled: [] }, bbs: { forums: [6], checkin: true, read: true, like: true, share: true, cancel_like: false, post_limit: 5, delay_seconds: [1, 3] }, schedule: { enable: false, time: '09:00', timezone: 'Asia/Shanghai', jitter_minutes: 5, run_on_start: false }, captcha: { max_retries: 2, channels: [] }, push: { channels: [], error_only: false }, shop_exchange: { enable: true, retry_seconds: 10, retry_interval: .5, plans: [] } }
  const goods = [
    { goods_id: 'physical', goods_name: physicalName, type: 1, price: 40000, game_biz: '', requires_address: true, requires_role: false, stock: '10', limit: '每月 0/1', display_status: 'scheduled', exchange_time: '即将开放', exchange_timestamp: Math.floor(Date.now() / 1000) + 10800, icon: '' },
    { goods_id: 'virtual', goods_name: '星穹铁道 · 游戏礼包（模拟商品）', type: 2, price: 1000, game_biz: 'hkrpg_cn', requires_address: false, requires_role: true, stock: '10', limit: '每月 0/1', display_status: 'online', exchange_time: '正在兑换', exchange_timestamp: 0, icon: '' },
  ]
  page.on('pageerror', e => errors.push(e.message))
  await context.route('**/*', async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname
    if (url.origin !== origin) { errors.push('External request blocked'); return route.abort() }
    if (!path.startsWith('/api/')) return route.continue()
    const ok = data => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ ok: true, data }) })
    const status = { running: false, logs: [], accounts, exchange: config.shop_exchange }
    if (path === '/api/v1/bootstrap') return ok({ auth: { has_admin: true, need_auth: true }, user, config, status })
    if (path === '/api/v1/status') return ok(status)
    if (path === '/api/v1/shop/status') return ok({ enabled: true, running: 0, next_run: 0, server_time: new Date().toISOString(), clock: { offset_ms: 0, rtt_ms: 10, error: '' } })
    if (path === '/api/v1/shop/goods') { catalogs.push(url.search); return ok({ games: [{ key: 'all', name: '全部商品' }, { key: 'hkrpg', name: '星穹铁道' }], goods }) }
    if (path === '/api/v1/shop/good-detail') return ok(goods.find(g => g.goods_id === url.searchParams.get('goods_id')))
    if (path === '/api/v1/shop/points') return ok({ points: 50000 })
    if (path === '/api/v1/shop/addresses') return ok([1, 2].map(n => ({ id: url.searchParams.get('account_id') + '-addr-' + n, name: '模拟收件人 ' + n, phone: '138****8000', address: '模拟省 模拟市 测试路 ' + n + ' 号（不寄送任何物品）' })))
    if (path === '/api/v1/shop/roles') return ok([1, 2].map(n => ({ uid: '10000000' + n, region: 'prod_gf_cn', nickname: '模拟开拓者 ' + n, region_name: '星穹列车', level: '70' })))
    if (path === '/api/v1/shop/plans' && request.method() === 'GET') return ok(config.shop_exchange.plans)
    if (path === '/api/v1/shop/plans' && request.method() === 'POST') {
      const body = request.postDataJSON()
      writes.push(body)
      const plan = { ...body, id: 'test-plan', revision: 1 }
      config.shop_exchange.plans = [plan]
      return ok(plan)
    }
    errors.push('Unexpected API: ' + request.method() + ' ' + path)
    return route.fulfill({ status: 500, contentType: 'application/json', body: '{"ok":false,"error":"Unmocked API denied"}' })
  })
  await page.goto(origin + '/#shop')
  await expect(page.locator('.good-card')).toHaveCount(1)
  return { context, page, errors, writes, catalogs }
}

for (const width of [390, 1440]) test(`预约选择器 ${width}px：键盘、窗口调整和返回不丢失选择`, async ({ browser }, testInfo) => {
  const w = await workspace(browser, width), { page } = w
  const list = label => page.getByRole('listbox', { name: label, exact: true })
  const trigger = label => page.getByRole('combobox', { name: label, exact: true })
  async function resizeWithOpenMenu(label, height) {
    await page.setViewportSize({ width, height })
    // Mobile keyboards may resize just the visual viewport, just the layout
    // viewport, or both. None of these events should dismiss a selection.
    await page.evaluate(() => { window.dispatchEvent(new Event('resize')); window.visualViewport?.dispatchEvent(new Event('resize')); window.visualViewport?.dispatchEvent(new Event('scroll')) })
    await expect(list(label)).toBeVisible()
    await expect(trigger(label)).toHaveAttribute('aria-expanded', 'true')
    await expect.poll(() => page.locator('.choice-menu').evaluate(el => {
      const r = el.getBoundingClientRect()
      return r.top >= 0 && r.bottom <= innerHeight + 1 && r.left >= 0 && r.right <= innerWidth + 1
    })).toBe(true)
    assert.equal(await page.evaluate(() => document.body.style.overflow), 'hidden')
  }
  try {
    await page.locator('.good-card').filter({ hasText: physicalName }).getByRole('button', { name: '查看详情' }).click()
    const dialog = page.getByRole('dialog', { name: physicalName, exact: true })
    await dialog.getByRole('button', { name: '预约这件商品', exact: true }).click()
    await expect(trigger('收货地址')).toBeEnabled()
    await trigger('兑换账号').click()
    await expect(list('兑换账号')).toBeVisible()
    if (width <= 640) await expect(page.getByRole('searchbox', { name: '搜索兑换账号', exact: true })).not.toBeFocused()
    else await expect(page.getByRole('searchbox', { name: '搜索兑换账号', exact: true })).toBeFocused()
    await page.getByRole('searchbox', { name: '搜索兑换账号', exact: true }).fill('备用')
    await resizeWithOpenMenu('兑换账号', 530)
    await expect(page.getByRole('searchbox', { name: '搜索兑换账号', exact: true })).toHaveValue('备用')
    await list('兑换账号').getByRole('option', { name: '备用兑换账号', exact: true }).click()
    await expect(trigger('兑换账号')).toHaveText('备用兑换账号')
    await expect(trigger('兑换账号')).toBeFocused()
    await expect(trigger('收货地址')).toBeEnabled()
    await page.setViewportSize({ width, height: 900 })
    await trigger('收货地址').click()
    await page.getByRole('searchbox', { name: '搜索收货地址', exact: true }).fill('测试路 2')
    await resizeWithOpenMenu('收货地址', 530)
    await page.screenshot({ path: testInfo.outputPath('address-selector-keyboard.png') })
    await list('收货地址').getByRole('option', { name: '模拟收件人 2 · 138****8000', exact: true }).click()
    await expect(trigger('收货地址')).toContainText('模拟收件人 2')
    await page.setViewportSize({ width, height: 900 })

    // A backdrop dismissal and browser Back close only the topmost selector;
    // the product editor, scroll lock, and selected values must survive.
    await trigger('收货地址').click()
    await page.locator('.choice-backdrop').click({ position: { x: 2, y: 2 } })
    await expect(list('收货地址')).toHaveCount(0)
    await trigger('兑换账号').click()
    await expect.poll(() => page.evaluate(() => history.state?.miyohub?.layers.length)).toBe(3)
    await page.goBack()
    await expect(list('兑换账号')).toHaveCount(0)
    await expect(dialog.locator('.plan-form')).toBeVisible()
    await expect(trigger('收货地址')).toContainText('模拟收件人 2')
    assert.equal(await page.evaluate(() => document.body.style.overflow), 'hidden')
    await dialog.getByRole('button', { name: '兑换时间', exact: true }).click()
    let calendar = page.getByRole('dialog', { name: '兑换时间', exact: true })
    await expect(calendar).toBeVisible()
    await expect(page.locator('input[type="datetime-local"]')).toHaveCount(0)
    await page.goBack()
    await expect(calendar).toHaveCount(0)
    await expect(dialog).toBeVisible()
    await dialog.getByRole('button', { name: '兑换时间', exact: true }).click()
    calendar = page.getByRole('dialog', { name: '兑换时间', exact: true })
    const date = new Date(Date.now() + 2 * 86400000), pad = n => String(n).padStart(2, '0')
    await choose(page, '年份', String(date.getFullYear()))
    await choose(page, '月份', pad(date.getMonth() + 1))
    await calendar.getByRole('button', { name: `${date.getFullYear()}-${pad(date.getMonth()+1)}-${pad(date.getDate())}`, exact: true }).click()
    await choose(page, '时间 · 小时', '18')
    await choose(page, '时间 · 分钟', '00')
    await choose(page, '时间 · 秒', '05')
    await calendar.getByRole('button', { name: '应用时间' }).click()
    await expect(trigger('收货地址')).toContainText('模拟收件人 2')
    await dialog.getByRole('button', { name: '保存预约', exact: true }).click()
    await expect(dialog).toHaveCount(0)
    assert.equal(w.writes.length, 1)
    assert.equal(w.writes[0].account_id, 'a2')
    assert.equal(w.writes[0].address_id, 'a2-addr-2')
    assert.equal(w.writes[0].exchange_at, Date.UTC(date.getFullYear(), date.getMonth(), date.getDate(), 10, 0, 5) / 1000)

    await page.getByRole('button', { name: '虚拟商品', exact: false }).click()
    await expect(page.locator('.good-card')).toHaveCount(1)
    assert.equal(w.catalogs.length, 1, 'switching goods type refetched the catalog')
    await trigger('商品分区').click()
    await expect(list('商品分区').getByRole('option', { name: '全部商品', exact: true })).toHaveCount(0)
    await page.keyboard.press('Escape')
    await page.locator('.good-card').filter({ hasText: '游戏礼包' }).getByRole('button', { name: '查看详情' }).click()
    await page.getByRole('button', { name: '选择账号与兑换方式', exact: true }).click()
    await expect(trigger('接收奖励的角色')).toBeEnabled()
    await trigger('接收奖励的角色').click()
    await resizeWithOpenMenu('接收奖励的角色', 530)
    await page.evaluate(() => {
      // iOS can pan/resize the visual viewport without a window resize.
      Object.defineProperty(window.visualViewport, 'height', { configurable: true, value: 310 })
      Object.defineProperty(window.visualViewport, 'offsetTop', { configurable: true, value: 50 })
      window.visualViewport.dispatchEvent(new Event('resize'))
      window.visualViewport.dispatchEvent(new Event('scroll'))
    })
    await expect.poll(() => page.locator('.choice-menu').evaluate(el => {
      const r = el.getBoundingClientRect(), v = window.visualViewport
      return r.top >= v.offsetTop && r.bottom <= v.offsetTop + v.height + 1
    })).toBe(true)
    await list('接收奖励的角色').getByRole('option', { name: '模拟开拓者 2', exact: true }).click()
    await expect(trigger('接收奖励的角色')).toContainText('模拟开拓者 2')
    await page.evaluate(() => { delete window.visualViewport.height; delete window.visualViewport.offsetTop })
    await page.getByRole('button', { name: '关闭弹窗', exact: true }).click()
    await page.setViewportSize({ width, height: 900 })

    await trigger('商品状态').click()
    await resizeWithOpenMenu('商品状态', 640)
    // Changing between bottom-sheet and anchored layouts must also be stable.
    await page.setViewportSize({ width: width <= 640 ? 900 : 390, height: 640 })
    await expect(page.locator('.choice-backdrop')).toHaveClass(width <= 640 ? 'choice-backdrop' : 'choice-backdrop choice-mobile')
    await expect(list('商品状态')).toBeVisible()
    await page.keyboard.press('End')
    await page.keyboard.press('Enter')
    await expect(trigger('商品状态')).toHaveText('即将开放 / 补货')
    await expect(list('商品状态')).toHaveCount(0)
    assert.equal(await page.evaluate(() => document.body.style.overflow), '')
    assert.equal(w.writes.length, 1, 'selection issued an unexpected write')
    assert.deepEqual(w.errors, [])
  } finally { await w.context.close() }
})

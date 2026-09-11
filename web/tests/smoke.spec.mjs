import assert from 'node:assert/strict'
import { test, devices } from '@playwright/test'
import { mkdir } from 'node:fs/promises'
import { choose } from './ui.mjs'

const base = 'http://127.0.0.1:4177'
test('签到、登录、兑换、任务筛选与分离设置的桌面和移动流程', async ({ browser }, testInfo) => {
const output = testInfo.outputDir
await mkdir(output, { recursive: true })
const reports = []
const user = { id: 'user-test', username: '旅行者', role: 'admin', status: 'active' }
const makeAccount = (id, name) => ({ id, name, user_id: user.id, stuid: id === 'a1' ? '100001' : '100002', disabled: false, status: 'valid', checked_at: new Date().toISOString(), has_cookie: true, has_stoken: true, cloud_configured: ['genshin'], task_results: { games: { success: 2, failed: 0, skipped: 1, details: ['原神 · 天空岛：签到成功，获得原石 × 20', '星穹铁道 · 星穹列车：今日已签到'] }, bbs: { success: 3, failed: 0, skipped: 1, details: ['米游币本次新增 60，余额 2,680'] } }, last_task_at: new Date().toISOString() })
const makePlan = (overrides = {}) => ({ id: 'p1', revision: 1, state: 'pending', phase: '', attempt: 0, price: 1500, goods_type: 2, enable: true, auto: true, account_id: 'a1', goods_id: 'gift-1', goods_name: '原神 · 旅途补给礼包', device_fp: '', uid: '123456789', region: 'cn_gf01', game_biz: 'hk4e_cn', address_id: '', exchange_at: Math.floor(Date.now() / 1000) + 3600, last_result: '', last_run: '', ...overrides })
const goods = [
  { goods_id: 'gift-1', goods_name: '原神 · 旅途补给礼包', type: 2, price: 1500, game_biz: 'hk4e_cn', requires_role: true, requires_address: false, stock: '128', limit: '每月 0/1', display_status: 'online', exchange_time: '正在兑换', exchange_timestamp: 0, icon: '/api/v1/shop/image?src=' + encodeURIComponent('https://bbs-static.miyoushe.com/static/test-only.png') },
  { goods_id: 'gift-2', goods_name: '派蒙的星光纪念徽章', type: 1, price: 2000, game_biz: '', requires_role: false, requires_address: true, stock: '80', limit: '每月 0/1', display_status: 'scheduled', exchange_time: '即将开放', exchange_timestamp: Math.floor(Date.now() / 1000) + 7200, icon: '' },
  { goods_id: 'gift-3', goods_name: '星穹铁道 · 开拓者补给', type: 2, price: 5000, game_biz: 'hkrpg_cn', requires_role: true, requires_address: false, stock: '不限量', limit: '每日 0/1', display_status: 'always', exchange_time: '随时兑换', exchange_timestamp: 0, icon: '' },
  { goods_id: 'gift-4', goods_name: '绝区零 · 绳网限定礼盒', type: 2, price: 1000, game_biz: '', requires_role: false, requires_address: false, stock: '0', limit: '每月 0/1', display_status: 'ended', exchange_time: '兑换已结束', exchange_timestamp: 0, icon: '' },
]

async function setup(options = {}, empty = false, role = 'admin') {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1080 }, timezoneId: 'Asia/Shanghai', reducedMotion: 'reduce', ...options })
  const page = await context.newPage()
  page.setDefaultTimeout(8000)
  const errors = [], calls = [], screenshots = [], assets = []
  page.on('request', request => { if (request.resourceType() === 'script') assets.push(new URL(request.url()).pathname) })
  const currentUser = { ...user, role }
  let tasks = [], plans = empty ? [] : [makePlan(), makePlan({ id: 'p-old', state: 'failed', auto: false, goods_id: 'gift-1', goods_name: '上次的旅途补给', last_result: '已售罄，未扣除米游币', attempt: 1 })]
  let accounts = empty ? [] : [makeAccount('a1', '主账号 · 提瓦特日记'), makeAccount('a2', '小号 · 星海同行')]
  if (accounts[1]) accounts[1].task_results = { bbs: { success: 0, failed: 0, skipped: 1, details: ['今日已得 50，还可获得 0', '今日米游币任务已完成'] } }
  let qr = { running: false, status: '', qr_url: '', error: '', account: '' }
  let idCounter = 10
  let logs = empty ? [] : [
    { at: new Date(Date.now() - 30000).toISOString(), component: 'games', message: '主账号 · 提瓦特日记：原神签到成功，获得原石 × 20' },
    { at: new Date(Date.now() - 20000).toISOString(), component: 'bbs', message: '主账号 · 提瓦特日记：米游币任务完成，今日新增 60' },
    { at: new Date(Date.now() - 10000).toISOString(), component: 'exchange', message: '原神 · 旅途补给礼包：预约已保存，将在开放前准备' },
  ]
  const cfg = { enabled: true, features: { game_checkin: true, cloud_game_checkin: true, bbs_tasks: true }, games: { enabled: ['genshin', 'starrail', 'zzz', 'honkai3rd', 'tears', 'honkai2'], black_list: {} }, cloud_games: { enabled: ['genshin', 'zzz'] }, bbs: { forums: [2, 6, 8, 1, 3, 4, 5], checkin: true, read: true, like: true, share: true, cancel_like: false, post_limit: 10, delay_seconds: [0, 0] }, schedule: { enable: true, time: '08:30', timezone: 'Asia/Shanghai', jitter_minutes: 5, run_on_start: false }, captcha: { max_retries: 2, channels: [] }, push: { error_only: false, channels: [] } }
  for (const a of accounts) a.task_settings = structuredClone({ revision: 1, automatic: true, features: cfg.features, games: cfg.games, cloud_games: cfg.cloud_games, bbs: cfg.bbs })
  const config = () => ({ ...cfg, accounts, shop_exchange: { enable: true, retry_seconds: 10, retry_interval: 0.5, plans } })
  const exchangeStatus = () => ({ enabled: true, running: plans.filter(p => p.state === 'running').length, next_run: plans.filter(p => p.auto && ['pending', 'running'].includes(p.state)).map(p => p.exchange_at).sort()[0] || 0, server_time: new Date().toISOString(), clock: { offset_ms: 12, rtt_ms: 38, synced_at: new Date().toISOString(), error: '' } })
  const status = () => ({ running: tasks.length > 0, logs, accounts, tasks, exchange: config().shop_exchange, exchange_scheduler: exchangeStatus(), scheduler: { enabled: true, running: tasks.length > 0, next_run: new Date(Date.now() + 8 * 3600000).toISOString(), last_run: new Date(Date.now() - 3600000).toISOString(), last_error: '', schedule: cfg.schedule } })
  page.on('pageerror', e => errors.push(e.message))
  page.on('console', msg => { if (msg.type() === 'error') errors.push(msg.text()) })
  await context.route('**/*', async route => {
    if (new URL(route.request().url()).origin !== base) { errors.push('Unexpected external request: ' + route.request().url()); await route.abort(); return }
    await route.continue()
  })
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname, method = request.method()
    const body = request.postData() ? JSON.parse(request.postData()) : {}
    calls.push({ path, method, body })
    const ok = data => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true, data }) })
    if (path === '/api/v1/bootstrap') { const { accounts: _accounts, exchange: _exchange, ...runtime } = status(); return ok({ auth: { need_auth: true, has_admin: true, registration_mode: 'review' }, user: currentUser, config: config(), status: runtime }) }
    if (path === '/api/v1/auth/status') return ok({ need_auth: true, has_admin: true, registration_mode: 'review' })
    if (path === '/api/v1/auth/me') return ok(currentUser)
    if (path === '/api/v1/config') { if (method === 'PUT') Object.assign(cfg, body); return ok(config()) }
    if (path === '/api/v1/captcha/config') return ok({ source: 'off', revision: 1, max_retries: 3, channels: [], site_allowed: role === 'admin', site_available: true })
    if (path === '/api/v1/captcha/test' && method === 'GET') return ok({ probe: null })
    if (path === '/api/v1/accounts/tasks') { const a = accounts.find(a => a.id === body.id); assert(a); a.task_settings = { ...body.task_settings, revision: a.task_settings.revision + 1 }; return ok(a) }
    if (path === '/api/v1/status') return ok(status())
    if (path === '/api/v1/admin/users') return ok({ users: [user], registration_mode: 'review' })
    if (path === '/api/v1/admin/invite-codes') return ok([])
    if (path === '/api/v1/accounts/check') return ok({})
    if (path === '/api/v1/run') { tasks = (body.account_ids || accounts.map(a => a.id)).map(id => ({ account_id: id, state: 'running', current: '游戏签到', started_at: new Date().toISOString() })); return ok({}) }
    if (path === '/api/v1/run/cancel') { tasks = tasks.filter(t => !(body.account_ids || accounts.map(a => a.id)).includes(t.account_id)); return ok({}) }
    if (path === '/api/v1/login/qr/start' || path === '/api/v1/login/qr/refresh') { qr = { running: true, status: 'waiting', qr_url: 'https://example.invalid/test-only-qr/' + Date.now(), error: '', account: body.account_name, account_id: body.account_id }; return ok(qr) }
    if (path === '/api/v1/login/qr') return ok(qr)
    if (path === '/api/v1/login/qr/cancel') { qr = { ...qr, running: false, status: 'cancelled', qr_url: '' }; return ok(qr) }
    if (path === '/api/v1/login/sms/send') return ok({ status: 'sent', message: '短信验证码已发送至 138****8000', phone: '138****8000', retry_at: new Date(Date.now() + 60000).toISOString(), expires_at: new Date(Date.now() + 600000).toISOString() })
    if (path === '/api/v1/login/sms/verify') { accounts.push(makeAccount('sms-test', '短信测试账号')); return ok({ status: 'verified' }) }
    if (path === '/api/v1/login/sms/cancel') return ok({})
    if (path === '/api/v1/shop/status') return ok(exchangeStatus())
    if (path === '/api/v1/shop/image') return route.fulfill({ status: 200, contentType: 'image/png', body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aWZkAAAAASUVORK5CYII=', 'base64') })
    if (path === '/api/v1/shop/goods') return ok({ games: [{ key: 'hk4e', name: '原神' }, { key: 'hkrpg', name: '星穹铁道' }], goods: goods.filter(g => !url.searchParams.get('game') || g.game_biz.startsWith(url.searchParams.get('game'))) })
    if (path === '/api/v1/shop/good-detail') return ok(goods.find(g => g.goods_id === url.searchParams.get('goods_id')))
    if (path === '/api/v1/shop/points') return ok({ points: url.searchParams.get('account_id') === 'a2' ? 100 : 2680 })
    if (path === '/api/v1/shop/roles') return ok([{ uid: '123456789', region: 'cn_gf01', nickname: '旅行者', region_name: '天空岛', level: '60' }])
    if (path === '/api/v1/shop/addresses') return ok([{ id: 'addr-test', name: '测试收件人', phone: '138****8000', address: '测试省 测试市 测试路 1 号（模拟地址）' }])
    if (path === '/api/v1/shop/plans') {
      if (method === 'POST' || method === 'PUT') { const p = { ...body, id: body.id || 'p' + idCounter++, state: 'pending', revision: (body.revision || 0) + 1, attempt: 0 }; plans = [...plans.filter(old => old.id !== p.id), p]; return ok(p) }
      if (method === 'DELETE') plans = plans.filter(p => p.id !== url.searchParams.get('id'))
      return ok(plans)
    }
    if (path === '/api/v1/shop/plans/run') { const p = plans.find(p => p.id === body.id); Object.assign(p, { state: 'running', phase: 'preparing', last_result: '正在准备兑换资料' }); return ok(p) }
    if (path === '/api/v1/shop/plans/cancel') { const p = plans.find(p => p.id === body.id); Object.assign(p, { state: 'cancelled', phase: '', last_result: '兑换已取消，未发送兑换请求' }); return ok({}) }
    errors.push('Unhandled API: ' + method + ' ' + path)
    return route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ ok: false, error: 'Unmocked API denied' }) })
  })
  await page.goto(base)
  await page.locator('.metrics').waitFor()
  const screenshot = async name => {
    const file = output + '/' + name + '.png'
    const modal = await page.getByRole('dialog').count() > 0
    if (!modal) await page.evaluate(() => window.scrollTo({ top: 0, behavior: 'instant' }))
    await page.screenshot({ path: file, fullPage: !modal })
    screenshots.push(file)
  }
  const noOverflow = async name => {
    const dims = await page.evaluate(() => ({ width: window.innerWidth, scroll: document.documentElement.scrollWidth, overflow: [...document.querySelectorAll('body *')].filter(el => { const r = el.getBoundingClientRect(); return r.width && (r.right > innerWidth + 1 || r.left < -1) && getComputedStyle(el).position !== 'fixed' }).slice(0, 5).map(el => el.className) }))
    assert(dims.scroll <= dims.width + 1, name + ' horizontal overflow: ' + JSON.stringify(dims))
  }
  return { context, page, calls, errors, assets, screenshot, noOverflow, reports: screenshots, setPlans: next => { plans = next }, finish: async name => { assert.deepEqual(errors, [], name + ' browser errors'); assert.equal(calls.filter(c => c.path.startsWith('/api/v1/push/')).length, 0, 'placeholder must not call push APIs'); reports.push({ name, screenshots, calls: calls.length }); await context.close() } }
}

async function separatedSettings(flow, prefix) {
  await flow.page.locator('.settings-form').waitFor()
  assert.equal(await flow.page.locator('.push-panel').count(), 0)
  assert.equal(await flow.page.locator('.admin-panel').count(), 0)
  assert.equal(await flow.page.getByLabel('原密码', { exact: true }).count(), 0)
  await flow.noOverflow(prefix + ' separated settings')
}
async function mobileSettings(flow) {
  await flow.page.getByRole('navigation', { name: '移动导航' }).getByRole('button', { name: '更多', exact: true }).click()
  const more = flow.page.getByRole('navigation', { name: '更多导航' })
  for (const label of ['任务总览', '商品兑换', '消息推送']) assert.equal(await more.getByRole('button', { name: label, exact: true }).count(), 0)
  assert.equal(await more.getByText('日常工作台', { exact: true }).count(), 0)
  await flow.page.getByRole('navigation', { name: '更多导航' }).getByRole('button', { name: '系统设置' }).click()
}

try {
  const desk = await setup()
  const { page } = desk
  assert(await page.locator('.hero-card').getByRole('heading', { name: '执行签到任务', exact: true }).isVisible())
  assert.equal(await page.locator('.hero-copy .eyebrow').count(), 0, 'promotional hero caption remained')
  assert(!/SMALL TASKS|把时间留给喜欢|让每一天，都从容/.test(await page.locator('body').innerText()))
  assert.equal(desk.calls.filter(call => call.path === '/api/v1/bootstrap').length, 1)
  assert(!desk.calls.some(call => ['/api/v1/auth/status', '/api/v1/auth/me', '/api/v1/config'].includes(call.path)), 'initial load retained its request waterfall')
  assert(!desk.assets.some(path => /\/(ShopPanel|PushPanel|CaptchaPanel|SettingsPanel|AdminPanel|ProfilePanel|browser)-/.test(path)), 'initial dashboard loaded an unused page or QR renderer')
  await desk.noOverflow('desktop dashboard')
  await desk.screenshot('desktop-dashboard')
  const primary = page.locator('.account-card').filter({ hasText: '主账号 · 提瓦特日记' })
  const completedBBS = page.locator('.account-card').filter({ hasText: '小号 · 星海同行' })
  assert(await completedBBS.getByText('米游币：今日无待领取奖励，本次仅检查状态', { exact: true }).isVisible())
  assert(await completedBBS.getByText('已完成状态检查', { exact: true }).isVisible())
  await primary.locator('summary').click()
  assert(await primary.getByText('原神 · 天空岛：签到成功，获得原石 × 20', { exact: true }).isVisible())
  await primary.getByRole('button', { name: '签到', exact: true }).click()
  await primary.getByRole('button', { name: '停止', exact: true }).waitFor()
  assert.deepEqual(desk.calls.findLast(c => c.path === '/api/v1/run').body.account_ids, ['a1'])
  assert.equal(await page.locator('.account-progress').count(), 1)
  await primary.getByRole('button', { name: '停止', exact: true }).click()
  await primary.getByRole('button', { name: '签到', exact: true }).waitFor()

  await page.getByRole('button', { name: '自选账号与任务' }).click()
  let selectionDialog = page.getByRole('dialog', { name: '选择本次任务' })
  await selectionDialog.locator('.run-account-option').filter({ hasText: '小号 · 星海同行' }).getByRole('checkbox').uncheck()
  await selectionDialog.getByRole('radio', { name: '仅游戏与云游戏' }).check()
  for (const checkbox of await selectionDialog.locator('.check-grid input').all()) await checkbox.uncheck()
  assert(await selectionDialog.getByRole('button', { name: '开始所选签到' }).isDisabled())
  await selectionDialog.getByLabel('原神', { exact: true }).check()
  await desk.screenshot('desktop-task-selection')
  await selectionDialog.getByRole('button', { name: '开始所选签到' }).click()
  await selectionDialog.waitFor({ state: 'hidden' })
  assert.deepEqual(desk.calls.findLast(c => c.path === '/api/v1/run').body, { account_ids: ['a1'], games_only: true, games: ['genshin'] })
  await primary.getByRole('button', { name: '停止', exact: true }).click()
  await primary.getByRole('button', { name: '签到', exact: true }).waitFor()

  await page.getByRole('button', { name: '查看全部日志' }).click()
  await page.getByLabel('搜索日志').fill('原石')
  assert.equal(await page.locator('.activity-row').count(), 1)
  const download = page.waitForEvent('download')
  await page.getByRole('button', { name: '导出', exact: true }).click()
  assert.equal((await download).suggestedFilename(), 'miyohub-logs.txt')
  await page.getByLabel('搜索日志').fill('')
  await page.getByRole('navigation', { name: '主导航', exact: true }).getByRole('button', { name: '任务总览', exact: true }).click()

  await page.getByRole('button', { name: '绑定账号', exact: true }).click()
  let dialog = page.getByRole('dialog')
  await dialog.getByLabel('账号名称').fill('短信测试账号')
  await dialog.getByRole('button', { name: '生成二维码', exact: true }).click()
  await dialog.locator('canvas').waitFor({ state: 'visible' })
  await desk.screenshot('desktop-qr')
  await dialog.getByRole('button', { name: '刷新二维码', exact: true }).click()
  await dialog.getByRole('button', { name: '刷新二维码', exact: true }).waitFor()
  await dialog.getByRole('button', { name: '刷新二维码', exact: true }).focus()
  await page.keyboard.press('Tab')
  assert.equal(await page.evaluate(() => document.activeElement.getAttribute('aria-label')), '关闭弹窗')
  await page.keyboard.press('Shift+Tab')
  assert((await page.evaluate(() => document.activeElement.textContent)).includes('刷新二维码'))
  await dialog.getByRole('button', { name: '短信登录', exact: true }).click()
  await dialog.locator('input[type=tel]').fill('13800138000')
  await dialog.getByRole('button', { name: '获取验证码', exact: true }).click()
  await dialog.getByText('短信验证码已发送至 138****8000', { exact: true }).waitFor()
  assert(await dialog.getByRole('button', { name: /秒后可重发/ }).isDisabled())
  await dialog.locator('input[autocomplete=one-time-code]').fill('123456')
  await desk.screenshot('desktop-sms')
  await dialog.getByRole('button', { name: '验证并绑定账号', exact: true }).click()
  await page.getByRole('dialog').waitFor({ state: 'hidden' })
  await page.locator('.account-card').filter({ hasText: '短信测试账号' }).waitFor()

  await page.getByRole('navigation', { name: '主导航', exact: true }).getByRole('button', { name: '商品兑换', exact: true }).click()
  await page.locator('.good-card').first().waitFor()
  await page.getByRole('button', { name: '虚拟商品', exact: false }).click()
  const shopImage = page.locator('.good-card img').first()
  assert.equal(await shopImage.getAttribute('loading'), 'lazy')
  assert.equal(await shopImage.getAttribute('decoding'), 'async')
  assert.equal(await shopImage.getAttribute('referrerpolicy'), 'no-referrer')
  await shopImage.dispatchEvent('error')
  assert(await page.getByRole('img', { name: '商品图片暂不可用', exact: true }).isVisible())
  await desk.noOverflow('desktop shop')
  await desk.screenshot('desktop-shop')
  await page.getByLabel('搜索商品', { exact: true }).fill('原神')
  assert.equal(await page.locator('.good-card').count(), 1)
  await page.getByLabel('搜索商品', { exact: true }).fill('')
  await choose(page, '商品状态', 'online')
  assert.equal(await page.locator('.good-card').count(), 2)
  await choose(page, '商品状态', '')
  await choose(page, '商品分区', 'hk4e')
  await page.waitForFunction(() => document.querySelectorAll('.good-card').length === 1)
  await choose(page, '商品分区', '')
  await page.waitForFunction(() => document.querySelectorAll('.good-card').length === 3)

  await page.getByRole('button', { name: '实物商品', exact: false }).click()
  await page.locator('.good-card').filter({ hasText: '派蒙的星光纪念徽章' }).getByRole('button', { name: '查看详情' }).click()
  dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: '预约这件商品' }).click()
  await dialog.getByRole('button', { name: '保存预约', exact: true }).waitFor()
  await page.waitForFunction(() => ![...document.querySelectorAll('[role=dialog] button')].find(el => el.textContent.trim() === '保存预约')?.disabled)
  assert((await dialog.getByRole('combobox', { name: '收货地址', exact: true }).textContent()).includes('测试收件人'))
  await desk.screenshot('desktop-booking')
  await dialog.getByRole('button', { name: '保存预约', exact: true }).click()
  await page.getByRole('dialog').waitFor({ state: 'hidden' })
  const booking = desk.calls.findLast(c => c.path === '/api/v1/shop/plans' && c.method === 'POST').body
  assert(booking.auto && booking.address_id === 'addr-test' && booking.exchange_at > Date.now() / 1000)

  await page.getByRole('button', { name: '虚拟商品', exact: false }).click()
  await page.locator('.good-card').filter({ hasText: '原神 · 旅途补给礼包' }).getByRole('button', { name: '查看详情' }).click()
  dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: '选择账号与兑换方式' }).click()
  await choose(page, '兑换账号', 'a2')
  await dialog.getByText('还差 1400 米游币，余额不足，暂不能保存或执行兑换。', { exact: true }).waitFor()
  assert(await dialog.getByRole('button', { name: '保存计划', exact: true }).isDisabled())
  await choose(page, '兑换账号', 'a1')
  await page.waitForFunction(() => ![...document.querySelectorAll('[role=dialog] button')].find(el => el.textContent.trim() === '直接兑换')?.disabled)
  assert((await dialog.getByRole('combobox', { name: '接收奖励的角色', exact: true }).textContent()).includes('旅行者'))
  await dialog.getByRole('button', { name: '直接兑换', exact: true }).click()
  dialog = page.getByRole('dialog', { name: '确认兑换', exact: true })
  await dialog.waitFor()
  assert.equal(desk.calls.filter(c => c.path === '/api/v1/shop/plans/run').length, 0, 'must not exchange before confirmation')
  await desk.screenshot('desktop-confirm')
  await dialog.getByRole('button', { name: '确认兑换', exact: true }).click()
  await dialog.waitFor({ state: 'hidden' })
  await page.locator('.exchange-plan[data-state=running]').waitFor()
  assert.equal(desk.calls.filter(c => c.path === '/api/v1/shop/plans/run').length, 1)
  await page.locator('.exchange-plan').filter({ hasText: '上次的旅途补给' }).getByRole('button', { name: '重新建计划' }).click()
  dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: '保存计划', exact: true }).waitFor()
  await page.keyboard.press('Escape')
  await dialog.waitFor({ state: 'hidden' })

  await page.getByRole('navigation', { name: '主导航', exact: true }).getByRole('button', { name: '系统设置', exact: true }).click()
  await page.getByRole('heading', { name: '运行与每日调度', exact: true }).waitFor()
  assert.equal(await page.getByLabel('大别野', { exact: true }).count(), 0, 'account-level tasks remained in site settings')
  await desk.noOverflow('desktop settings')
  await desk.screenshot('desktop-settings')
  await separatedSettings(desk, 'desktop')
  await desk.finish('desktop flows')

  const mobile = await setup({ ...devices['iPhone 13'], defaultBrowserType: undefined })
  await mobile.noOverflow('mobile dashboard')
  assert(await mobile.page.getByRole('navigation', { name: '移动导航' }).isVisible())
  await mobile.screenshot('mobile-dashboard')
  await mobile.page.getByRole('button', { name: '绑定账号', exact: true }).click()
  await mobile.page.getByRole('dialog').getByRole('button', { name: '短信登录', exact: true }).click()
  await mobile.noOverflow('mobile SMS dialog')
  await mobile.screenshot('mobile-sms')
  await mobile.page.getByRole('button', { name: '关闭弹窗' }).click()
  await mobile.page.getByRole('navigation', { name: '移动导航' }).getByRole('button', { name: '商品兑换' }).click()
  await mobile.page.locator('.good-card').first().waitFor()
  mobile.setPlans([makePlan({ state: 'running', phase: 'waiting', last_result: '准备完成，等待兑换时间' }), makePlan({ id: 'unknown', state: 'unknown', auto: false, goods_name: '待核对的兑换', last_result: '请求结果无法确认', attempt: 1 })])
  await mobile.page.getByRole('button', { name: '刷新数据' }).click()
  await mobile.page.getByText('准备就绪', { exact: true }).waitFor()
  assert.equal(await mobile.page.locator('.exchange-plan[data-state=unknown]').getByRole('button', { name: '重新建计划' }).count(), 0)
  await mobile.noOverflow('mobile shop')
  await mobile.screenshot('mobile-shop')
  await mobile.page.locator('.good-card').filter({ hasText: '派蒙的星光纪念徽章' }).getByRole('button', { name: '查看详情' }).click()
  await mobile.page.getByRole('button', { name: '预约这件商品' }).click()
  await mobile.page.getByRole('button', { name: '保存预约', exact: true }).waitFor()
  await mobile.noOverflow('mobile booking')
  await mobile.screenshot('mobile-booking')
  await mobile.page.getByRole('button', { name: '保存预约', exact: true }).scrollIntoViewIfNeeded()
  assert(await mobile.page.getByRole('button', { name: '保存预约', exact: true }).isVisible())
  await mobile.screenshot('mobile-booking-actions')
  await mobile.page.getByRole('button', { name: '关闭弹窗' }).scrollIntoViewIfNeeded()
  await mobile.page.getByRole('button', { name: '关闭弹窗' }).click()
  await mobileSettings(mobile)
  await mobile.page.getByRole('heading', { name: '运行与每日调度', exact: true }).waitFor()
  await mobile.noOverflow('mobile settings')
  await separatedSettings(mobile, 'mobile')
  await mobile.finish('mobile layout and phases')

  const narrow = await setup({ viewport: { width: 600, height: 900 } }, true)
  assert(await narrow.page.getByRole('navigation', { name: '移动导航' }).isVisible(), 'narrow desktop UA must show mobile navigation')
  assert(await narrow.page.getByRole('button', { name: '开始今日签到' }).isDisabled())
  await narrow.page.getByText('尚未绑定米游社账号', { exact: true }).waitFor()
  await narrow.noOverflow('empty narrow dashboard')
  await narrow.screenshot('narrow-empty-dashboard')
  await narrow.page.getByRole('navigation', { name: '移动导航' }).getByRole('button', { name: '商品兑换' }).click()
  await narrow.page.locator('.good-card').first().waitFor()
  await narrow.noOverflow('empty narrow shop')
  await narrow.finish('narrow desktop UA and empty state')

  const tiny = await setup({ viewport: { width: 320, height: 780 } }, false, 'user')
  await tiny.noOverflow('320px dashboard')
  await tiny.page.getByRole('button', { name: '自选账号与任务' }).click()
  await tiny.noOverflow('320px task selection')
  await tiny.page.getByRole('button', { name: '开始所选签到' }).scrollIntoViewIfNeeded()
  assert(await tiny.page.getByRole('button', { name: '开始所选签到' }).isVisible())
  await tiny.screenshot('tiny-task-selection')
  await tiny.page.getByRole('button', { name: '关闭弹窗' }).click()
  await tiny.page.getByRole('navigation', { name: '移动导航' }).getByRole('button', { name: '更多', exact: true }).click()
  assert.equal(await tiny.page.getByRole('navigation', { name: '更多导航' }).getByRole('button', { name: /系统设置/ }).count(), 0)
  await tiny.page.getByRole('navigation', { name: '更多导航' }).getByRole('button', { name: /打码服务/ }).click()
  await tiny.page.getByRole('heading', { name: '我的打码服务', exact: true }).waitFor()
  assert(await tiny.page.getByRole('radio', { name: '使用站点打码服务', exact: true }).isDisabled())
  await tiny.noOverflow('tiny personal captcha')
  assert.equal(tiny.calls.filter(call => call.path.startsWith('/api/v1/admin/')).length, 0)
  await tiny.finish('320px ordinary-user layout')

  const back = await setup({ viewport: { width: 390, height: 844 } })
  const mobileNav = back.page.getByRole('navigation', { name: '移动导航' })
  await mobileNav.getByRole('button', { name: '商品兑换', exact: true }).click()
  await back.page.getByRole('button', { name: '虚拟商品', exact: false }).click()
  await back.page.locator('.good-card').filter({ hasText: '原神 · 旅途补给礼包' }).getByRole('button', { name: '查看详情' }).click()
  await back.page.getByRole('button', { name: '选择账号与兑换方式', exact: true }).click()
  await back.page.getByRole('combobox', { name: '兑换账号', exact: true }).click()
  await back.page.goBack()
  await back.page.getByRole('listbox').waitFor({ state: 'hidden' })
  assert(await back.page.getByRole('combobox', { name: '兑换账号', exact: true }).isVisible())
  await back.page.goBack()
  await back.page.getByRole('button', { name: '选择账号与兑换方式', exact: true }).waitFor()
  await back.page.goBack()
  await back.page.getByRole('dialog').waitFor({ state: 'hidden' })
  await back.page.goBack()
  await back.page.waitForURL(/#dashboard$/)
  await mobileNav.getByRole('button', { name: '更多', exact: true }).click()
  await back.page.getByRole('navigation', { name: '更多导航' }).getByRole('button', { name: '运行日志', exact: true }).click()
  await back.page.waitForURL(/#logs$/)
  await back.page.goBack()
  await back.page.waitForURL(/#dashboard$/)
  assert.equal(await back.page.getByRole('dialog').count(), 0, 'dismissed menu left a phantom back entry')
  await back.page.locator('.account-card').first().getByRole('button', { name: '签到设置', exact: true }).click()
  const floating = back.page.getByRole('button', { name: '快速保存签到设置', exact: true })
  assert(!await floating.isVisible(), 'clean editor shows an unnecessary floating save')
  await back.page.getByRole('checkbox', { name: '自动保存配置', exact: true }).uncheck()
  await back.page.getByLabel('参加每日自动签到', { exact: true }).uncheck()
  await back.page.getByText('按角色 UID 排除', { exact: true }).click()
  await back.page.getByRole('dialog').evaluate(el => { el.scrollTop = 0 })
  await floating.waitFor({ state: 'visible' })
  const floatingBounds = await floating.boundingBox()
  assert(floatingBounds && floatingBounds.x + floatingBounds.width > 350 && floatingBounds.y + floatingBounds.height > 790 && floatingBounds.y + floatingBounds.height <= 844, 'save button is not at the visible bottom-right corner')
  await back.page.goBack()
  await back.page.getByRole('button', { name: '继续编辑', exact: true }).waitFor()
  assert.equal(new URL(back.page.url()).hash, '#dashboard', 'dirty editor exited the page')
  await back.page.getByRole('button', { name: '放弃修改', exact: true }).click()
  await back.page.getByRole('dialog').waitFor({ state: 'hidden' })
  await mobileNav.getByRole('button', { name: '商品兑换', exact: true }).click()
  await back.page.waitForURL(/#shop$/)
  await back.page.goBack()
  await back.page.waitForURL(/#dashboard$/)
  await back.page.goForward()
  await back.page.waitForURL(/#shop$/)
  assert.equal(await back.page.getByRole('dialog').count(), 0)
  await back.finish('mobile layered back, dirty guard and forward navigation')

  await testInfo.attach('flow-report', { body: JSON.stringify({ ok: true, reports }, null, 2), contentType: 'application/json' })
} catch (error) {
  for (const context of browser.contexts()) for (const page of context.pages()) await page.screenshot({ path: output + '/failure-' + Date.now() + '.png', fullPage: true }).catch(() => {})
  throw error
}
})

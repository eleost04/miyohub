import assert from 'node:assert/strict'
import { expect, test } from '@playwright/test'
import QRCode from 'qrcode'
import { choose, addCaptcha } from './ui.mjs'

const origin = 'http://127.0.0.1:4177'
const secrets = ['token', 'webhook', 'secret', 'api_url', 'client_secret', 'push_url', 'access_token', 'smtp_password', 'context_token', 'sync_cursor']
const defaults = provider => ({ id: '', name: provider, provider, enable: false, token: '', webhook: '', secret: '', api_url: '', chat_id: '', app_id: '', client_secret: '', openid: '', topic: '', push_url: '', access_token: '', send_id: '', msg_type: 'private', smtp_host: '', smtp_port: 465, smtp_user: '', smtp_password: '', mail_from: '', mail_to: '', smtp_ssl: true, mode: '', bot_id: '', context_token: '', sync_cursor: '', binding_state: '' })

async function workspace(browser, width = 1440, role = 'admin') {
  const context = await browser.newContext({ viewport: { width, height: 980 }, timezoneId: 'Asia/Shanghai', reducedMotion: 'reduce' })
  const page = await context.newPage(), calls = [], errors = []
  page.setDefaultTimeout(10_000)
  page.on('pageerror', e => errors.push(e.message))
  page.on('console', msg => { if (msg.type() === 'error') errors.push(msg.text()) })
  const user = { id: 'owner', username: '旅行者', role, status: 'active', permissions: { exchange: false, site_captcha: false } }
  const config = { enabled: true, accounts: [], features: { game_checkin: true, cloud_game_checkin: false, bbs_tasks: true }, games: { enabled: ['genshin'], black_list: {} }, cloud_games: { enabled: [] }, bbs: { forums: [2], checkin: true, read: true, like: false, share: false, cancel_like: false, post_limit: 5, delay_seconds: [1, 3] }, schedule: { enable: false, time: '09:00', timezone: 'Asia/Shanghai', jitter_minutes: 5, run_on_start: false }, push: { channels: [], error_only: false }, captcha: { max_retries: 2, channels: [] }, shop_exchange: { enable: true, retry_seconds: 10, retry_interval: .5, plans: [] } }
  let settings = { enable: false, tasks: true, exchange: true, error_only: false, revision: 1, channels: [] }, history = [], qr = { status: 'idle' }, serial = 0, failTest = false
  const image = await QRCode.toDataURL('https://example.invalid/miyohub-test-only', { width: 288 })
  function publicSettings() { return { ...settings, channels: settings.channels.map(c => { const out = { ...c, configured: secrets.filter(key => !!c[key]) }; secrets.forEach(key => { out[key] = '' }); return out }) } }
  function confirmQR() {
    const isQQ = qr.provider === 'qqbot'
    const c = { ...defaults(qr.provider), id: qr.channel_id || 'channel-' + ++serial, name: isQQ ? 'QQ 官方机器人' : '微信 · 扫码直连', enable: true, app_id: isQQ ? '123456' : '', client_secret: isQQ ? 'QQ_PRIVATE' : '', openid: 'scanning-user', mode: isQQ ? '' : 'ilink', token: isQQ ? '' : 'WX_PRIVATE', bot_id: isQQ ? '' : 'weixin-test-bot', api_url: isQQ ? '' : 'https://ilinkai.weixin.qq.com', binding_state: isQQ ? 'ready' : 'waiting_message' }
    settings.channels = [...settings.channels.filter(old => old.id !== c.id), c]; settings.revision++
    qr = { ...qr, channel_id: c.id, status: 'confirmed', running: false, qr_image: '', qr_url: '', message: isQQ ? 'QQ 已绑定，凭据已安全保存。' : '微信已绑定，请先给机器人发送一条消息。' }
  }
  await context.route('**/*', async route => {
    if (new URL(route.request().url()).origin !== origin) { errors.push('External request blocked'); return route.abort() }
    return route.continue()
  })
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname, method = request.method(), body = request.postDataJSON() || {}
    calls.push({ path, method, body })
    const ok = data => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true, data }) })
    if (path === '/api/v1/bootstrap') return ok({ auth: { has_admin: true, need_auth: true, registration_mode: 'review' }, user, config, status: { running: false, logs: [] } })
    if (path === '/api/v1/auth/status') return ok({ has_admin: true, need_auth: true, registration_mode: 'review' })
    if (path === '/api/v1/auth/me') return ok(user)
    if (path === '/api/v1/config') return ok(config)
    if (path === '/api/v1/captcha/config') return ok({ source: 'off', revision: 1, max_retries: 3, channels: [], site_allowed: role === 'admin', site_available: true })
    if (path === '/api/v1/captcha/test' && route.request().method() === 'GET') return ok({ probe: null })
    if (path === '/api/v1/status') return ok({ running: false, accounts: [], exchange: config.shop_exchange, logs: [{ at: new Date().toISOString(), component: 'bbs', message: '测试账号：增币尚未确认，请核对米游社' }, { at: new Date().toISOString(), component: 'games', message: '原神签到成功' }, { at: new Date().toISOString(), component: 'push', message: '通用 Webhook：服务已接受通知（不代表终端已读）' }] })
    if (path === '/api/v1/admin/users') return ok({ users: [user], registration_mode: 'review' })
    if (path === '/api/v1/admin/invite-codes') return ok([])
    if (path === '/api/v1/push/config') {
      if (method === 'PUT') {
        assert.equal(body.revision, settings.revision)
        settings = { ...body, revision: settings.revision + 1, channels: body.channels.map(c => { const old = settings.channels.find(item => item.id === c.id); const next = { ...defaults(c.provider), ...c, id: c.id || 'channel-' + ++serial }; secrets.forEach(key => { if (c.clear_fields?.includes(key)) next[key] = ''; else if (!c[key]) next[key] = old?.[key] || '' }); return next }) }
      }
      return ok(publicSettings())
    }
    if (path === '/api/v1/push/history') return ok(history)
    if (path === '/api/v1/push/test') {
      const c = settings.channels.find(c => c.id === body.channel_id); assert(c)
      const result = { channel_id: c.id, name: c.name, provider: c.provider, ok: !failTest, error: failTest ? '模拟渠道额度不足，请检查配置' : '' }
      history.unshift({ id: 'delivery-' + ++serial, channel_id: c.id, channel_name: c.name, provider: c.provider, kind: 'test', title: '推送测试', status: failTest ? 'failed' : 'accepted', error: result.error, created_at: new Date().toISOString() })
      return ok({ all_ok: !failTest, results: [result] })
    }
    if (path === '/api/v1/push/qr/start') { qr = { session_id: 'qr-' + ++serial, provider: body.provider, channel_id: body.channel_id, status: 'waiting', running: true, qr_image: image, qr_url: 'https://q.qq.com/qqbot/openclaw/connect.html?task_id=test-only', message: '请扫码并在官方页面确认', expires_at: new Date(Date.now() + 300000).toISOString() }; return ok(qr) }
    if (path === '/api/v1/push/qr') { if (qr.provider === 'wechat_claw' && qr.status === 'waiting') qr = { ...qr, status: 'need_verifycode', message: '请输入手机显示的数字配对码' }; return ok(qr) }
    if (path === '/api/v1/push/qr/verify') { assert.equal(body.code, '123456'); confirmQR(); return ok({}) }
    if (path === '/api/v1/push/qr/cancel') { if (body.session_id === qr.session_id) qr = { ...qr, running: false, status: 'cancelled' }; return ok({}) }
    errors.push('Unhandled API: ' + path); return route.fulfill({ status: 500, contentType: 'application/json', body: '{"ok":false,"error":"Unexpected mock request"}' })
  })
  await page.goto(origin + '/#notifications')
  await page.getByRole('button', { name: 'QQ 扫码绑定' }).waitFor()
  async function navigate(name) {
    if (width > 640) await page.getByRole('navigation', { name: '主导航', exact: true }).getByRole('button', { name, exact: true }).click()
    else if (['任务总览', '商品兑换', '消息推送'].includes(name)) await page.getByRole('navigation', { name: '移动导航', exact: true }).getByRole('button', { name, exact: true }).click()
    else { await page.getByRole('navigation', { name: '移动导航', exact: true }).getByRole('button', { name: '更多', exact: true }).click(); await page.getByRole('navigation', { name: '更多导航', exact: true }).getByRole('button', { name: new RegExp(name) }).click() }
  }
  async function noOverflow(label) { const d = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, width: innerWidth })); assert(d.scroll <= d.width + 1, label + ': ' + JSON.stringify(d)) }
  return { context, page, calls, errors, navigate, noOverflow, confirmQR, fail: () => { failTest = true }, expire: () => { qr = { ...qr, running: false, status: 'expired', message: '二维码已过期，请刷新' } }, settings: () => settings }
}

test('十种推送渠道、QQ/微信扫码、发送结果与独立后台', async ({ browser }, testInfo) => {
  const w = await workspace(browser), { page } = w
  try {
    assert.equal(await page.locator('.push-provider-tile').count(), 10)
    assert((await page.locator('.push-provider-name').first().boundingBox()).width >= 90, 'desktop provider labels are squeezed by inline hints')
    await w.noOverflow('notifications overview')
    await page.screenshot({ path: testInfo.outputPath('notifications-overview.png'), fullPage: true })
    await page.locator('.push-provider-tile[data-provider="webhook"]').click()
    await page.getByRole('dialog').getByRole('button', { name: '配置并添加', exact: true }).click()
    await page.getByRole('dialog').getByLabel('Webhook 地址', { exact: true }).fill('https://example.invalid/not-saved')
    await page.goBack()
    await page.getByRole('dialog', { name: '通用 Webhook · 接收方式', exact: true }).waitFor()
    await page.getByRole('button', { name: '暂不添加', exact: true }).click()
    assert.equal(w.settings().channels.length, 0, 'cancelled provider setup was persisted')
    assert.equal(w.calls.filter(c => c.path === '/api/v1/push/config' && c.method === 'PUT').length, 0)
    await page.getByRole('button', { name: 'QQ 扫码绑定' }).click()
    let modal = page.getByRole('dialog')
    await modal.getByAltText('QQ 官方机器人绑定二维码').waitFor()
    await page.screenshot({ path: testInfo.outputPath('qq-binding.png') })
    w.confirmQR(); await modal.getByRole('heading', { name: '绑定成功' }).waitFor()
    await modal.getByRole('button', { name: '完成', exact: true }).click()
    await page.getByRole('button', { name: '微信扫码绑定' }).click()
    modal = page.getByRole('dialog')
    await modal.getByLabel('微信数字配对码').waitFor()
    await page.screenshot({ path: testInfo.outputPath('weixin-pairing.png') })
    await modal.getByLabel('微信数字配对码').fill('123456')
    await modal.getByRole('button', { name: '确认配对码' }).click()
    await modal.getByRole('heading', { name: '绑定成功' }).waitFor()
    await modal.getByRole('button', { name: '完成', exact: true }).click()
    assert.equal(w.settings().channels.length, 2)
    const cases = [
      ['pushplus', [['Token', 'PUSHPLUS_TEST']]],
      ['telegram', [['Token', '123:test-token'], ['Chat ID', '10001']]],
      ['wxpusher', [['AppToken', 'APP_TEST'], ['用户 UID', 'UID_TEST']]],
      ['dingrobot', [['Webhook 地址', 'https://example.invalid/ding']]],
      ['feishubot', [['Webhook 地址', 'https://example.invalid/feishu']]],
      ['qq', [['OneBot 基础地址', 'https://example.invalid/onebot'], ['Access Token', 'ONEBOT_TEST'], ['接收 QQ 号 / 群号', '10001']]],
      ['email', [['SMTP 主机', 'smtp.example.invalid'], ['SMTP 用户名', 'from@example.invalid'], ['SMTP 授权码 / 密码', 'SMTP_TEST'], ['收件邮箱', 'to@example.invalid']]],
      ['webhook', [['Webhook 地址', 'https://example.invalid/hook']]],
    ]
    for (const [provider, fields] of cases) {
      await page.locator('.push-provider-tile[data-provider="' + provider + '"]').click()
      await page.getByRole('dialog').getByRole('button', { name: '配置并添加', exact: true }).click()
      const editor = page.getByRole('dialog')
      for (const [label, value] of fields) await editor.getByLabel(label, { exact: true }).fill(value)
      await editor.getByRole('checkbox', { name: '启用这个接收渠道', exact: true }).check()
      assert(await editor.getByRole('button', { name: '发送测试消息' }).isDisabled())
      await editor.getByRole('button', { name: '保存推送配置', exact: true }).click()
      await page.getByText('推送配置已保存，密钥不会回传到浏览器。', { exact: true }).waitFor()
      const channel = page.locator('.push-channel').last()
      await channel.locator('.push-channel-expand').click()
      assert(await channel.locator('input[type=password]').evaluateAll(inputs => inputs.every(input => input.value === '')))
    }
    assert.equal(await page.getByRole('button', { name: '添加渠道', exact: true }).count(), 0)
    assert.equal(await page.getByRole('combobox', { name: '新渠道类型', exact: true }).count(), 0)
    assert.equal(w.settings().channels.length, 10)
    const last = page.locator('.push-channel').last()
    await last.getByRole('button', { name: '发送测试消息' }).click()
    await last.getByText('推送服务已接收测试消息。', { exact: true }).waitFor()
    await expect(page.locator('.push-delivery').first().getByText('服务已接收', { exact: true })).toBeVisible()
    assert(!/终端已读|已送达/.test(await page.locator('.push-history').innerText()))
    assert.equal(w.settings().enable, false, 'manual test enabled automatic pushes')
    w.fail(); await last.getByRole('button', { name: '发送测试消息' }).click()
    await last.locator('.push-test-result.is-error').waitFor()
    await page.locator('.push-delivery').filter({ hasText: '发送失败' }).waitFor()
    await page.getByRole('checkbox', { name: '自动保存配置', exact: true }).uncheck()
    await page.getByRole('checkbox', { name: '启用自动推送', exact: true }).check()
    await w.navigate('系统设置')
    await page.getByRole('dialog', { name: '推送配置尚未保存' }).getByRole('button', { name: '返回编辑' }).click()
    assert(await page.getByRole('checkbox', { name: '启用自动推送', exact: true }).isChecked())
    await page.getByRole('button', { name: '保存推送配置', exact: true }).click()
    await page.getByText('推送配置已保存，密钥不会回传到浏览器。', { exact: true }).waitFor()
    await w.navigate('系统设置'); await page.locator('.settings-form').waitFor()
    assert.equal(await page.locator('.push-panel, .admin-panel, .profile-security').count(), 0)
    await w.navigate('后台管理'); await page.locator('.admin-panel').first().waitFor()
    await expect(page.locator('.admin-panel')).toHaveCount(2)
    assert.equal(await page.locator('.settings-form').count(), 0)
    await w.navigate('运行日志')
    await expect(page.getByText('通用 Webhook：推送服务已接收通知', { exact: true })).toBeVisible()
    assert(!/终端已读/.test(await page.locator('.logs-panel').innerText()))
    await page.getByLabel('搜索日志', { exact: true }).fill('推送服务已接收')
    await expect(page.locator('.activity-row')).toHaveCount(1)
    await page.getByLabel('搜索日志', { exact: true }).fill('')
    await page.getByRole('button', { name: '米游币任务', exact: true }).click()
    await page.getByRole('button', { name: '只看异常', exact: true }).click()
    assert.equal(await page.locator('.activity-row').count(), 1)
    assert(await page.locator('.activity-row').getByText(/增币尚未确认/).isVisible())
    await page.screenshot({ path: testInfo.outputPath('logs-filtered.png'), fullPage: true })
    assert.deepEqual(w.errors, [])
  } finally { await w.context.close() }
})

for (const width of [390, 320]) test(`验证码服务 ${width}px 的双渠道配置布局`, async ({ browser }, testInfo) => {
  const w = await workspace(browser, width, 'admin'), { page } = w
  try {
    await w.navigate('系统设置')
    const panel = page.locator('.captcha-panel')
    await addCaptcha(page, panel, 'custom', dialog => dialog.getByLabel('接口地址', { exact: true }).fill('http://miyohub-captcha:9645/pass_nine'))
    await addCaptcha(page, panel, 'damagou', async dialog => {
      await w.noOverflow('mobile captcha settings sheet')
      assert.equal(await page.evaluate(() => document.body.style.overflow), 'hidden', 'closing nested selector unlocked the modal')
      await dialog.getByLabel(/打码狗 UserKey/).fill('test-only-key')
      await page.screenshot({ path: testInfo.outputPath('captcha-settings-sheet.png') })
    })
    await panel.getByRole('button', { name: '上移验证码渠道 2', exact: true }).click()
    assert(await panel.locator('.captcha-channel').first().getByText('打码狗', { exact: true }).isVisible())
    await w.noOverflow('mobile captcha editor')
    await panel.screenshot({ path: testInfo.outputPath('captcha-services-mobile.png') })
    assert.deepEqual(w.errors, [])
  } finally { await w.context.close() }
})

for (const width of [390, 320]) test(`普通用户 ${width}px 的推送与扫码取消、过期流程`, async ({ browser }, testInfo) => {
  const w = await workspace(browser, width, 'user'), { page } = w
  try {
    await w.noOverflow('mobile push')
    await page.screenshot({ path: testInfo.outputPath('mobile-notifications.png'), fullPage: true })
    await page.getByRole('button', { name: 'QQ 扫码绑定' }).click()
    let modal = page.getByRole('dialog'); await modal.getByAltText('QQ 官方机器人绑定二维码').waitFor()
    await w.noOverflow('mobile QR')
    w.expire(); await modal.getByText('二维码已过期，请刷新', { exact: true }).waitFor()
    await modal.getByRole('button', { name: '刷新二维码' }).click()
    await modal.getByAltText('QQ 官方机器人绑定二维码').waitFor()
    await modal.getByRole('button', { name: '取消绑定', exact: true }).click()
    await modal.waitFor({ state: 'hidden' })
    assert.equal(w.settings().channels.length, 0)
    assert(w.calls.some(c => c.path === '/api/v1/push/qr/cancel' && c.body.session_id))
    await page.getByRole('button', { name: '微信扫码绑定' }).click()
    modal = page.getByRole('dialog'); await modal.getByLabel('微信数字配对码').waitFor()
    await w.noOverflow('mobile pairing')
    await page.screenshot({ path: testInfo.outputPath('mobile-weixin-pairing.png') })
    await modal.getByRole('button', { name: '取消绑定', exact: true }).click()
    await page.locator('.push-provider-tile[data-provider="telegram"]').click()
    await page.getByRole('dialog', { name: 'Telegram · 接收方式', exact: true }).waitFor()
    await page.screenshot({ path: testInfo.outputPath('provider-introduction.png') })
    await page.getByRole('button', { name: '配置并添加', exact: true }).click()
    modal = page.getByRole('dialog', { name: '配置 Telegram', exact: true })
    await modal.getByLabel('Token', { exact: true }).fill('TEST_ONLY_TELEGRAM')
    await modal.getByLabel('Chat ID', { exact: true }).fill('100001')
    await w.noOverflow('provider parameter dialog')
    await page.screenshot({ path: testInfo.outputPath('provider-configuration.png') })
    await page.goBack()
    await page.getByRole('dialog', { name: 'Telegram · 接收方式', exact: true }).waitFor()
    await page.goBack()
    await page.getByRole('dialog').waitFor({ state: 'hidden' })
    assert.equal(w.settings().channels.length, 0, 'cancelled provider dialog saved an incomplete channel')
    await page.getByRole('navigation', { name: '移动导航' }).getByRole('button', { name: '更多', exact: true }).click()
    assert.equal(await page.getByRole('navigation', { name: '更多导航' }).getByRole('button', { name: /后台管理/ }).count(), 0)
    assert.equal(await page.getByRole('navigation', { name: '更多导航' }).getByRole('button', { name: /系统设置/ }).count(), 0)
    await page.getByRole('navigation', { name: '更多导航' }).getByRole('button', { name: /打码服务/ }).click()
    await expect(page.getByRole('radio', { name: '使用站点打码服务', exact: true })).toBeDisabled()
    await expect(page.getByRole('radio', { name: '使用自己的打码服务', exact: true })).toBeEnabled()
    await w.noOverflow('mobile personal captcha')
    await w.navigate('个人账号'); await page.locator('.profile-security').waitFor()
    await w.noOverflow('mobile profile')
    assert.equal(w.calls.filter(c => c.path.startsWith('/api/v1/admin/')).length, 0)
    assert.deepEqual(w.errors, [])
  } finally { await w.context.close() }
})

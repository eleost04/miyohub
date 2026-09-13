import assert from 'node:assert/strict'
import { expect, test } from './api-test.mjs'
import { choose, dismissGuide } from './ui.mjs'

for (const width of [320, 390, 1440]) test(`按需便笺与安全提示 ${width}px`, async ({ browser }, testInfo) => {
  const origin = 'http://127.0.0.1:5897', headers = { 'Content-Type': 'application/json', 'X-MiyoHub-Request': '1' }
  const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: 'reduce' })
  async function request(method, path, data) {
    const response = await context.request.fetch(origin + path, { method, headers, ...(data ? { data } : {}) })
    const body = await response.json(); assert(response.ok() && body.ok, body.error || path); return body.data
  }
  try {
    await request('POST', '/api/v1/auth/setup', { username: 'record-owner', password: 'local-test-only-123' })
    await dismissGuide(context)
    await request('POST', '/api/v1/accounts', { name: '便笺测试账号', cookie: 'stuid=901001;cookie_token=record-fixture', disabled: false })
    const effects = [], errors = [], reads = []
    await context.route('**/*', route => {
      const url = new URL(route.request().url())
      if (url.origin !== origin || /^\/api\/v1\/(run|login\/|accounts\/check|push\/|shop\/)/.test(url.pathname)) { effects.push(url.pathname); return route.abort() }
      if (url.pathname === '/api/v1/game-record/note') {
        reads.push(Object.fromEntries(url.searchParams))
        const roles = [{ uid: '902001', region: 'cn_gf01', nickname: '测试角色', region_name: '天空岛' }, { uid: '902002', region: 'cn_qd01', nickname: '第二角色', region_name: '世界树' }]
        const role = roles.find(r => r.uid === url.searchParams.get('role_id')) || roles[0]
        const limited = url.searchParams.get('game') === 'zzz'
        return route.fulfill({ json: { ok: true, data: { game: url.searchParams.get('game'), roles, role, status: limited ? 'verification' : 'ok', message: limited ? '上游要求安全验证，请在米游社官方客户端处理；本账号便笺与日历查询暂停 6 小时' : '', refresh_at: new Date(Date.now() + (limited ? 6 * 3600 : 180) * 1000).toISOString(), observed_at: new Date().toISOString(), cached: false, stale: false, ...(!limited ? { note: { metrics: [{ key: 'energy', label: '原粹树脂', current: 0, max: 200, recovery_seconds: 600 }, { key: 'daily', label: '每日委托', current: null, max: 4 }], flags: [{ key: 'reward', label: '委托额外奖励已领取', value: false }] } } : {}) } } })
      }
      return route.continue()
    })
    const page = await context.newPage(); page.on('pageerror', e => errors.push(e.message))
    await page.goto(origin + '/#notes')
    await expect(page.getByRole('heading', { name: '实时便笺', exact: true })).toBeVisible()
    assert.equal(reads.length, 0)
    await page.getByRole('button', { name: '读取便笺', exact: true }).click()
    await expect(page.locator('.note-metric').filter({ hasText: '原粹树脂' }).locator('strong')).toHaveText('0')
    await expect(page.locator('.note-metric').filter({ hasText: '每日委托' }).locator('strong')).toHaveText('未提供')
    await expect(page.locator('.note-flags strong')).toHaveText('否')
    await expect(page.getByRole('button', { name: /分钟后可更新/ })).toBeDisabled()
    await choose(page, '便笺角色', '902002:cn_qd01')
    assert.equal(reads.length, 1)
    await page.getByRole('button', { name: '读取便笺', exact: true }).click()
    await expect(page.locator('.note-observed>strong')).toHaveText('第二角色')
    assert.equal(reads[1].role_id, '902002'); assert.equal(reads[1].server, 'cn_qd01')
    await page.screenshot({ path: testInfo.outputPath('game-note.png') })
    await choose(page, '便笺游戏', 'zzz')
    await page.getByRole('button', { name: '读取便笺', exact: true }).click()
    await expect(page.getByRole('status').filter({ hasText: '上游要求安全验证' })).toBeVisible()
    await expect(page.getByRole('button', { name: /分钟后可更新/ })).toBeDisabled()
    assert.equal(reads.length, 3)
    await page.getByRole('combobox', { name: '便笺游戏', exact: true }).click()
    await page.goBack(); await expect(page.getByRole('listbox')).toHaveCount(0)
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
    assert.deepEqual(effects, []); assert.deepEqual(errors, [])
  } finally { await context.close() }
})

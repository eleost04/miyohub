import assert from 'node:assert/strict'
import { expect, test } from '@playwright/test'

const origin = 'http://127.0.0.1:4177'

async function workspace(browser, width) {
  const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: 'reduce', timezoneId: 'Asia/Shanghai', ...(width <= 640 ? { isMobile: true, hasTouch: true } : {}) })
  const page = await context.newPage(), errors = []
  const user = { id: 'log-owner', username: '日志测试用户', role: 'user', status: 'active', onboarding_status: 'dismissed', permissions: { exchange: false, site_captcha: false } }
  const config = { enabled: true, accounts: [], features: {}, games: { enabled: [], black_list: {} }, cloud_games: { enabled: [] }, bbs: { forums: [] }, schedule: { enable: false, time: '09:00', timezone: 'Asia/Shanghai', jitter_minutes: 0, run_on_start: false }, captcha: { max_retries: 2, channels: [] }, push: { channels: [], error_only: false }, shop_exchange: { enable: false, retry_seconds: 10, retry_interval: .5, plans: [] } }
  const rows = [
    ['task', '模拟同名账号: 开始执行任务', 'a1', 'run-one'],
    ['bbs', '模拟同名账号: 米游币任务设置：社区签到开启、看帖开启、点赞开启、分享关闭', 'a1', 'run-one'],
    ['bbs', '模拟同名账号: 米游币任务列表：本次返回的任务 ID：62、64', 'a1', 'run-one'],
    ['bbs', '模拟同名账号: 另一个账号的独立记录', 'a2', 'run-one'],
    ['bbs', '模拟同名账号: 看帖：任务列表未返回对应项目（ID 59），本次跳过', 'a1', 'run-one'],
    ['bbs', '模拟同名账号: 分享：账号设置未开启，本次跳过', 'a1', 'run-one'],
    ['bbs', '模拟同名账号: 米游币本次新增 50，今日已得 50，剩余可得 0，余额 2050', 'a1', 'run-one'],
    ['bbs', '模拟同名账号: 米游币操作汇总：成功 2，失败 0，跳过 3', 'a1', 'run-one'],
    ['task', '模拟同名账号: 任务执行结束', 'a1', 'run-one'],
    ['bbs', '模拟同名账号: 后一次执行失败：网络异常 <img src="https://example.invalid/not-an-image">', 'a1', 'run-two'],
    ['bbs', '模拟同名账号: 米游币操作汇总：成功 0，失败 1，跳过 0', 'a1', 'run-two'],
    ['bbs', '旧日志: 当时的详细原因'],
    ['bbs', '旧日志: 米游币操作汇总：成功 1，失败 0，跳过 1'],
  ]
  const logs = rows.map(([component, message, account_id, run_id], index) => ({ at: new Date(Date.UTC(2026, 8, 12, 0, 0, index)).toISOString(), component, message, account_id, run_id }))
  page.on('pageerror', e => errors.push(e.message))
  await context.route('**/*', async route => {
    const request = route.request(), url = new URL(request.url())
    if (url.origin !== origin) { errors.push('Unexpected external request'); return route.abort() }
    if (!url.pathname.startsWith('/api/')) return route.continue()
    const status = { running: false, logs, accounts: [], tasks: [], exchange: config.shop_exchange }
    const ok = data => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ ok: true, data }) })
    if (request.method() === 'GET' && url.pathname === '/api/v1/bootstrap') return ok({ auth: { has_admin: true, need_auth: true }, user, config, status })
    if (request.method() === 'GET' && url.pathname === '/api/v1/status') return ok(status)
    errors.push('Unexpected API: ' + request.method() + ' ' + url.pathname)
    return route.fulfill({ status: 500, contentType: 'application/json', body: '{"ok":false}' })
  })
  await page.goto(origin + '/#logs')
  await expect(page.locator('.activity-row')).toHaveCount(rows.length)
  return { context, page, errors, logs }
}

for (const width of [320, 1440]) test(`执行日志 ${width}px：按次明细、旧日志上下文和准确异常筛选`, async ({ browser }, testInfo) => {
  const w = await workspace(browser, width), { page } = w
  try {
    await page.getByRole('button', { name: '只看异常', exact: true }).click()
    await expect(page.locator('.activity-row')).toHaveCount(2)
    await expect(page.locator('.activity-row').filter({ hasText: '失败 0' })).toHaveCount(0)
    await page.getByRole('button', { name: '只看异常', exact: true }).click()
    await page.getByRole('searchbox', { name: '搜索日志' }).fill('成功 2，失败 0')
    const summary = page.locator('.activity-row').filter({ hasText: '模拟同名账号: 米游币操作汇总：成功 2，失败 0，跳过 3' })
    await expect(summary).toHaveCount(1)
    await summary.getByRole('button', { name: '查看米游币日志明细', exact: true }).click()
    let dialog = page.getByRole('dialog', { name: '日志详情', exact: true })
    await expect(dialog).toBeVisible()
    await expect(dialog.getByText('同一账号、同一次执行的已保留记录，按时间排列。', { exact: true })).toBeVisible()
    await expect(dialog.locator('.log-detail-entry')).toHaveCount(8)
    await expect(dialog.getByText(/ID 59/)).toBeVisible()
    await expect(dialog.getByText(/分享：账号设置未开启/)).toBeVisible()
    await expect(dialog.getByText(/米游币本次新增 50/)).toBeVisible()
    await expect(dialog.getByText(/另一个账号|后一次执行/)).toHaveCount(0)
    const downloadEvent = page.waitForEvent('download')
    await dialog.getByRole('button', { name: '导出本次记录', exact: true }).click()
    const download = await downloadEvent, stream = await download.createReadStream(), chunks = []
    for await (const chunk of stream) chunks.push(chunk)
    const text = Buffer.concat(chunks).toString('utf8')
    assert(text.includes('ID 59') && text.includes('米游币本次新增 50'))
    assert(!text.includes('另一个账号') && !text.includes('后一次执行'))
    await page.screenshot({ path: testInfo.outputPath('execution-log-details.png') })
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
    w.logs.push({ at: '2026-09-12T00:01:00.000Z', component: 'task', message: '新任务: 本次操作：成功 2，失败 0，跳过 0', account_id: 'a1', run_id: 'run-three' })
    await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')))
    await expect(page.locator('.activity-row')).toHaveCount(2)
    await page.goBack()
    await expect(dialog).toHaveCount(0)
    await expect(page.getByRole('searchbox', { name: '搜索日志' })).toHaveValue('成功 2，失败 0')
    await expect(summary.getByRole('button', { name: '查看米游币日志明细', exact: true })).toBeFocused()
    await page.getByRole('searchbox', { name: '搜索日志' }).fill('旧日志: 米游币操作汇总')
    await page.locator('.activity-row').getByRole('button', { name: '查看米游币日志明细', exact: true }).click()
    dialog = page.getByRole('dialog', { name: '日志详情', exact: true })
    await expect(dialog.getByText(/历史记录没有执行编号/)).toBeVisible()
    await expect(dialog.getByText(/旧日志: 当时的详细原因/)).toBeVisible()
    await expect(dialog.locator('.log-detail-entry img')).toHaveCount(0)
    await dialog.getByRole('button', { name: '关闭弹窗', exact: true }).click()
    assert.deepEqual(w.errors, [])
  } finally { await w.context.close() }
})

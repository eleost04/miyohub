import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { expect, test } from './api-test.mjs'
import { choose, dismissGuide } from './ui.mjs'

const releases = JSON.parse(readFileSync(new URL('../src/releases.json', import.meta.url), 'utf8'))
const currentVersion = releases[0].version
const linkedEntry = releases.flatMap(release => release.entries).find(entry => entry.commit)

for (const width of [390, 1440]) test(`更新日志 ${width}px：版本、筛选、关联提交和返回工作台`, async ({ browser }, testInfo) => {
  const origin = 'http://127.0.0.1:5897'
  const context = await browser.newContext({ viewport: { width, height: 900 } })
  try {
    const status = (await (await context.request.get(origin + '/api/v1/auth/status')).json()).data
    const response = await context.request.post(origin + (status.has_admin ? '/api/v1/auth/login' : '/api/v1/auth/setup'), {
      headers: { 'Content-Type': 'application/json', 'X-MiyoHub-Request': '1' }, data: { username: 'smoke-admin', password: 'local-test-only-123' },
    })
    assert(response.ok())
    await dismissGuide(context)
    const page = await context.newPage()
    await page.goto(origin)
    await expect(page.getByRole('heading', { name: '任务总览', exact: true })).toBeVisible()
    if (width < 640) {
      await page.getByRole('navigation', { name: '移动导航', exact: true }).getByRole('button', { name: '更多', exact: true }).click()
      await page.getByRole('navigation', { name: '更多导航', exact: true }).getByRole('button', { name: /更新日志/ }).click()
    } else await page.getByRole('navigation', { name: '主导航', exact: true }).getByRole('button', { name: '更新日志', exact: true }).click()
    await expect(page.getByRole('heading', { name: `MiyoHub ${currentVersion}`, exact: true })).toBeVisible()
    if (linkedEntry) {
      await choose(page, '更新类型', linkedEntry.category)
      assert((await page.locator('.release-entry-heading .pill').allTextContents()).every(value => value === linkedEntry.category))
      await page.getByRole('searchbox', { name: '搜索更新', exact: true }).fill(linkedEntry.commit)
      await expect(page.locator('.release-entries li')).toHaveCount(1)
      await expect(page.getByRole('link', { name: `查看提交 ${linkedEntry.commit}` })).toHaveAttribute('href', `https://github.com/eleost04/miyohub/commit/${linkedEntry.commit}`)
    } else {
      await expect(page.getByText('此版本为功能基线，后续改动将逐项记录。', { exact: true })).toBeVisible()
      await expect(page.getByRole('link', { name: /查看提交/ })).toHaveCount(0)
    }
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
    await page.screenshot({ path: testInfo.outputPath(`changelog-${width}.png`), fullPage: true })
    await page.goBack()
    await expect(page.getByRole('heading', { name: '任务总览', exact: true })).toBeVisible()
  } finally { await context.close() }
})

import assert from 'node:assert/strict'
import { readdir, readFile } from 'node:fs/promises'
import { gunzipSync, brotliDecompressSync } from 'node:zlib'
import { expect, test } from './api-test.mjs'
import { dismissGuide } from './ui.mjs'

test('预压缩资源与最终构建一致，静态缓存不影响私人 API', async ({ request }) => {
  const root = new URL('../dist/assets/', import.meta.url)
  const files = await readdir(root)
  const compressed = files.filter(name => /\.(br|gz)$/.test(name))
  assert(compressed.length > 0, 'build did not generate compressed resources')
  for (const name of compressed) {
    const encoded = await readFile(new URL(name, root))
    const original = await readFile(new URL(name.replace(/\.(br|gz)$/, ''), root))
    const decoded = name.endsWith('.br') ? brotliDecompressSync(encoded) : gunzipSync(encoded)
    assert.deepEqual(decoded, original, name + ' was compressed before final bundle transforms')
  }
  const origin = 'http://127.0.0.1:5897'
  const script = files.find(name => /^index-.*\.js$/.test(name))
  const plain = await request.get(origin + '/assets/' + script, { headers: { 'Accept-Encoding': 'identity' } })
  assert.equal(plain.status(), 200)
  assert.equal(plain.headers()['cache-control'], 'public, max-age=31536000, immutable')
  for (const encoding of ['gzip', 'br']) {
    const response = await request.get(origin + '/assets/' + script, { headers: { 'Accept-Encoding': encoding } })
    assert.equal(response.headers()['content-encoding'], encoding)
    assert.equal(response.headers().vary, 'Accept-Encoding')
    assert.deepEqual(await response.body(), await plain.body(), encoding + ' browser decoding changed the script')
    const cached = await request.get(origin + '/assets/' + script, { headers: { 'Accept-Encoding': encoding, 'If-None-Match': response.headers().etag } })
    assert.equal(cached.status(), 304)
  }
  const index = await request.get(origin)
  assert.equal(index.headers()['cache-control'], 'no-cache')
  const html = await index.text()
  for (const [name, type] of [['favicon.svg', 'image/svg+xml'], ['favicon.ico', 'image/'], ['apple-touch-icon.png', 'image/png']]) {
    assert(html.includes('/' + name), 'missing icon link: ' + name)
    const icon = await request.get(origin + '/' + name)
    assert.equal(icon.status(), 200)
    assert(icon.headers()['content-type'].startsWith(type), name + ' must be served as an image')
    assert.equal(icon.headers()['cache-control'], 'no-cache')
    assert((await icon.body()).length > 100)
  }
  const bootstrap = await request.get(origin + '/api/v1/bootstrap')
  assert.equal(bootstrap.headers()['cache-control'], 'no-store')
  assert.equal((await bootstrap.json()).data.user, null)
})

test('首屏只加载工作台资源，入口压缩体积保持预算内', async ({ browser }) => {
  const root = new URL('../dist/assets/', import.meta.url)
  const files = await readdir(root)
  for (const [type, budget] of [['js', 50 * 1024], ['css', 16 * 1024]]) {
    const name = files.find(name => new RegExp('^index-.*\\.' + type + '\\.br$').test(name))
    assert(name, 'missing Brotli entry ' + type)
    const bytes = (await readFile(new URL(name, root))).length
    assert(bytes <= budget, `first-load ${type}: ${bytes} bytes exceeds ${budget}-byte budget`)
  }
  const origin = 'http://127.0.0.1:5897'
  const context = await browser.newContext({ viewport: { width: 390, height: 844 } })
  try {
    const status = (await (await context.request.get(origin + '/api/v1/auth/status')).json()).data
    const login = await context.request.post(origin + (status.has_admin ? '/api/v1/auth/login' : '/api/v1/auth/setup'), {
      headers: { 'Content-Type': 'application/json', 'X-MiyoHub-Request': '1' },
      data: { username: 'smoke-admin', password: 'local-test-only-123' },
    })
    assert(login.ok())
    await dismissGuide(context)
    const external = [], requests = []
    await context.route('**/*', route => {
      if (new URL(route.request().url()).origin !== origin) { external.push(route.request().url()); return route.abort() }
      return route.continue()
    })
    const page = await context.newPage()
    page.on('request', request => requests.push(new URL(request.url()).pathname))
    await page.goto(origin)
    await expect(page.getByRole('heading', { name: '执行签到任务', exact: true })).toBeVisible()
    await page.waitForLoadState('networkidle')
    assert.deepEqual(external, [], 'first load must not contact QR, captcha, font or image services')
    assert.equal(requests.filter(path => path === '/api/v1/bootstrap').length, 1)
    const deferred = /\/(BindAccountModal|AccountTasksModal|RunTasksModal|ShopPanel|PushPanel|CaptchaPanel|SettingsPanel|AdminPanel|OnboardingGuide|HumanCaptcha|ChangelogPanel|browser)-/
    assert.deepEqual(requests.filter(path => deferred.test(path)), [], 'an unopened page or modal was loaded eagerly')
  } finally { await context.close() }
})

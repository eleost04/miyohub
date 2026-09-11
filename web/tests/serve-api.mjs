// A fresh synthetic API state for browser tests. Never open repository data/.
import { mkdtemp, rm } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawn } from 'node:child_process'

const repo = fileURLToPath(new URL('../../', import.meta.url))
const temporary = await mkdtemp(path.join(os.tmpdir(), 'miyohub-browser-api-'))
const binary = path.join(temporary, process.platform === 'win32' ? 'miyohub.exe' : 'miyohub')
let child, stopping = false
async function stop(signal = 'SIGTERM') {
  if (stopping) return
  stopping = true
  if (child && child.exitCode === null && child.signalCode === null) {
    const closed = new Promise(resolve => child.once('close', resolve))
    child.kill(signal)
    const timeout = setTimeout(() => child.kill('SIGKILL'), 8000)
    await closed
    clearTimeout(timeout)
  }
  await rm(temporary, { recursive: true, force: true })
}
for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => void stop(signal))
try {
  child = spawn('go', ['build', '-o', binary, './cmd/miyohub'], { cwd: repo, stdio: 'inherit' })
  const built = await new Promise((resolve, reject) => { child.once('error', reject); child.once('close', resolve) })
  if (built !== 0) throw new Error('隔离 API 构建失败')
  if (!stopping) {
    child = spawn(binary, ['serve', '--host', '127.0.0.1', '--port', '5897', '--data-dir', path.join(temporary, 'data'), '--web-dir', path.join(repo, 'web/dist')], {
      cwd: repo, stdio: 'inherit', env: { ...process.env, MIYOHUB_PUBLIC_ORIGIN: '', MIYOHUB_SECURE_COOKIE: 'false' },
    })
    const result = await new Promise((resolve, reject) => { child.once('error', reject); child.once('close', resolve) })
    if (!stopping && result !== 0) throw new Error('隔离 API 异常退出')
  }
} catch (error) {
  if (!stopping) { console.error(error.message); process.exitCode = 1 }
} finally { await stop() }

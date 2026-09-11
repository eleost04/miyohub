// Each real-API test gets its own process, encrypted store and rate counters.
// Production authorization/rate limits remain enabled and unchanged.
import { spawn } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { test as base } from '@playwright/test'
export { expect } from '@playwright/test'

export const test = base.extend({
  isolatedAPI: [async ({}, use) => {
    const child = spawn(process.execPath, [fileURLToPath(new URL('./serve-api.mjs', import.meta.url))], { stdio: ['ignore', 'pipe', 'pipe'] })
    const closed = new Promise(resolve => child.once('close', resolve))
    let output = '', readyTimer
    const ready = new Promise((resolve, reject) => {
      readyTimer = setTimeout(() => reject(new Error('Isolated API startup timed out: ' + output)), 25_000)
      const read = chunk => {
        output = (output + chunk).slice(-12_000)
        if (output.includes('http://127.0.0.1:5897')) resolve()
      }
      child.stdout.on('data', read)
      child.stderr.on('data', read)
      child.once('error', reject)
      child.once('close', code => reject(new Error('Isolated API exited before startup: ' + code + '\n' + output)))
    })
    try {
      await ready
      clearTimeout(readyTimer)
      await use()
    } finally {
      clearTimeout(readyTimer)
      child.kill('SIGTERM')
      const force = setTimeout(() => child.kill('SIGKILL'), 12_000)
      await closed
      clearTimeout(force)
    }
  }, { auto: true, timeout: 40_000 }],
})

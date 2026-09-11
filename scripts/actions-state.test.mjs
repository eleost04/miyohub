import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp, readFile, readdir, rm, stat } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { restoreState } from './actions-state.mjs'

async function fixture(t) {
  const root = await mkdtemp(path.join(os.tmpdir(), 'miyohub-actions-test-'))
  t.after(() => rm(root, { recursive: true, force: true }))
  const state = Buffer.from(JSON.stringify({ format: 'miyohub-state-v1', ciphertext: 'mock-ciphertext' }))
  return { root, state, env: { RUNNER_TEMP: root, GITHUB_ENV: path.join(root, 'github-env'), MIYOHUB_STATE_B64: state.toString('base64'), MIYOHUB_KEY_B64: Buffer.alloc(32, 7).toString('base64') } }
}

test('restores isolated private files without exposing secrets in environment output', async t => {
  const { root, env, state } = await fixture(t)
  const first = await restoreState(env)
  const second = await restoreState(env)
  assert.notEqual(first, second)
  assert.equal(path.dirname(first), root)
  assert.deepEqual(await readFile(path.join(first, 'state.json')), state)
  assert.deepEqual(await readFile(path.join(first, 'state.json.key')), Buffer.alloc(32, 7))
  if (process.platform !== 'win32') {
    assert.equal((await stat(first)).mode & 0o777, 0o700)
    assert.equal((await stat(path.join(first, 'state.json.key'))).mode & 0o777, 0o600)
  }
  const output = await readFile(env.GITHUB_ENV, 'utf8')
  assert.equal(output, 'MIYOHUB_DATA_DIR=' + first + '\nMIYOHUB_DATA_DIR=' + second + '\n')
  assert.ok(!output.includes(env.MIYOHUB_KEY_B64))
})

test('invalid or plaintext secrets cannot create state', async t => {
  const { root, env } = await fixture(t)
  for (const patch of [
    { MIYOHUB_KEY_B64: '' }, { MIYOHUB_STATE_B64: '#bad' },
    { MIYOHUB_KEY_B64: Buffer.alloc(31).toString('base64') },
    { MIYOHUB_STATE_B64: Buffer.from('{"config":{}}').toString('base64') },
    { MIYOHUB_STATE_B64: Buffer.from('not-json').toString('base64') },
    { RUNNER_TEMP: root + '\nINJECTED=yes' },
  ]) await assert.rejects(() => restoreState({ ...env, ...patch }))
  assert.deepEqual(await readdir(root), [])
})

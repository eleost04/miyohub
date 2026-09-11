// Restore only explicitly configured secrets into a private, fresh Actions dir.
import { appendFile, chmod, mkdtemp, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { pathToFileURL } from 'node:url'

function decodeSecret(value, limit) {
  if (typeof value !== 'string' || !value || value.length > limit * 2) throw new Error('签到 Secret 缺失或超过大小限制')
  const normalized = value.replace(/\s/g, '')
  if (!/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(normalized)) throw new Error('签到 Secret 必须为标准 Base64')
  const data = Buffer.from(normalized, 'base64')
  if (!data.length || data.length > limit) throw new Error('签到 Secret 大小无效')
  return data
}

export async function restoreState(env) {
  if (!env.RUNNER_TEMP || !env.GITHUB_ENV || /[\r\n]/.test(env.RUNNER_TEMP)) throw new Error('缺少安全的 Actions 临时目录')
  const state = decodeSecret(env.MIYOHUB_STATE_B64, 8 * 1024 * 1024)
  const key = decodeSecret(env.MIYOHUB_KEY_B64, 32)
  if (key.length !== 32) throw new Error('状态密钥长度必须为 32 字节')
  let envelope
  try { envelope = JSON.parse(state.toString('utf8')) } catch { throw new Error('状态 Secret 不是有效 JSON') }
  if (envelope?.format !== 'miyohub-state-v1' || typeof envelope.ciphertext !== 'string' || !envelope.ciphertext) throw new Error('仅接受 MiyoHub 加密状态，不能使用旧版 YAML 或明文凭据')
  const directory = await mkdtemp(path.join(path.resolve(env.RUNNER_TEMP), 'miyohub-checkin-'))
  await chmod(directory, 0o700)
  await writeFile(path.join(directory, 'state.json'), state, { mode: 0o600, flag: 'wx' })
  await writeFile(path.join(directory, 'state.json.key'), key, { mode: 0o600, flag: 'wx' })
  await appendFile(env.GITHUB_ENV, 'MIYOHUB_DATA_DIR=' + directory + '\n')
  return directory
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try { await restoreState(process.env); console.log('临时加密状态已就绪；不上传任何状态、密钥或运行日志。') }
  catch { console.error('准备签到状态失败，请检查 Actions Secrets 与临时目录；敏感内容不会输出。'); process.exitCode = 1 }
}

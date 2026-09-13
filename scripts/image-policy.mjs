import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { appendFileSync, readFileSync } from 'node:fs'
import { pathToFileURL } from 'node:url'

export function imagePublicationPlan(context, repository, commit, existingPackage) {
  assert(['push', 'workflow_dispatch'].includes(context.event), 'Images are never published from pull requests.')
  assert.equal(context.ref, 'refs/heads/develop', 'Only develop can publish the development image.')
  assert.match(context.repository, /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/)
  assert.match(context.sha, /^[a-f0-9]{40}$/)
  assert.match(context.version, /^\d+\.\d+\.\d+(?:-(?:beta|rc)\.[1-9]\d*)?$/)
  assert.equal(repository.full_name?.toLowerCase(), context.repository.toLowerCase(), 'Repository identity mismatch.')
  assert.equal(repository.private, true, 'This publishing policy requires a private repository.')
  assert.equal(commit.sha, context.sha, 'Commit identity mismatch.')
  assert.equal(commit.commit?.verification?.verified, true, 'Only a verified signed commit can be published.')
  if (existingPackage) assert.equal(existingPackage.visibility, 'private', 'Refusing to publish to a non-private package.')
  const image = 'ghcr.io/' + context.repository.toLowerCase()
  return { image, sha_tag: image + ':sha-' + context.sha, version: context.version }
}

function githubMetadata(path, allowMissing = false) {
  const result = spawnSync('gh', ['api', '--include', path], { encoding: 'utf8', maxBuffer: 4 * 1024 * 1024 })
  const status = Number(result.stdout?.match(/^HTTP\/\S+\s+(\d{3})/m)?.[1] || 0)
  if (allowMissing && status === 404) return null // New GHCR packages start private.
  assert(result.status === 0 && status === 200, `GitHub metadata check failed (HTTP ${status || 'unavailable'}); publication stopped.`)
  const parts = result.stdout.split(/\r?\n\r?\n/)
  return JSON.parse(parts.slice(1).join('\n\n'))
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    const context = { event: process.env.GITHUB_EVENT_NAME, ref: process.env.GITHUB_REF, repository: process.env.GITHUB_REPOSITORY, sha: process.env.GITHUB_SHA, version: readFileSync(new URL('../internal/buildinfo/VERSION', import.meta.url), 'utf8').trim() }
    assert.match(context.repository || '', /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/)
    assert.match(context.sha || '', /^[a-f0-9]{40}$/)
    const [owner, name] = context.repository.split('/')
    const repository = githubMetadata('repos/' + context.repository)
    const commit = githubMetadata('repos/' + context.repository + '/commits/' + context.sha)
    const existing = githubMetadata(`users/${owner}/packages/container/${encodeURIComponent(name.toLowerCase())}`, !process.argv.includes('--require-package'))
    const plan = imagePublicationPlan(context, repository, commit, existing)
    if (process.env.GITHUB_OUTPUT) appendFileSync(process.env.GITHUB_OUTPUT, Object.entries(plan).map(([key, value]) => `${key}=${value}\n`).join(''))
    console.log(`Private image policy verified: ${plan.image}, signed revision ${context.sha}.`)
  } catch (error) {
    console.error(error.message)
    process.exitCode = 1
  }
}

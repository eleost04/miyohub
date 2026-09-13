import { test } from 'node:test'
import assert from 'node:assert/strict'
import { imagePublicationPlan } from './image-policy.mjs'

const context = { event: 'push', ref: 'refs/heads/develop', repository: 'Example/Project', sha: 'a'.repeat(40), version: '0.1.0-beta.2' }
const repository = { full_name: 'Example/Project', private: true }
const commit = { sha: context.sha, commit: { verification: { verified: true } } }

test('development images have a private signed source and traceable tags', () => {
  const plan = imagePublicationPlan(context, repository, commit, { visibility: 'private' })
  assert.equal(plan.image, 'ghcr.io/example/project')
  assert.equal(plan.sha_tag, plan.image + ':sha-' + context.sha)
  assert.equal(plan.version, context.version)
  assert.deepEqual(imagePublicationPlan(context, repository, commit, null), plan)
})

test('PRs, wrong refs, unsigned commits, public packages and injected metadata are rejected', () => {
  for (const patch of [{ event: 'pull_request' }, { event: 'pull_request_target' }, { ref: 'refs/heads/main' }, { ref: 'refs/heads/feat/topic' }, { sha: '$(bad)' }, { repository: 'Example/Project\nimage=injected' }, { version: 'dev\nnext=value' }]) assert.throws(() => imagePublicationPlan({ ...context, ...patch }, repository, commit, null))
  for (const patch of [{ private: false }, { full_name: 'Another/Project' }]) assert.throws(() => imagePublicationPlan(context, { ...repository, ...patch }, commit, null))
  for (const invalid of [{ ...commit, sha: 'b'.repeat(40) }, { ...commit, commit: { verification: { verified: false } } }]) assert.throws(() => imagePublicationPlan(context, repository, invalid, null))
  for (const visibility of ['public', 'internal', undefined]) assert.throws(() => imagePublicationPlan(context, repository, commit, { visibility }))
})

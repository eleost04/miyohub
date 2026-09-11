import { test } from 'node:test'
import assert from 'node:assert/strict'
import { needsCodeVerification, verificationScope } from './ci-scope.mjs'

test('only explicit documentation paths skip heavy verification', () => {
  assert.equal(needsCodeVerification(['README.md', 'README.en.md', 'ROADMAP.md', '.github/SECURITY.md', '.github/ISSUE_TEMPLATE/bug_report.yml']), false)
  for (const path of ['internal/tasks/bbs_checkin.go', 'web/src/releases.json', 'web/src/App.vue', '.github/workflows/ci.yml', '.gitleaks.toml', '.gitignore', 'Dockerfile', 'compose.yaml', 'scripts/ci-scope.mjs', 'new-unknown-file']) {
    assert.equal(needsCodeVerification(['README.md', path]), true, path)
  }
})

test('manual, new branch and unavailable-base runs fail closed to full checks', () => {
  const base = 'a'.repeat(40)
  const docs = () => ['README.md']
  assert.equal(verificationScope('push', base, docs), false)
  assert.equal(verificationScope('pull_request', base, () => ['web/src/App.vue']), true)
  assert.equal(verificationScope('workflow_dispatch', base, docs), true)
  for (const invalid of ['', '0'.repeat(40), 'refs/heads/main', '--help', '$(invalid)']) assert.equal(verificationScope('push', invalid, docs), true)
  assert.equal(verificationScope('push', base, () => { throw new Error('missing base') }), true)
})

import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'

const root = new URL('../', import.meta.url)
const version = (await readFile(new URL('internal/buildinfo/VERSION', root), 'utf8')).trim()
assert.match(version, /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-(beta|rc)\.[1-9]\d*)?$/)
const readJSON = async name => JSON.parse(await readFile(new URL(name, root), 'utf8'))
assert.equal((await readJSON('web/package.json')).version, version, 'package.json version differs')
const lock = await readJSON('web/package-lock.json')
assert.equal(lock.version, version, 'package-lock version differs')
assert.equal(lock.packages[''].version, version, 'package-lock root differs')
const releases = await readJSON('web/src/releases.json')
assert.equal(releases[0]?.version, version, 'current changelog version differs')
for (const release of releases) {
  const commits = new Set()
  for (const entry of release.entries) {
    assert.match(entry.commit, /^[a-f0-9]{7,40}$/)
    assert(entry.title && entry.description && entry.category)
    assert(!commits.has(entry.commit), 'duplicate changelog commit')
    commits.add(entry.commit)
  }
}
const tag = process.argv[2] || (process.env.GITHUB_REF_TYPE === 'tag' ? process.env.GITHUB_REF_NAME : '')
if (tag) assert.equal(tag, 'v' + version, 'release tag differs from bundled version')
console.log(`Version ${version}: Go, web and package lock agree${tag ? ' with ' + tag : ''}.`)

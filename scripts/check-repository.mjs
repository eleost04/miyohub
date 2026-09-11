import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('../', import.meta.url))
const files = execFileSync('git', ['ls-files', '-z'], { cwd: root, encoding: 'utf8' }).split('\0').filter(Boolean)
const excludedDirectory = /(^|\/)(docs|data|backup|backups|logs|models|upstream|node_modules|dist|build|bin|test-results|playwright-report|coverage|secrets)(\/|$)/
const privateFile = /(^|\/)(\.env(?:\..*)?|state\.json(?:\..*)?|\.miyohub\.lock|credentials[^/]*\.json)$|\.(key|pem|p12|pfx|sqlite|db|onnx|pth|tar|zip|7z|log)$|\.tar\.gz$/
const forbidden = files.filter(file => excludedDirectory.test(file) || privateFile.test(file) && !file.endsWith('/.env.example') && file !== '.env.example')
assert.deepEqual(forbidden, [], 'generated files or private runtime data are tracked')
console.log(`Repository boundary: ${files.length} core files; no forbidden runtime paths.`)

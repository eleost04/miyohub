import assert from 'node:assert/strict'
import { readFile, writeFile } from 'node:fs/promises'

const releases = JSON.parse(await readFile(new URL('../web/src/releases.json', import.meta.url), 'utf8'))
const version = process.argv[2]
const release = releases.find(item => item.version === version)
assert(release, 'Release notes are missing for ' + version)
const lines = [`# MiyoHub ${version}`, '', release.summary, '', '## 更新 / Changes', '']
for (const entry of release.entries) lines.push(`- ${entry.title}：${entry.description} ([${entry.commit}](https://github.com/eleost04/miyohub/commit/${entry.commit}))`)
lines.push('', '## 部署 / Deployment', '', 'Linux archives include the Go binary, compiled web assets and bilingual README. Run from the extracted directory and initialize the administrator on localhost before exposing HTTPS.', '', '备份加密状态及对应密钥后再升级。网络结果不确定的兑换不会自动重放，请在米游社核对。', '', 'Private development images are published separately by opted-in develop CI; see README for pull permissions, tags and digest pinning. This release workflow does not upload captcha models or deploy containers.', '')
if (process.argv[3]) await writeFile(process.argv[3], lines.join('\n'))
else process.stdout.write(lines.join('\n'))

import { execFileSync } from 'node:child_process'
import { appendFileSync } from 'node:fs'
import { pathToFileURL } from 'node:url'

// Keep privacy/repository checks for documentation changes too. Only skip
// builds and browsers for an explicit documentation-only allowlist.
export function needsCodeVerification(files) {
  return files.some(file => !(/\.md$/i.test(file) || /^\.github\/ISSUE_TEMPLATE\/[^/]+\.ya?ml$/.test(file)))
}

export function verificationScope(event, base, changedFiles) {
  if (event === 'workflow_dispatch' || !/^[a-f0-9]{40}$/.test(base || '') || /^0+$/.test(base)) return true
  try { return needsCodeVerification(changedFiles(base)) }
  catch { return true } // A missing base must never silently skip validation.
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const code = verificationScope(process.env.CI_EVENT, process.env.CI_BASE, base =>
    execFileSync('git', ['diff', '--name-only', '-z', base, 'HEAD', '--'], { encoding: 'utf8' }).split('\0').filter(Boolean))
  if (process.env.GITHUB_OUTPUT) appendFileSync(process.env.GITHUB_OUTPUT, `code=${code}\n`)
  console.log(code ? 'Code/configuration changed: run the full verification.' : 'Documentation only: repository and secret checks remain enabled.')
}

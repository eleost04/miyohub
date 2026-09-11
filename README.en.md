# MiyoHub

[Feature board](ROADMAP.md) · [Contributing and maintenance](CONTRIBUTING.md) · [Issues](https://github.com/eleost04/miyohub/issues)

[简体中文](README.md) · Version **0.0.1**

Self-hosted MiYouShe check-ins, community coin tasks and merchandise exchange management, built with Go and Vue 3. This is not an official HoYoverse product. Upstream APIs, account challenges and stock can change; task rewards and successful exchanges are not guaranteed.

## Features

- QR, SMS and Cookie account binding; check-ins for Genshin Impact, Honkai: Star Rail, Zenless Zone Zero, Honkai Impact 3rd, Tears of Themis and Houkai Gakuen 2; cloud-game time and community coin tasks.
- Independent game, role-exclusion, community and daily-time settings for each bound account. Personal schedules take precedence over the site's fallback time.
- Physical / virtual catalogs, address and role selection, immediate or scheduled exchanges, bounded retries and execution history.
- Personal captcha providers or authorized site providers, asynchronous anonymous probes, and manual interactive SMS verification.
- PushPlus, Telegram, WxPusher, DingTalk, Feishu, official QQ bots, QQ OneBot, SMTP, WeChat iLink and webhooks. Official QQ and WeChat support QR binding.
- Responsive selectors, layered browser Back, preference autosave, contextual mobile save buttons, dismissible onboarding, SVG icons and commit-linked release notes.

A site user is the ownership boundary. Administrators cannot browse another user's game accounts, private logs or reservations in the task workspace. System settings and user administration are separate.

## Quick start

Requires Docker Engine and Compose v2.

```bash
git clone https://github.com/eleost04/miyohub.git
cd miyohub
cp .env.example .env
docker compose up -d --build
docker compose ps
curl -fsS http://127.0.0.1:5890/api/v1/health
```

The default port binds to loopback. Create the administrator locally before publishing the service; do not leave first-time setup exposed. Use an SSH tunnel for initial setup on a remote host.

The container is named `miyohub`, the persistent volume is `miyohub_miyohub-data`, and the network is `miyohub_default`. The process is non-root with a read-only root filesystem. **Do not use `docker compose down -v` to upgrade**: it deletes the account-data volume.

The optional [miyohub-captcha](https://github.com/eleost04/miyohub-captcha) companion provides a local CPU solver. The main application can run without it.

## HTTPS and Caddy

Set the actual external origin in the untracked `.env`:

```dotenv
MIYOHUB_PUBLIC_ORIGIN=https://miyohub.example.com
MIYOHUB_SECURE_COOKIE=true
```

Example for Caddy running on the host:

```caddyfile
miyohub.example.com {
    encode zstd gzip
    reverse_proxy 127.0.0.1:5890
}
```

Replace the domain. The configured origin must exactly match the browser's scheme, host and port, without a path. Recreate the container after environment changes: `docker compose up -d --force-recreate`. Preserve the external Host, as Caddy does by default. Do not disable cross-site request protection or add wildcard CORS to fix an origin mismatch. Containerized Caddy must reach `miyohub:5890` on a shared network; its own loopback is not the host.

## Accounts, invitations and schedules

Onboarding can be skipped or closed and reopened from the profile. Bind an account, then open its task settings to select games, cloud tasks, community tasks, timezone and daily time. Cloud games require their own `x-rpc-combo_token`.

The site-wide task switch pauses all check-ins. Disabling only the fallback scheduler does not disable personal times. Personal schedules are checked every ten seconds; downtime is not caught up on restart. Automatic runs have durable per-account daily deduplication; manual runs remain possible. A displayed schedule is not a promise of exact-second execution.

A network failure when reading coin-task state is retried at most twice, without replaying mutations. If the upstream reports zero remaining daily rewards, the run checks state without doing tasks again. Previously earned coins are not new rewards, and this situation does not imply a captcha configuration problem.

Administrators generate random invitation codes with independent exchange and site-captcha grants. New invitations default to one use, seven-day expiry, both grants enabled and site captcha selected; these presets are editable. Ordinary registration without an invitation does not receive those grants. Legacy invitations are not retroactively elevated. Expiry, revocation, concurrent redemption, usage limits and the creator's current administrator status are checked server-side.

Each user has a stable site user ID, distinct from a MiYouShe UID. Automatic notifications are off until the user enables them.

## Captcha providers and SMS

Users choose off, personal providers or an authorized site service.

- Personal Damagou keys and custom public endpoints do not require a site-service grant. Failure does not silently consume site-provider credit.
- Administrators configure shared providers in system settings and grant access in user administration or invitations. Personal providers cannot reach the server's private network.
- For the companion on the shared Docker network, configure `http://miyohub-captcha:9645/pass_nine` with a recommended 60-second timeout. Only host-side programs use `127.0.0.1:9645`.

A custom endpoint accepts GET parameters `gt`, `challenge`, `use_v3_model` and returns:

```json
{"data":{"result":"success","validate":"...","challenge":"optional-updated-challenge"}}
```

Optional Bearer authentication is supported; never put credentials in the URL. Providers receive challenge parameters, not account Cookies or STokens. Personal endpoints reject private, loopback and metadata addresses, redirects and environment proxies, with DNS revalidation at connection time.

Anonymous probes run in the background, survive browser disconnection, and retain a redacted result. They are limited to once per user per minute and approximately 65 seconds. Polling does not call the provider again. Probes do not use Damagou or run account tasks, although a custom provider may charge for requests. Producing validation parameters is not proof of acceptance for a real account.

SMS login supports manual Geetest 3 and 4 challenges during both sending and code verification, using an isolated, lazily loaded page. Geetest 3 may also use a configured automatic provider; V4 challenges are never sent to V3-only services. SMS sessions last ten minutes and interactive challenges at most two. Restarting requires a new session. Failed sends and cooldowns are not reported as successful sends. Live risk-control compatibility still requires the account owner's acceptance.

## Exchange timing and retries

BBS mission-state reads retry up to five times by default (six requests in total). Administrators can set 0–10 retries under System settings → Connection and retries. Transient network/HTTP failures use 1/2/4/8-second backoff and honor `Retry-After`; a requested wait over one minute ends the current check. Authentication and captcha failures are not network retries. SMS, sign-in, likes and uncertain exchanges are never blindly replayed. Logs include actual retries and cancellation reasons.

Reservations prepare approximately 180 seconds ahead. Different products can prepare together; actual requests for one account are serialized. Clock synchronization prefers the goods API's `now_time`, falls back to HTTP `Date`, and refreshes before opening. These timestamps are typically second-resolution; the UI reports their source and estimated uncertainty. Keep host NTP synchronized too. Network latency and stock competition cannot be eliminated.

A booked sale round is distinguished from a later restock advertised by the upstream. Genuine postponements, sold-out stock and newly created reservations are validated separately; plans are not silently moved to another week.

Temporary explicit rejections may retry within the configured window: at most 60 requests, minimum 0.2-second interval, and at least two seconds of backoff for rate-limit responses. A zero window sends only once. Insufficient balance, purchase limits, stock exhaustion, expired authentication and invalid parameters stop the run. Timeouts, connection loss and malformed responses are **uncertain outcomes and are never automatically replayed**. Check MiYouShe exchange records first to avoid duplicate spending. Expiry prevents new attempts but allows an in-flight result to finish. Notifications contain the final summary; logs retain attempts and return codes.

## Notifications and interaction

Every user controls automatic delivery, task/exchange categories, error-only filtering and private channels. Only results belonging to that user's bound accounts are sent. Choose a delivery method to configure it directly in a dialog. Enabling a channel does not enable the master automatic-delivery switch. A manual test sends immediately.

“Accepted by the notification service” means the provider acknowledged the request, not that a device received or read it. Official QQ setup happens on the official QR page. After WeChat binding, send the bot a message to establish a session. Provider permissions, session expiry and quotas still apply.

Preferences autosave about one second after editing stops, with a browser-local opt-out. Secrets are not stored in browser storage. Credentials, passwords, permissions, new channels and exchange plans require explicit confirmation. The mobile floating save action appears only for unsaved edits without another visible save action. Back closes selectors, dialogs and details before navigating away.

## Performance and resources

Normal UI assets are small SVGs; ICO/iOS PNG files exist only for compatibility. Pages, editors, QR generation and human-verification scripts load on demand. Brotli/gzip are generated at build time. Hashed assets are immutable for one year, HTML revalidates, and private APIs are not cached. Product images use restricted official-CDN 302 redirects without downloading images through the application server; this cannot fix an unreachable CDN.

Reference footprint from a Linux/amd64 build on 2026-09-11. Image sizes are uncompressed; runtime memory depends on workload:

| Service | Image | Example idle RAM | Suggested allocation |
| --- | ---: | ---: | --- |
| MiyoHub | 18.6 MiB | approximately 14 MiB just after startup | 1 CPU, 256 MiB RAM |
| Optional captcha service | 364.1 MiB plus about 177 MiB of models | approximately 352 MiB after startup; earlier running sample 535 MiB | 2 CPUs, 1.5 GiB RAM |

For both services, allow at least 2 CPUs, 2 GiB RAM and over 3 GiB free disk. Local builds should have 4 GiB RAM and at least 5 GiB free disk, excluding growing caches/backups/logs. Idle measurements are not peak-capacity guarantees. The solver uses one worker; increasing workers is not supported.

Releases currently provide source and Linux archives, **not automatically published Docker images**. Any image distribution should use explicit version and rollback tags. Check usage and redistribution rights separately for the solver's third-party runtime code and model weights.

## Upgrade, backup and logs

```bash
docker compose build
docker compose up -d --no-build
docker compose logs --tail 100
docker compose exec miyohub /usr/local/bin/miyohub logs --component bbs --tail 100
```

Avoid active work or imminent reservations during upgrades. Back up `state.json` **together with `state.json.key`**, retaining mode 600 and a matching pair, preferably while the service is briefly stopped. Keep the previous image for rollback. Never upload backups, keys, account screenshots or challenge images. Read-only `logs` may run alongside the server; other CLI commands sharing a data directory require stopping the server. Do not remove or replay historical reservations without authorization.

## Development and releases

Use the Go version from `go.mod` and Node.js 22/npm.

```bash
npm --prefix web ci
npm --prefix web run build
go test -race ./...
go vet ./...
node --test scripts/actions-state.test.mjs
node scripts/check-version.mjs
node scripts/check-repository.mjs
cd web
npx playwright install --with-deps chromium
npm run test:e2e
```

Real-API browser scenarios each use their own process, temporary encrypted state and normal rate limits. Ports 4177 and 5897 must be available. Tests do not read production accounts or send real SMS, check-in, exchange or notification requests. Development servers: `npm --prefix web run dev` and `go run ./cmd/miyohub`.

See [CONTRIBUTING.md](CONTRIBUTING.md) for focused changes, `fix` / `feat` commits, GPG signatures, branches, version increments and release gates. [ROADMAP.md](ROADMAP.md) tracks capabilities, known issues and comparison with reference projects. User-facing changes appear in the in-app changelog.

Release automation verifies signed tags and branch ancestry, then builds Linux amd64/arm64 archives with frontend assets. Arm64 is cross-compiled but not hardware-validated. Test and release workflows support manual recovery without skipping checks or overwriting existing releases.

Versioned content is limited to core source, necessary tests, dependency locks, build/CI configuration and bilingual README files. Exclusions include real `.env*` files, data/state/keys, `docs/`, backups, logs, dependencies, build products, browser reports and archives. The companion also excludes upstream checkouts, model weights and challenge images. History secret scanning supplements, not replaces, review before committing.

## References

[MiyoQian](https://github.com/ytf211/MiyoQian), [MiyoSign](https://github.com/ytf211/miyosign) and [MihoyoBBSTools](https://github.com/Womsxd/MihoyoBBSTools) inform API compatibility and feature design. Captcha references include [this discussion](https://github.com/Womsxd/MihoyoBBSTools/issues/198) and [test_nine](https://github.com/luguoyixiazi/test_nine).

This project is not affiliated with miHoYo or the projects above. Third-party code, models and services remain subject to their respective terms; referencing a project does not grant additional rights.

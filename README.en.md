<div align="center">

  <img src="web/public/apple-touch-icon.png" width="100" height="100" alt="MiyoHub Logo" style="border-radius: 20px;">

  # MiyoHub

  **Lightweight, self-hosted management system for miHoYo daily check-ins, HoYo BBS tasks, and scheduled merchandise exchange.**

  [![Release](https://img.shields.io/github/v/release/eleost04/miyohub?color=blue&logo=github)](https://github.com/eleost04/miyohub/releases)
  [![Go Version](https://img.shields.io/badge/Go-1.27+-00ADD8?logo=go&logoColor=white)](go.mod)
  [![Vue 3](https://img.shields.io/badge/Vue-3.x-4FC08D?logo=vuedotjs&logoColor=white)](web/)
  [![Docker](https://img.shields.io/badge/Docker-Hub-2496ED?logo=docker&logoColor=white)](https://hub.docker.com/r/eleost/miyohub)
  [![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

  [简体中文](README.md) · [English](README.en.md) · [Roadmap](ROADMAP.md) · [Contributing](CONTRIBUTING.md) · [Issues](https://github.com/eleost04/miyohub/issues)

</div>

---

## 🌟 Key Features

- 🎮 **Multi-Game Check-ins**: Out-of-the-box support for *Genshin Impact*, *Honkai: Star Rail*, *Zenless Zone Zero*, *Honkai Impact 3rd*, *Tears of Themis*, and *Houkai Gakuen 2*, plus Cloud Genshin/ZZZ daily durations.
- 🪙 **HoYo BBS Coin Automation**: Automated forum check-ins, browsing, liking, and sharing tasks with customizable task filters and execution schedules.
- ⚡ **Precision Exchange Timing**: Millisecond-level time alignment using upstream HTTP Date, supporting pre-reservations, intelligent window retries, and replay protection.
- 📢 **Omnichannel Notifications**: Supports **Official Mobile QQ Bot QR Login**, **WeChat iLink QR Login**, Telegram, DingTalk, Feishu, ServerChan, Bark, PushPlus, SMTP Email, and Webhooks (10+ channels).
- 🛡️ **Security & Multi-Tenant Isolation**: Local state encrypted at rest, exclusive cross-process directory locking, strict user boundary isolation, and anti-SSRF egress protection.
- 📱 **Modern Responsive UI**: Built with Vue 3 and Tailwind CSS. Finely tuned for desktop, tablet, and mobile narrow viewports.

---

## 🚀 Quick Start

### Option 1: Docker Compose (Recommended)

Deploy in three simple steps:

```bash
# 1. Clone the repository
git clone https://github.com/eleost04/miyohub.git
cd miyohub

# 2. Configure environment variables
cp .env.example .env

# 3. Start the containers
docker compose up -d
```

Once started, navigate to `http://localhost:5890` in your browser to initialize the administrator account.

> 💡 **Production Note**: By default, the service listens on `127.0.0.1:5890`. For public access, it is recommended to set up an HTTPS reverse proxy via Caddy or Nginx.

---

### Option 2: Pre-built Docker Image

You can also run MiyoHub directly with a standalone `docker-compose.yml`:

```yaml
name: miyohub

services:
  miyohub:
    container_name: miyohub
    image: eleost/miyohub:latest
    restart: unless-stopped
    ports:
      - "127.0.0.1:5890:5890"
    volumes:
      - miyohub-data:/data

volumes:
  miyohub-data:
```

Run `docker compose up -d` to launch.

---

## ⚙️ Environment Variables

Adjust key deployment options in `.env`:

| Variable | Default | Description |
| :--- | :--- | :--- |
| `MIYOHUB_HTTP_PORT` | `5890` | Host port mapping. |
| `MIYOHUB_BIND_ADDR` | `127.0.0.1` | Host bind address (keep loopback when behind a reverse proxy). |
| `MIYOHUB_PUBLIC_ORIGIN` | *empty* | Full public origin URL (e.g. `https://miyohub.example.com`). |
| `MIYOHUB_SECURE_COOKIE` | `false` | Enforces Secure flag on session cookies when HTTPS is used. |
| `MIYOHUB_PUSH_ALLOW_PRIVATE` | `false` | Allows webhook/notification relays to connect to private subnets. |

---

## 🧭 Documentation

- Architecture & Constraints: [docs/architecture.md](docs/architecture.md)
- CLI Usage Guide: [docs/cli.md](docs/cli.md)
- Push Notifications & Channels: [docs/push.md](docs/push.md)
- Deployment Guide: [docs/deployment.md](docs/deployment.md)
- Version & Feature Roadmap: [ROADMAP.md](ROADMAP.md)

---

## Development preview: account groups and batch actions

The `develop` branch adds **Dashboard → Accounts → Groups and batch actions**; the published `0.1.0-beta.3` does not include this feature. Filter your own accounts by group and update up to 50 accounts in one transaction: group, automatic check-in, individual schedule, or enabled state. Changing a schedule only affects future scheduling; it neither starts a run nor changes selected games. Individual settings can still be adjusted afterwards. **Run selected accounts** is an explicit action and retains the existing queue and request pacing.

Group labels are local to each site user; an empty label removes membership. Missing, unauthorized, or stale account settings reject the entire batch without partial writes.

## Development preview: encrypted account migration

**Profile → Account migration** transfers your own accounts, groups and task settings between trusted deployments (up to 100 accounts). Export requires your current site password and a separate transfer passphrase of at least 12 characters. Files use PBKDF2-SHA256 (600,000 iterations) and AES-256-GCM. Keep the passphrase separate from the file and import only into a trusted HTTPS instance.

Import shows a five-minute preview before explicit confirmation. Existing UIDs or duplicate names are skipped, never overwritten. New accounts are disabled with automatic tasks turned off until reviewed. Site permissions, sessions, logs, solver/push secrets and redemption plans are excluded. Export does not stop the original deployment: disable its corresponding tasks before switching to avoid duplicate work. This is file migration, not ongoing synchronization; it is not included in the published `0.1.0-beta.3`.

## Development preview: game notes

**Game notes** (under **More** on mobile) reads official stamina, daily progress and expedition data for Genshin Impact, Honkai: Star Rail and Zenless Zone Zero on demand. It does not perform in-game actions. Choose one of your accounts and games, then select a role verified against that account's upstream role list. Integer numbers and numeric strings are normalized; missing numbers and boolean states remain unknown rather than zero or complete. Recovery times are estimates based on the snapshot time.

Notes are cached for 3 minutes and roles for 15 minutes. Requests for an account are serialized with a minimum 3-second gap. Opening a page, global status polling and hidden tabs never poll upstream notes. Verification responses pause that account's record queries for 6 hours in the current process, including after game or credential changes; rate limits respect `Retry-After`. Stale snapshots are labeled explicitly. Queries reuse the configured outbound proxy and existing device identity, without automatic device registration, credential renewal or CAPTCHA solving. Enable records/notes and complete verification in the official client. These safeguards reduce request pressure, but cannot guarantee freedom from upstream restrictions. This feature is not included in published `0.1.0-beta.3`.

## 🤝 Contributing


Contributions, issues, and feature requests are welcome!
Please check the [Contributing Guidelines (CONTRIBUTING.md)](CONTRIBUTING.md) before submitting pull requests.

---

## ⚠️ Disclaimer

1. This project is for personal learning, software engineering practice, and automated testing research only. It does not provide any commercial or bypass services.
2. MiyoHub is an independent open-source project and is not affiliated with, endorsed by, or associated with miHoYo Co., Ltd.
3. Users are responsible for complying with the terms of service and community guidelines of the respective games.
4. The maintainers assume no liability for any account restrictions, penalties, or losses resulting from the use of this software.

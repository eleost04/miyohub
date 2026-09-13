<div align="center">

  <img src="web/public/apple-touch-icon.png" width="100" height="100" alt="MiyoHub Logo" style="border-radius: 20px;">

  # MiyoHub

  **轻量、优雅的米游社多账号签到、米游币日常与定时抢兑自托管系统**

  [![Release](https://img.shields.io/github/v/release/eleost04/miyohub?color=blue&logo=github)](https://github.com/eleost04/miyohub/releases)
  [![Go Version](https://img.shields.io/badge/Go-1.27+-00ADD8?logo=go&logoColor=white)](go.mod)
  [![Vue 3](https://img.shields.io/badge/Vue-3.x-4FC08D?logo=vuedotjs&logoColor=white)](web/)
  [![Docker](https://img.shields.io/badge/Docker-Hub-2496ED?logo=docker&logoColor=white)](https://hub.docker.com/r/eleost/miyohub)
  [![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

  [简体中文](README.md) · [English](README.en.md) · [功能看板](ROADMAP.md) · [参与贡献](CONTRIBUTING.md) · [问题反馈](https://github.com/eleost04/miyohub/issues)

</div>

---

## 🌟 核心特性

- 🎮 **多游戏自动签到**：原生覆盖《原神》、《崩坏：星穹铁道》、《绝区零》、《崩坏3》、《未定事件簿》、《崩坏学园2》，支持云·原神与云·绝区零自动签到与免费时长获取。
- 🪙 **米游币与互动任务**：支持米游社社区签到、自动看帖、点赞与分享日常任务，支持自定义任务筛选与执行时间。
- ⚡ **秒级精准抢兑**：集成上游 HTTP Date 高精度时间校准，支持实物/虚拟商品定时预约、窗口期智能重试与防重放保护。
- 📢 **全平台消息通知**：支持 **手机 QQ 官方机器人扫码直连**、**微信 iLink 官方扫码**、Telegram、钉钉、飞书、Server酱、Bark、PushPlus、SMTP 邮件等 10+ 种渠道。
- 🛡️ **数据安全与隔离**：纯本地加密数据存储，跨进程独占文件锁，多用户权限严格物理隔离，出站网络默认防 SSRF 保护。
- 📱 **现代化响应式界面**：基于 Vue 3 + Tailwind CSS，支持深色/浅色视觉，完美适配桌面端、平板、移动端及窄屏设备。

---

## 🚀 快速开始

### 方式一：Docker 一键部署（推荐）

通过 Docker Compose，只需三步即可在本地或服务器快速启动：

```bash
# 1. 克隆代码仓库
git clone https://github.com/eleost04/miyohub.git
cd miyohub

# 2. 配置环境变量
cp .env.example .env

# 3. 启动服务
docker compose up -d
```

启动完成后，在浏览器中打开 `http://localhost:5890` 即可进入管理界面创建管理员账号。

> 💡 **生产环境建议**：默认仅监听 `127.0.0.1:5890`。若需公网访问，建议配合 Caddy 或 Nginx 配置 HTTPS 反向代理。

---

### 方式二：直接拉取预构建 Docker 镜像

如果你不需要克隆完整代码，可直接创建 `docker-compose.yml` 运行：

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

运行 `docker compose up -d` 即可。

---

## ⚙️ 常用环境变量

在 `.env` 文件中可以根据需要调整以下运行参数：

| 环境变量 | 默认值 | 作用与说明 |
| :--- | :--- | :--- |
| `MIYOHUB_HTTP_PORT` | `5890` | 宿主机访问端口 |
| `MIYOHUB_BIND_ADDR` | `127.0.0.1` | 宿主机监听地址（配合反向代理建议保持回环） |
| `MIYOHUB_PUBLIC_ORIGIN` | *空* | 对外访问的完整站点地址（例如 `https://miyohub.example.com`） |
| `MIYOHUB_SECURE_COOKIE` | `false` | 开启 HTTPS 时建议设为 `true`，防止 Cookie 明文传输 |
| `MIYOHUB_PUSH_ALLOW_PRIVATE` | `false` | 是否允许消息推送访问私网/回环中继（用于自建推送代理） |

---

## 🧭 项目文档

- 架构设计与迁移约束：[docs/architecture.md](docs/architecture.md)
- 命令行工具与使用手册：[docs/cli.md](docs/cli.md)
- 消息推送与各渠道配置：[docs/push.md](docs/push.md)
- 生产环境部署说明：[docs/deployment.md](docs/deployment.md)
- 功能进度与版本看板：[ROADMAP.md](ROADMAP.md)

---

## 🤝 参与贡献

非常欢迎提交 Issue 和 Pull Request！
在参与开发前，请查阅我们的 [参与开发与维护规范 (CONTRIBUTING.md)](CONTRIBUTING.md)。

---

## ⚠️ 免责声明

1. 本项目仅供个人学习、软件工程实践及自动化技术研究使用，不提供任何破解或商用服务；
2. 本项目为独立自托管开源项目，非米哈游（miHoYo）官方软件，与米哈游无任何商业或从属关联；
3. 使用本项目时请严格遵守相关游戏用户协议与社区公约，合理设置执行频率；
4. 因个人使用不当导致的任何账号风险或损失，项目维护者不承担任何直接或连带责任。

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

## 开发版：账号分组与批量操作

以下四项属于 `0.2.0-beta.1` 开发快照，位于 `feat/account-workspace`，待 PR 合入 `develop`；尚未打标签发布或部署。最新已发布预览版仍为 `0.1.0-beta.3`。隔离测试不代表真实上游或接收设备已验收，具体范围见[功能看板](ROADMAP.md)。

当前开发快照新增「任务总览 → 米游社账号 → 分组与批量」。可按分组筛选，每次选择最多 50 个自己的账号，设置分组、自动签到开关、个人时间或启停状态。批量设置时间只影响下一次调度，不立即执行，也不改变各账号选定的游戏；之后仍可逐个调整。只有明确点击「执行所选账号」才开始本次任务，沿用现有排队与间隔。

同名分组仅在当前用户的账号内生效；空名称表示移出分组。批量保存是原子操作，任一账号越权、已删除或签到设置版本冲突时不会部分保存。

## 开发版：加密账号迁移

「个人账号 → 账号迁移」支持在可信的部署实例间迁移自己的账号、分组和签到设置（最多 100 个）。导出需要再次输入本站密码，并设置独立的迁移密码（至少 12 个字符）；文件采用 PBKDF2-SHA256（600,000 次）与 AES-256-GCM 加密。请分开保管文件和迁移密码，仅通过可信 HTTPS 站点导入。

导入先解密预览，5 分钟内确认；已有 UID 或重名账号跳过且不覆盖。新账号默认停用、自动签到关闭，核对后自行启用。迁移不包含站点权限、会话、日志、打码 / 推送密钥或兑换预约，也不会停用原实例；切换前应停用原实例的对应任务，避免两端重复执行。这是文件迁移，不是持续同步；已发布的 `0.1.0-beta.3` 不包含此项。

## 开发版：游戏便笺

「游戏便笺」（手机位于「更多」）按需读取原神、星穹铁道和绝区零的体力、每日进度、派遣等官方数据，不执行游戏内操作。先选择自己的米游社账号与游戏，读取后可切换该账号实际绑定的角色。数字和数字字符串按整数解析，未知数值 / 布尔状态显示「未提供」，不会被当作 0 或已完成；恢复时间是基于记录时间的估计。

便笺缓存 3 分钟、角色缓存 15 分钟，同账号请求串行且至少间隔 3 秒；页面、全局状态轮询和隐藏标签不会自动查询上游。验证限制触发 6 小时的账号级冷却，限流遵守 `Retry-After`；更换游戏或凭据不会解除本进程的冷却。可用旧快照会明确标注。数据查询复用站点出站代理和已绑定设备，不自动换设备、更新凭据或打码。请在官方客户端开启战绩 / 便笺并完成所需验证；这些限制只能降低请求压力，不能保证不触发上游风控。此项尚未随 `0.1.0-beta.3` 发布。

## 开发版：活动与版本日历

「活动日历」按需读取原神与星穹铁道已公布的卡池、限时活动和挑战，缓存 30 分钟，并与便笺共用账号冷却。只接受可识别的时间戳，区分秒和毫秒；开始时间或完成状态缺失时不猜测，格式错误的项目会说明未展示。绝区零官方活动接口尚未接入。

三款游戏均可添加个人自定义日程，记录已公告的版本更新时间等；日期按界面显示的时区录入，服务端校验时间顺序和一年范围，每用户最多 100 项。自定义日程标为「自行录入」，不冒充官方数据，不会自动更新未来时间。数据仅当前用户可见，删除账号同步移除其日程；本项不包含在已发布的 `0.1.0-beta.3` 中。

### 日历提醒

在日程卡片点击「设置提醒」，选择开始 / 结束及准时、提前 10 分钟、30 分钟、1 小时或 1 天。提醒只使用刚读取的官方快照或当前用户的自定义日程，不额外访问上游；时间变化需自行取消后重新安排。在「消息推送」开启自动推送与「日历提醒」并配置接收渠道后才会发送，默认关闭。「仅提醒异常」只过滤签到 / 兑换，不过滤显式订阅的日历提醒。

后台每 30 秒检查本地提醒（每批最多 50 项），提醒状态与加密待发队列一起原子保存；重启不会重复入队，结果不确定的通知不重发。超过提醒时刻 15 分钟，或超过目标时刻 5 分钟的提醒失效，不集中补发。到期仍核对用户、账号和推送权限；关闭推送 / 停用账号会跳过。最多保留 200 项个人提醒，可显式清理已结束记录；已提交给推送服务的通知不能撤回，服务受理不代表设备送达。迁移文件不包含日程和提醒。

## 🤝 参与贡献

非常欢迎提交 Issue 和 Pull Request！
在参与开发前，请查阅我们的 [参与开发与维护规范 (CONTRIBUTING.md)](CONTRIBUTING.md)。

---

## ⚠️ 免责声明

1. 本项目仅供个人学习、软件工程实践及自动化技术研究使用，不提供任何破解或商用服务；
2. 本项目为独立自托管开源项目，非米哈游（miHoYo）官方软件，与米哈游无任何商业或从属关联；
3. 使用本项目时请严格遵守相关游戏用户协议与社区公约，合理设置执行频率；
4. 因个人使用不当导致的任何账号风险或损失，项目维护者不承担任何直接或连带责任。

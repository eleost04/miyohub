# MiyoHub

[English](README.en.md) · 当前版本：**0.1.0-beta.1**

[功能看板](ROADMAP.md) · [参与开发与维护规范](CONTRIBUTING.md) · [问题反馈](https://github.com/eleost04/miyohub/issues) · [安全政策](.github/SECURITY.md)

自部署的米游社签到、米游币任务与商品兑换管理。Go + Vue 3，非米哈游官方产品；上游接口、账号风控和商品库存可能变化，不保证奖励到账或兑换成功。

## 功能

- 米游社账号扫码、短信或 Cookie 绑定；支持原神、星穹铁道、绝区零、崩坏 3、未定事件簿、崩坏学园 2 签到，云·原神 / 云·绝区零时长及米游币日常任务。
- 每个绑定账号独立配置签到项目、角色排除、社区及自动执行时间。个人时间优先，未设置时使用站点默认时间。
- 实物 / 虚拟商品浏览、地址与角色选择、即时兑换、定时预约、有限重试及执行记录。
- 个人打码渠道、按权限使用的站点打码、后台可查询的匿名测试；短信人机验证也可由用户手动完成。
- PushPlus、Telegram、WxPusher、钉钉、飞书、QQ 官方机器人、QQ OneBot、邮件、微信 iLink、Webhook；QQ / 微信可扫码绑定。
- 手机与桌面适配选择器、分层返回、偏好自动保存、按需悬浮保存、可关闭的新手引导、SVG 图标和关联提交的更新日志。

站点用户是权限边界：管理员也不能在日常页面访问其他用户的米游社账号、私人日志和兑换计划。用户管理与系统设置分别在独立页面。

本版本为 Beta：短信、米游币请求、QQ 网关与兑换兼容性已通过隔离回归，真实账号风控和客户端仍需验收，详见看板。`main` 保持稳定基线，测试版从签名标签或 `develop` 获取。

参与开发请克隆 `develop`：`git clone --branch develop https://github.com/eleost04/miyohub.git`。普通克隆默认检出 `main`；已有完整克隆可执行 `git fetch origin develop`，再用 `git switch develop` 切换（没有本地分支时会跟踪 `origin/develop`）。

## 快速开始

需要 Docker Engine 和 Compose v2。

```bash
git clone --branch v0.1.0-beta.1 --depth 1 https://github.com/eleost04/miyohub.git
cd miyohub
cp .env.example .env
docker compose up -d --build
docker compose ps
curl -fsS http://127.0.0.1:5890/api/v1/health
```

默认只监听宿主机回环地址。先在可信本机环境创建管理员，再向公网开放；首次设置入口不应长时间暴露。远程服务器可使用 SSH 端口转发访问本机端口。

容器名为 `miyohub`，数据卷为 `miyohub_miyohub-data`，网络为 `miyohub_default`。根文件系统只读、非 root 运行。不要使用 `docker compose down -v` 更新，它会删除账号数据卷。

另一个可选项目 [miyohub-captcha](https://github.com/eleost04/miyohub-captcha) 提供本机 CPU 验证码服务；不需要自建服务时可只部署主站。

## 环境变量

Compose 从项目目录的 `.env` 读取下列部署变量；复制 `.env.example` 后按需修改，建议 `chmod 600 .env`。只提交示例，不提交实际配置。裸程序**不会自动加载 `.env`**，请通过进程环境或 CLI 参数传入。

| 变量 | 默认值 | 作用 / 注意事项 |
| --- | --- | --- |
| `MIYOHUB_IMAGE` | `miyohub:local` | 仅 Compose：本地镜像或已登录仓库中的镜像；跟随开发版可设 `ghcr.io/eleost04/miyohub:develop` |
| `MIYOHUB_BIND_ADDR` | `127.0.0.1` | 仅 Compose：宿主机监听地址；公网反代建议保持回环 |
| `MIYOHUB_HTTP_PORT` | `5890` | 仅 Compose：宿主机映射端口；不改变容器内部端口 |
| `MIYOHUB_PUBLIC_ORIGIN` | 空 | 浏览器访问的完整来源，如 `https://miyohub.example.com`；无路径，需与协议 / 主机 / 端口一致 |
| `MIYOHUB_SECURE_COOKIE` | `false` | HTTPS 部署设为 `true`；启用后 Cookie 仅通过 HTTPS 发送，纯 HTTP 本地访问不要开启 |
| `MIYOHUB_PUSH_ALLOW_PRIVATE` | `false` | `true` 允许**所有站点用户**的推送渠道访问私网 / 回环；只用于完全可信的部署，不建议多用户公网服务开启 |

布尔值只有小写 `true` 才启用。修改 Compose 环境后用 `docker compose up -d --force-recreate` 重建容器；`docker compose restart` 不会更新环境。

直接运行二进制 / `go run` 另支持以下变量；CLI 同名用途的参数优先：

| 变量 | 裸程序默认值 | 对应 CLI 参数 / 容器行为 |
| --- | --- | --- |
| `MIYOHUB_HOST` | `127.0.0.1` | `--host`；镜像内部默认 `0.0.0.0` |
| `MIYOHUB_PORT` | `5890` | `--port`；Compose 内部健康检查使用此默认端口 |
| `MIYOHUB_DATA_DIR` | `data` | `--data-dir`；镜像内部为持久卷 `/data`，必须同时保留状态和密钥 |
| `MIYOHUB_WEB_DIR` | `web/dist` | `--web-dir`；相对于工作目录，镜像中为 `/app/web/dist` |

```bash
MIYOHUB_HOST=127.0.0.1 MIYOHUB_PORT=5890 MIYOHUB_DATA_DIR=./data \
  go run ./cmd/miyohub serve --web-dir ./web/dist
```

把裸程序专用变量写入 Compose 的 `.env` 并不会自动传进容器，现有 Compose 只传递表中声明的部署变量。账号凭据、签到时间、打码与推送密钥通过网页配置；本项目不支持用 `MIYOHUB_ADMIN_PASSWORD` 或 Cookie 环境变量创建用户。米哈游出站代理在系统设置配置，不读取 `HTTP_PROXY` / `HTTPS_PROXY` / `ALL_PROXY`。

## HTTPS 与 Caddy

在不提交到 Git 的 `.env` 中填写实际公开地址：

```dotenv
MIYOHUB_PUBLIC_ORIGIN=https://miyohub.example.com
MIYOHUB_SECURE_COOKIE=true
```

宿主机 Caddy 示例：

```caddyfile
miyohub.example.com {
    encode zstd gzip
    reverse_proxy 127.0.0.1:5890
}
```

域名替换为自己的域名；公开地址必须与浏览器中的协议、主机和端口一致，不能带路径。修改环境变量后执行 `docker compose up -d --force-recreate`。Caddy 默认保留外部 Host；不要改写成容器内部主机名，也不要关闭跨站请求防护或添加允许所有来源的 CORS。Caddy 在容器中时，应通过双方共享的 Docker 网络连接 `miyohub:5890`，其内部 `127.0.0.1` 不是宿主机。

## 账号、邀请与签到

首次配置引导可跳过或关闭，在「个人账号」重新打开。扫码 / 短信绑定之后，在账号卡片「签到设置」选择游戏、云游戏、米游币任务、时区及每日时间；云游戏需单独填写对应的 `x-rpc-combo_token`。

站点运行总开关可暂停全部签到；关闭“站点默认调度”不会关闭用户已设定的个人时间。个人调度每 10 秒检查一次，不补跑停机期间错过的时间；同一账号同一天的自动运行有持久去重，手动运行不受此限制。显示时间不等于精确到秒的执行承诺。

米游币状态查询使用完整 Cookie 与网页请求头；社区签到、互动及验证码校验按各自接口使用 App 凭据和签名。每个账号在「签到设置 → 米游币 → 执行方式」独立选择：

- **按奖励进度执行（默认）**：今日还可获得 0 时只查状态；已完成或未列出的看帖 / 点赞 / 分享项目跳过，社区签到缺项仍按用户选择处理。
- **按所选项目执行**：即使奖励任务缺失或已领取完，也执行明确开启的项目；所选社区各签到一次，看帖最多 3 篇、点赞最多 5 篇、分享最多 1 篇，并受最多帖子数限制。不会自动开启原先关闭的项目；手动再次运行会重新执行。网络 / 凭据错误仍按原安全规则处理。

两种模式都复查积分，操作成功不等于奖励到账；既有积分不是本次新增，也不说明需要打码。旧账号不会被自动切换为主动执行模式。

管理员可生成随机邀请码，预设兑换与站点打码权限。新邀请码默认一次使用、七天有效，默认授予这两项权限并选择站点打码；管理员可以修改。没有邀请码的普通注册不会默认获得这两项权限。旧邀请码不会被追溯提权。生成者失去管理员权限或被停用后，其邀请码也不能继续使用；并发注册、过期、停用、次数上限及授权在服务端检查。

每个用户有稳定的站点用户 ID，可在「个人账号」查看；它不是米游社 UID。推送默认关闭，由用户自己开启。

## 验证码服务与短信

个人「打码服务」可选择关闭、自己的渠道或已获授权的站点服务：

- 自己的打码狗 UserKey 或自定义公网接口不需要站点服务授权；个人渠道失败不会偷偷转用站点额度。
- 站点服务由管理员在系统设置维护、在后台管理或邀请码中授权。普通用户不能用自己的配置访问服务器内网。
- 自建服务位于同一 Docker 网络时，站点渠道填写 `http://miyohub-captcha:9645/pass_nine`，建议超时 60 秒；主机程序才使用 `127.0.0.1:9645`。

自定义接口接受 GET 参数 `gt`、`challenge`、`use_v3_model`，成功契约：

```json
{"data":{"result":"success","validate":"...","challenge":"optional-updated-challenge"}}
```

支持 Bearer Token，不要把密钥写入 URL。只传验证码参数，不向服务发送米游社 Cookie / SToken。个人接口禁止私网、回环、元数据地址、重定向及环境代理，并在实际连接时重新校验 DNS。

匿名测试在后台执行，页面关闭不取消；每用户每分钟最多一次，最长约 65 秒，结果和脱敏日志可稍后查询。刷新状态不会重复调用。测试不调用打码狗、不执行账号任务，但自定义服务是否收费仍由提供方决定。返回校验参数不等于真实账号一定通过验证。

短信支持 Geetest 3 / 4 手动验证，发送短信和提交验证码两个阶段都可以验证后继续；第三方组件仅在隔离页面按需加载。Geetest 3 也可选择配置好的自动渠道，Geetest 4 不会误交给仅支持 V3 的服务。短信会话 10 分钟、人机验证挑战最多 2 分钟；重启需重新获取。发送失败或冷却中不会显示“验证码已发送”。真实上游风控仍需本人账号验收。

## 兑换时间与重试

米游币状态查询默认最多自动重试 5 次（共 6 次请求）；管理员可在「系统设置 → 连接与重试」配置 0–10 次。网络中断和临时 HTTP 错误采用 1/2/4/8 秒退避，并遵守上游 `Retry-After`；要求等待超过一分钟时本轮暂停。不重试验证码或凭据错误，也不重发短信、签到、点赞和结果不确定的兑换请求。日志显示实际重试次数和停止原因。

### HTTP / SOCKS5 代理

管理员在「系统设置 → 连接与重试 → 配置代理」填写 `socks5://proxy.example:1080` 或 HTTP(S) 地址，用户名/密码使用独立字段；SOCKS5 与 SOCKS5H 均由代理解析目标域名。配置加密落盘，密码不回传浏览器；更换地址或用户名不沿用旧密码。弹窗应用后再由页面保存，不会在填写一半时启用。

代理用于米游社登录、游戏/云游戏签到、米游币、商品查询/兑换和米哈游校时。保存后从后续请求生效，无需重启；开启后代理故障不会回退直连。关闭时这些接口直连，不读取 `HTTP_PROXY` / `HTTPS_PROXY`。个人/站点打码、推送和浏览器图片不经过此代理，个人接口的 SSRF 防护不变。Docker 中 `127.0.0.1` 是容器自身；宿主机代理须绑定容器可访问的地址并配置防火墙，不能只监听宿主机回环地址。

预约提前约 180 秒准备；不同商品可同时准备，同账号的实际请求逐次串行。优先用商品详情 `now_time` 校时，HTTP `Date` 为备用，开售前再次同步。上游时间通常只有秒级精度，开始兑换前会按误差下界等待，避免把估计时间当作精确时刻。页面展示来源及误差估计；请同时保持宿主机 NTP 正常。校时无法消除网络延迟，也不保证抢到库存。

兑换使用账号自己的设备信息与网页设备指纹，申请指纹时提供平台 5 的环境参数，不附带账号 Cookie。实体地址、虚拟角色、余额和价格在提交前重新校验；请求参数按 MiyoQian 的兑换流程核对，不能以准备完成代替真实兑换成功。

已预约的本轮商品不会因接口切换成下周补货时间而被误判为提前兑换；真正延期、售罄及新建预约仍分别校验，不会擅自改成下一轮预约。

商品列表缺少本轮开售时间时显示“开放时间待详情确认”，进入单品后再查询详情并填写预约时间。这样列表只加载分页数据，无需逐个补查商品。时间同时包含开售与补货字段时，按 MiyoQian 的轮次规则选取有效时间；空库存字段按缺失处理，不当作已售罄。

“未到兑换时间”、明确临时拒绝及繁忙会在窗口内继续尝试。预约窗口从**计划时间**开始，准备或排队不会延长截止时间；即时兑换从本次执行开始。窗口最多 120 秒，实际间隔不低于 0.2 秒，保护上限为 600 次，不再用 60 次提前截断长窗口；频繁提示至少退避 2 秒。零秒窗口只请求一次，预约保留一分钟派发宽限，不代表无限重试。

成功、余额不足、限购、售罄、已结束、登录失效和参数错误停止。超时、连接中断或无法解析响应意味着结果不确定：**不会自动重放**，先去米游社核对记录，避免重复扣币。窗口截止后不再发起新请求，但会等待已发请求的结果（最长 15 秒）。很短的窗口可能不足以覆盖校时误差或排队时间。通知只发送最终摘要，详细尝试和返回码留在日志。

## 推送与界面

各用户独立配置自动推送、签到 / 兑换范围和仅异常通知，只发送其绑定账号的结果。点击接收方式即可在弹窗配置，无需再找“添加渠道”。启用某个渠道不等于启用自动推送总开关；手动测试会立即发送消息。

“推送服务已接收”仅表示服务端受理成功，不承诺终端送达或阅读。QQ 官方扫码在官方页面创建 / 选择机器人；微信扫码后须先发送一条消息建立会话。官方权限、消息时效与额度仍然适用。

QQ 官方页的“连接”要等机器人网关上线才会完成。MiyoHub 在绑定凭据后建立鉴权与心跳连接，断开后有限退避重连；在渠道配置及运行日志查看“正在连接 / 已上线 / 正在重试”及原因。连接独立于通知开关：关闭自动推送不会使机器人离线，删除渠道、清除 ClientSecret 或停用用户才会断开。不会回复或保存聊天，也不会根据来信更换接收者。服务器需能访问 QQ 官方 HTTPS / WSS；若本站显示已上线而官方页仍不能完成，再检查 QQ 客户端版本、网络与机器人权限，不能仅据此认定是鸿蒙问题。

QQ / 微信扫码任务在服务端保持 5 分钟。关闭弹窗、切换应用或刷新页面不会取消，重新打开相同入口即可恢复，且不会延长有效期或重复创建二维码。“取消绑定”立即作废，“刷新二维码”替换旧会话；修改推送配置后需重新扫码。每用户同时一个任务，重启服务会丢失未完成会话；绑定成功后保存的渠道不受影响。QQ 凭据保存与机器人上线分开显示。

偏好默认停止输入约 1 秒后自动保存，可在当前浏览器关闭；密钥不存入浏览器缓存。账号凭据、密码、权限、新增渠道和兑换计划仍需明确提交。手机右下角的保存按钮只在有未保存修改且没有同屏保存入口时出现。浏览器返回按选择器、弹窗、详情和页面逐层返回。

## 性能与资源

界面使用小体积 SVG；ICO / iOS PNG 仅作兼容。页面、配置弹窗、二维码库和人机验证按需加载。静态 JS / CSS 预生成 Brotli、gzip，哈希资源缓存一年，入口页重新验证；私人 API 不缓存。商品图片通过受限官方 CDN 302 跳转，主站不下载图片；这不解决客户端无法访问 CDN 的情况。

参考占用（Linux/amd64，2026-09-11 构建样本）。镜像为未压缩大小；实际内存随负载变化：

| 服务 | 镜像 | 参考空闲内存 | 建议预留 |
| --- | ---: | ---: | --- |
| MiyoHub | 18.6 MiB | 约 14 MiB（刚启动） | 1 核、256 MiB RAM |
| 可选打码服务 | 364.1 MiB，另需约 177 MiB 模型 | 约 352 MiB（刚启动）；既有运行样本约 535 MiB | 2 核、1.5 GiB RAM |

两者同机建议至少 2 核 / 2 GiB，可用磁盘预留 3 GiB 以上；本机构建建议 4 GiB RAM 和 5 GiB 以上可用磁盘，构建缓存、备份及日志另计。空闲样本不是容量上限，高并发与复杂验证需要更多余量。打码服务固定单 worker，不能简单增加 worker 数。

源码与 Linux 安装包通过签名 Release 发布。主站另提供可启用的私有开发镜像流水线，详见下节；打码镜像与模型不随主站上传，第三方运行代码及模型的再分发许可需另外核实。

## 跟随开发版镜像

仓库维护者在 Actions Variables 设置 `MIYOHUB_PUBLISH_DEV_IMAGE=true` 后，`develop` 的代码 CI 通过才自动构建并推送 Linux amd64 / arm64 镜像：

- `ghcr.io/eleost04/miyohub:develop`：最近通过验证并发布的开发镜像。
- `ghcr.io/eleost04/miyohub:sha-<完整提交 SHA>`：对应源码提交的追溯 / 回退标签。
- 需要严格固定镜像内容时使用 `ghcr.io/eleost04/miyohub@sha256:<digest>`；digest 在 Actions 摘要中显示，标签本身不是不可变存储。

PR 和未通过检查的代码不会发布；纯文档变更不重建镜像，必要时可手动运行 `Build and test` 并选择 `develop`。发布检查仓库与已有镜像包均为私有、提交签名已验证；权限或检查失败即停止，不改变包的可见性。使用工作流自带的 `GITHUB_TOKEN`，仅镜像发布作业拥有 `packages: write`，不需要上传账号凭据或 PAT。fork / 其他仓库需自行选择是否启用；当前策略不会发布公开包。

主机拉取私有镜像需要 GitHub Packages 读取权限；在受信终端运行 `docker login ghcr.io -u YOUR_GITHUB_USERNAME`，在密码提示中输入具备 `read:packages` 权限的凭据（不是 GitHub 登录密码），不要放进命令、README 或项目 `.env`。然后把 `.env` 的 `MIYOHUB_IMAGE` 改为开发镜像地址：

```bash
docker compose pull miyohub
# 确认没有运行中任务 / 临近兑换，备份状态与密钥后执行：
docker compose up -d --no-build miyohub
docker compose ps
```

镜像自动发布不等于运行中的容器自动升级。默认不安装无人值守更新器，避免更新中断兑换；生产建议先验收 Beta，再锁定 digest。回退时修改 `MIYOHUB_IMAGE` 为保留的 SHA 标签 / digest，再拉取并重建，保留当前数据卷；不把旧状态备份直接覆盖新用户数据。Compose 文件、环境变量及数据格式仍应按对应版本的升级说明核对。arm64 镜像会构建，但不等于完成实机验收。

## 更新、备份与日志

```bash
docker compose build
docker compose up -d --no-build
docker compose logs --tail 100
docker compose exec miyohub /usr/local/bin/miyohub logs --component bbs --tail 100
```

更新前避开临近预约和正在运行的任务，备份 `state.json` **及同目录的 `state.json.key`**，保持文件权限 600。两者必须匹配；建议短暂停止服务后成对复制，并妥善保存旧镜像。不上传备份、截图、密钥或验证码图片到 GitHub。只读 `logs` 可与服务并行；其他访问同一数据目录的 CLI 命令应先停止 Web 服务。没有显式授权时不要删除或重放历史预约。

## 开发与发布

Go 版本见 `go.mod`，前端使用 Node.js 22 / npm。

```bash
npm --prefix web ci
npm --prefix web run build
go test -race ./...
go vet ./...
node --test scripts/*.test.mjs
node scripts/check-version.mjs
node scripts/check-repository.mjs
cd web
npx playwright install --with-deps chromium
npm run test:e2e
```

真实 API 浏览器测试每条使用独立进程、临时加密状态和原有限流，需要本机 4177、5897 端口空闲。测试不读取实际账号，也不发送真实短信、通知、签到或兑换请求。前端开发运行 `npm --prefix web run dev`；后端运行 `go run ./cmd/miyohub`。

贡献流程、`fix` / `feat` 提交规则、GPG 签名、分支职责、版本递增和发布检查见 [CONTRIBUTING.md](CONTRIBUTING.md)。功能范围、已知问题及与参考项目的差异见 [ROADMAP.md](ROADMAP.md)。站内「更新日志」记录面向用户的改动。

发布流程检查签名标签和分支归属，构建包含前端资源的 Linux amd64 / arm64 安装包；arm64 仅交叉构建，尚未进行硬件验收。Actions 的测试与发布工作流均支持手动恢复，不跳过检查，不覆盖已有 Release。

GitHub Actions **不运行任何账号签到、兑换或推送**，也不需要上传账号状态或密钥。日常任务请在自己的 Docker / 主机上调度。CI 只在 `main` / `develop` 推送、面向这两个分支的 PR 或手动操作时运行；纯文档改动保留凭据扫描与仓库检查，跳过 Go / 浏览器 / Docker 重型测试。特性分支提交先在本地验证并推送，建立 PR 后再由 CI 检查。

Git 保留源码、必要测试、依赖锁文件、构建 / CI 配置和双语 README。排除 `.env*`（示例除外）、`data/`、状态及密钥、`docs/`、备份、日志、依赖目录、构建产物、浏览器测试报告和压缩归档；伴随打码仓库还排除 `models/`、`upstream/` 和验证码图片。版本流程扫描提交历史中的凭据；不要用忽略规则代替提交前检查。

## 相关项目

[MiyoQian](https://github.com/ytf211/MiyoQian)、[MiyoSign](https://github.com/ytf211/miyosign) 与 [MihoyoBBSTools](https://github.com/Womsxd/MihoyoBBSTools) 提供了接口和功能设计参考；验证码服务参考 [相关讨论](https://github.com/Womsxd/MihoyoBBSTools/issues/198) 与 [test_nine](https://github.com/luguoyixiazi/test_nine)。

本项目与米哈游及上述项目无官方隶属关系。第三方代码、模型和服务遵循各自条款；功能参考不代表可以忽略其许可或使用限制。

安装包与容器携带 [第三方许可说明](THIRD_PARTY_NOTICES.md)；这不改变项目本身、验证码模型或外部服务的许可范围。

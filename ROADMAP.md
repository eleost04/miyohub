# 功能与兼容性看板

稳定基线：`0.0.1`。本轮主分支预览为 `0.2.0-beta.2`：已完成本机与 GitHub CI 验证、主站部署及 Docker Hub Linux amd64 镜像发布，保留 Beta 标识，不创建新的正式 Release。最新 GitHub 发布标签仍为 `v0.1.0-beta.3`。发布方式与提交规则见 [CONTRIBUTING.md](CONTRIBUTING.md)。

本看板区分代码实现、部署与真实上游 / 设备验收。`main` 本轮同步当前预览，日常开发继续在 `develop`；不收集账号、游戏 UID、余额、验证码或机器人凭据。

状态含义：**已实现** = 有代码及本地回归，不代表所有真实账号/设备都已验收；**修复中** = 已知问题正在处理；**待对齐** = 需要和参考行为逐项比较；**待验收** = 需要外部服务或实际设备确认；**规划** = 尚未实现。不要用百分比代替可验证条目。

## 当前维护周期

| ID | 类型 | 项目 | 状态 | 验收条件 |
| --- | --- | --- | --- | --- |
| AUTH-01 | fix | 短信登录 Geetest 3/4 兼容 | 已实现，待真实上游验收 | 已覆盖两种挑战、发送与登录恢复、归属/过期/防重放、320px 沙箱与按需加载；需本人完成一次真实验证 |
| TASK-01 | fix | 米游币运行被无变化保存取消 | 已部署，配置变更场景待真实复测 | 无变化保存不递增版本；只改时间/自动开关不打断本次执行；正常签到已有用户反馈，但不代替运行中修改配置的验收 |
| TASK-02 | feat | 增加可配置的状态查询重试 | 已实现，隔离测试通过 | 默认 5 次，可设 0–10；实际次数、可取消退避和 Retry-After 已测试；手机/桌面配置可保存，不重放写请求 |
| TASK-03 | fix | 对齐米游币网页 / App 请求配置 | 社区签到与增币已有用户反馈，互动待实际任务验收 | 用户反馈社区签到成功、复查有增量且剩余可得为 0；看帖 / 点赞 / 分享代码已实现，但任务列表缺少对应项目时不主动执行，不将缺失项目判定为永久不支持 |
| TASK-04 | feat | 账号自主选择奖励无关的社区任务 | 已部署，主动互动待真实验收 | 默认按奖励进度；主动选择后，奖励列表缺项或已完成仍有限执行已开启项目；不自动开启点赞 / 分享，不把零增币误报为操作失败；按账号保存，320px / 桌面自动保存与返回测试通过 |
| NET-01 | feat | HTTP / SOCKS5 出站代理 | 已实现，隔离测试通过 | 管理员弹窗配置；覆盖米游社官方接口；密码加密/不回显；HTTP CONNECT / SOCKS5 / SOCKS5H 握手、错误不回退、私有地址隔离与手机/桌面保存测试通过 |
| SHOP-01 | fix | 对齐 MiyoQian 商品兑换流程 | 已实现，待真实上游验收 | 设备指纹 / 请求头 / 实体与虚拟负载已核对；补开售与补货轮次、空库存判断；缺时间时按需查详情，320px / 桌面预约回归通过；保留未知结果不重放、校时及窗口重试 |
| SHOP-02 | fix | 未到兑换时间停止重试、窗口提前结束 | 已部署，待下一次真实兑换验收 | 秒级校时按下界等待，未来开售时间优先于冲突的在线标识；未到时间 / 繁忙继续尝试；窗口锚定预约时间且排队不延期，保留最小请求间隔和最长 120 秒窗口；成功 / 售罄 / 未知结果停止 |
| PUSH-01 | fix | QQ 官方页面点击连接无响应 | 已部署并观测到网关上线，问题设备待确认 | 官方页等待 online_state=1；网关鉴权/心跳/恢复已覆盖；已观测到真实连接保持在线，但不等同于鸿蒙客户端点击连接已验收 |
| PUSH-02 | fix | 签到通知过于冗长 | 已部署，隔离回归通过 | 正常通知只保留账号 / 时间、签到统计、复查确认的米游币增量与余额；异常原因去重并限长，失败 / 停止不报成成功；原始明细与脱敏规则保留 |
| LOG-01 | fix | 汇总行无法查看完整过程、异常筛选误报 | 已部署，隔离回归通过 | 新日志按账号和执行编号关联；旧日志明确显示上下文；320px / 桌面可查看和导出明细，实时刷新后返回保持焦点；失败 0 不算异常，不改变用户隔离 |
| DOC-01 | docs | 面向项目读者的双语说明与维护规则 | 已实现 | README、功能看板、版本/分支/提交/发布规则可直接使用 |
| CI-01 | ci | 停止 GitHub 账号任务、保留精简代码验证 | 已实现 | 签到工作流远端已停用并从源码移除；主干 / PR 自动 CI，纯文档保留安全检查；发布步骤最小权限，Action 固定 SHA |
| CI-02 | test | CONNECT 代理测试夹具的等待时序 | 已修复，beta.3 发布验证通过 | beta.2 Release 暴露 WaitGroup 注册与等待缺少 Go 同步关系；旧代码本地复现，修复后单例 100 次竞态与全代理包 20 轮通过；只改隔离测试，不改变代理、鉴权或任务业务 |
| DOC-02 | docs | 环境变量、Issue 表单与安全政策 | 已实现 | 中英部署变量与开发入口；脱敏提示、私密上报可用性说明；许可证依维护者决定暂不新增 |
| IMAGE-01 | build | develop 私有镜像跟随 CI | 历史 beta.3 验证通过；当前开关已关闭 | CI、签名和私有范围通过才发布 develop / SHA 镜像；已记录多架构 digest，本地健康检查通过；不自动换版生产容器，不等同于 arm64 真机验收 |

## 已有功能与参考项目差异

| 功能 | MiyoHub 状态 | 实现 / 验证入口 | 参考与边界 |
| --- | --- | --- | --- |
| 米游社扫码、Cookie 绑定 | 已实现，真实风控待验收 | `internal/auth/`、`web/tests/onboarding.spec.mjs` | 参考 MiyoQian / MiyoSign；短信问题单列 AUTH-01 |
| 六款游戏签到、角色排除 | 已实现；三款游戏已有用户成功反馈 | `internal/tasks/game_checkin.go`、`internal/tasks/*_test.go` | 原神 / 星穹铁道 / 绝区零已有成功反馈；其余游戏及角色排除场景不据此宣称全部真实验收 |
| 云·原神 / 云·绝区零 | 已实现，真实服务待验收 | `internal/tasks/cloud_checkin.go` | 需独立 Combo Token |
| 米游币社区及互动任务 | 社区签到与增币已有反馈，互动待验收 | `internal/tasks/bbs_checkin.go`、`internal/tasks/bbs_selected_test.go` | 默认尊重奖励任务进度；用户可主动选择按所选项目执行，无奖励也有限执行已开启项目；不擅自开启点赞或分享 |
| 个人签到时间、每日去重 | 已实现 | `internal/scheduler/`、`internal/store/task_settings.go` | 个人时间优先；不补跑离线时段 |
| 实物 / 虚拟商品与预约 | 已实现，SHOP-01 待真实验收 | `internal/shop/`、`internal/shop/engine_test.go`、`web/tests/selectors.spec.mjs` | 参考 MiyoQian；开售时间缺失时先查单品详情；不承诺库存或兑换成功 |
| 上游时钟与有限兑换重试 | 已实现 | `internal/shop/clock.go`、`internal/shop/exchange.go` | 秒级上游时间；未知结果不盲目重复提交 |
| 多用户隔离、邀请码预授权 | 已实现 | `internal/store/permissions.go`、`internal/api/owner_isolation_test.go` | 管理员在日常页面也只见本人账号 |
| 个人 / 站点验证码服务 | 已实现 | `internal/captcha/`、`internal/api/captcha_probe.go` | 自建服务为可选组件；后台探测有独立冷却 |
| QQ、微信及其他推送渠道 | 已实现，外部渠道待验收 | `internal/notify/`、`web/src/components/PushPanel.vue` | 收件人、渠道额度和有效期遵守官方限制 |
| 移动端弹窗、自动保存、SVG | 已实现 | `web/src/components/`、`web/tests/` | 持续限制首屏资源；密钥不写入浏览器缓存 |
| 游戏实时便笺 | 已实现并部署预览，真实上游待验收 | `internal/gamerecord/`、`web/tests/game-notes.spec.mjs` | 原神 / 星铁 / 绝区零按需读取；3 分钟缓存、串行限频与账号级验证冷却，未知值不当作零 |
| 活动日历与显式订阅提醒 | 已实现并部署预览，真实上游 / 送达待验收 | `internal/mihoyo/calendar.go`、`internal/store/calendar_reminders.go` | 原神 / 星铁官方活动，三款游戏可自定义日程；默认不推送，绝区零官方活动未接入 |
| 账号分组、批量设置与加密迁移 | 已实现并部署预览 | `internal/store/account_batch.go`、`internal/transfer/` | 所有权与版本冲突校验、原子保存；迁移独立密码与明确确认，不携带权限或预约 |
| 主站私有开发镜像自动发布 | 历史验证通过，当前开关已关闭 | `.github/workflows/ci.yml`、`scripts/image-policy.mjs` | develop 完整 CI 通过后发布 develop / SHA 标签；检查私有可见性与签名，PR 不发布；打码镜像 / 模型不包含在内 |

参考代码：[MiyoQian](https://github.com/ytf211/MiyoQian)、[MiyoSign](https://github.com/ytf211/miyosign)、[MihoyoBBSTools](https://github.com/Womsxd/MihoyoBBSTools)。接口随上游变化；仅借鉴可验证的行为，不以参考项目能运行替代本项目验收。

## 0.2.0-beta.2 新增能力与部署记录

- [x] **游戏实时便笺与状态面板**（已实现并部署预览）：原神 / 星铁 / 绝区零体力与日常进度按需读取；角色归属校验，缺失值不当作零，3 分钟缓存、串行限频与账号级验证冷却。手机与桌面隔离回归通过；官方验证 / 隐私限制不承诺绕过，真实上游场景仍需验收。
- [x] **版本活动日历与提醒**（已实现并部署预览）：原神 / 星铁官方卡池、活动、挑战日历；三款游戏均可自行录入已公告的版本日程。提醒显式订阅、默认不推送，30 秒本地调度、原子入队、取消 / 过期 / 重启防重发及多屏回归已覆盖。绝区零官方活动接口未接入，版本日期不自动猜测；真实渠道送达另行验收。
- [x] **加密配置导出与跨实例迁移**（已实现并部署预览）：独立密码加密账号 / 分组 / 签到设置，本站密码复核、5 分钟预览与明确导入；不覆盖已绑定账号，新账号默认停用，不携带权限、日志、推送密钥或预约。属于文件迁移，不持续同步两端。
- [x] **多账号分组与批量操作**（已实现并部署预览）：按个人账号分组筛选；每次最多 50 个账号原子修改分组、自动开关或个人时间，也可显式执行所选账号。批量时间不立即运行、不改变已选游戏；跨用户 / 过期配置整批拒绝。320px、390px、桌面及隔离竞态回归通过。
- [x] **加载与登录页 Logo 尺寸修复**（`0.2.0-beta.2`，手动镜像已发布并部署）：品牌组件默认 34px，侧栏 30px、页脚 15px 保持不变；保留原图形。慢速初始化、登录 / 注册 / 首次设置及初始化错误均覆盖 320px、390px 和桌面回归，关联提交 `9d58c26`。

开发实现不等于生产验收：绝区零官方活动接口尚待核实，未公布的版本时间不会猜测；缓存和冷却降低请求压力，但不能保证免于上游风控。以上新增能力包含在本轮 `main` / `develop` 预览与手动镜像中，不包含在 `v0.1.0-beta.3` / `latest` 中。

`0.2.0-beta.1` 隔离验收：全量 `go test -race ./...`、`go vet ./...`、Node 脚本测试、版本 / 仓库检查、前后端构建与 50 项 Playwright 回归通过；覆盖 320px、390px 和桌面布局，首屏资源预算保持不变。Linux amd64 容器已部署，健康、前端资源与匿名账号 / 预约一致性检查通过；打码服务未重启。未验证 ARM 真机或真实账号操作。

`0.2.0-beta.2` 尺寸修复已通过本机同一套完整检查及 53 项 Playwright 回归；新增用受控初始化响应复现慢速加载，不依赖固定延时，首屏资源预算未放宽。镜像来自签名提交 `f3f640d3f5abc836809a07f1316e120fdc99f308`，Docker Hub digest 为 `sha256:8df5879bdde298f7329035b67cca376d57d1328eaa904295394e0e5dd597827d`。部署前停写并成对备份状态 / 密钥，部署后核对健康、前端字节一致性、匿名鉴权和账号 / 预约完整性；仅更新主站，打码容器未重启。

原有私有镜像策略未放宽；当前仓库已关闭 `MIYOHUB_PUBLISH_DEV_IMAGE`。公开源码不会自动公开 GHCR 包，也不会触发生产容器升级。既有签名提交与版本标签保留，不上传本地归档历史。

## 条目完成的条件

实现、回归测试、权限/隐私检查、说明、提交和看板状态缺一不可。需要真实服务或特定设备的项目必须另列验收状态。新增问题补充版本、复现步骤、预期/实际行为和脱敏日志；不提交真实挑战、机器人绑定链接或账号凭据。

## Beta 的真实环境验收

2026-09-13 发布验证：主站 beta.2 的开发镜像在 [CI 34727237905](https://github.com/eleost04/miyohub/actions/runs/34727237905) 完成 amd64 / arm64 发布及私有回读。本机构建升级后健康检查、前端内容校验通过，只读对比确认原账号归属和预约保留；未发起真实账号测试。随后 [Release 34727792073](https://github.com/eleost04/miyohub/actions/runs/34727792073) 检出代理测试夹具的同步竞态；已本地复现并修复，使用 beta.3 重新验证发布，保留 beta.2 标签与失败记录。

2026-09-13 收尾：[beta.3 Release](https://github.com/eleost04/miyohub/releases/tag/v0.1.0-beta.3) 与 [开发镜像 CI](https://github.com/eleost04/miyohub/actions/runs/34729321609) 均通过完整验证。下载的两个 Linux 包与校验和一致，ELF 架构分别为 amd64 / arm64，包含前端与第三方声明。部署在原自动任务结束后进行，成对备份状态和密钥，保留旧镜像；公网健康、前端内容和账号 / 预约一致性检查通过，验证码服务未参与本次升级。观察到的自然调度为无失败、均跳过，不将它算作新的签到或增币成功验收。

2026-09-12 更新：账号所有者反馈原神、星穹铁道、绝区零签到成功，社区签到后的米游币复查确认新增且剩余可得为 0；部署观察确认 QQ 网关上线。以下未覆盖场景继续保留待验收状态，不公开个人记录。

- AUTH-01：由账号所有者完成一次短信发送、实际出现的人机验证和登录；校验发送失败、冷却提示与真实结果一致。
- TASK-01/03：观察一次原有自动任务或本人手动运行，确认修改时间 / 无变化保存不会中断；正常停止仍有效，米游币实际增量以复查结果为准。
- NET-01：管理员填写自己的代理后，验证正常连接和代理不可用时的失败提示；未配置代理时仍直连，不自动启用示例地址。
- PUSH-01：在出现问题的 QQ 客户端重新扫码，确认本站显示网关上线、官方页完成连接；检查只收到自己的任务结果。隔离浏览器测试不等同于鸿蒙真机验收。
- SHOP-01：保留原有预约，由所有者核对下一次兑换的实际轮次、请求次数与米游社记录；自动回归不进行真实扣币测试。

以上验收结果记录版本和脱敏现象，不能将模拟接口通过写成官方服务已验证。通过后再推进正式版；验证码服务保持自己的 `0.0.1` 版本。

## English summary

This board separates implementation from real upstream/device acceptance. The maintainer is promoting the current `0.2.0-beta.2` preview to `main` while retaining its Beta designation; normal development continues on `develop`. Version `0.1.0-beta.3` is published and deployed after complete CI, archive checksum / architecture checks, private image verification and a safe data-preserving rollout. It includes beta.2's concise task notifications, inspectable logs, independent community actions and exchange-window fixes. The beta.2 archive release was stopped by a reproduced test-fixture race; the corrected fixture was released under a new signed tag without changing proxy runtime behavior or moving the old tag. Owner feedback confirms one successful run for Genshin Impact, Honkai: Star Rail and Zenless Zone Zero, plus verified community coin gain; QQ gateway connectivity has been observed. The latest observed scheduled run reported only skipped outcomes, with no failures, and is not counted as new check-in or reward acceptance. This does not validate every upstream challenge, interaction mission, exchange or mobile client. Missing read/like/share missions are skipped by default, not classified as permanently unsupported; the account owner may explicitly enable bounded independent execution. Account groups, encrypted migration, on-demand notes and event calendars with opt-in reminders are implemented in the 0.2.0-beta.2 preview. Their live upstream/device acceptance remains separate. The private development-image switch is off; public Docker Hub previews are published separately.

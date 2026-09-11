# 功能与兼容性看板

基线：`0.0.1`。当前目标：`0.1.0-beta.1`。更新方式与提交规则见 [CONTRIBUTING.md](CONTRIBUTING.md)。

状态含义：**已实现** = 有代码及本地回归，不代表所有真实账号/设备都已验收；**修复中** = 已知问题正在处理；**待对齐** = 需要和参考行为逐项比较；**待验收** = 需要外部服务或实际设备确认；**规划** = 尚未实现。不要用百分比代替可验证条目。

## 当前维护周期

| ID | 类型 | 项目 | 状态 | 验收条件 |
| --- | --- | --- | --- | --- |
| AUTH-01 | fix | 短信登录 Geetest 3/4 兼容 | 已实现，待真实上游验收 | 已覆盖两种挑战、发送与登录恢复、归属/过期/防重放、320px 沙箱与按需加载；需本人完成一次真实验证 |
| TASK-01 | fix | 米游币运行被无变化保存取消 | 已实现，待部署观察 | 无变化保存不递增版本；只改时间/自动开关不打断本次执行；实际任务变更、停用和主动停止保留，日志与推送记录原因 |
| TASK-02 | feat | 增加可配置的状态查询重试 | 已实现，隔离测试通过 | 默认 5 次，可设 0–10；实际次数、可取消退避和 Retry-After 已测试；手机/桌面配置可保存，不重放写请求 |
| NET-01 | feat | HTTP / SOCKS5 出站代理 | 已实现，隔离测试通过 | 管理员弹窗配置；覆盖米游社官方接口；密码加密/不回显；HTTP CONNECT / SOCKS5 / SOCKS5H 握手、错误不回退、私有地址隔离与手机/桌面保存测试通过 |
| SHOP-01 | fix | 对齐 MiyoQian 商品兑换流程 | 待对齐 | 核对角色/地址、开售轮次、校时、准备与重试、错误分类；用模拟接口回归，不实际扣币 |
| PUSH-01 | fix | QQ 官方页面点击连接无响应 | 排查中 | 分别核对站点会话、官方轮询和客户端行为；覆盖会话恢复与错误提示；鸿蒙设备需用户验收 |
| DOC-01 | docs | 面向项目读者的双语说明与维护规则 | 已实现 | README、功能看板、版本/分支/提交/发布规则可直接使用 |

## 已有功能与参考项目差异

| 功能 | MiyoHub 状态 | 实现 / 验证入口 | 参考与边界 |
| --- | --- | --- | --- |
| 米游社扫码、Cookie 绑定 | 已实现，真实风控待验收 | `internal/auth/`、`web/tests/onboarding.spec.mjs` | 参考 MiyoQian / MiyoSign；短信问题单列 AUTH-01 |
| 六款游戏签到、角色排除 | 已实现 | `internal/tasks/game_checkin.go`、`internal/tasks/*_test.go` | 只执行用户选中的项目，无角色则跳过 |
| 云·原神 / 云·绝区零 | 已实现，真实服务待验收 | `internal/tasks/cloud_checkin.go` | 需独立 Combo Token |
| 米游币社区及互动任务 | 已实现，存在 TASK-01 | `internal/tasks/bbs_checkin.go` | 对照 MiyoSign；只执行仍存在的互动任务，收益需复查确认 |
| 个人签到时间、每日去重 | 已实现 | `internal/scheduler/`、`internal/store/task_settings.go` | 个人时间优先；不补跑离线时段 |
| 实物 / 虚拟商品与预约 | 已实现，SHOP-01 待对齐 | `internal/shop/`、`internal/shop/engine_test.go` | 参考 MiyoQian；不承诺库存或兑换成功 |
| 上游时钟与有限兑换重试 | 已实现 | `internal/shop/clock.go`、`internal/shop/exchange.go` | 秒级上游时间；未知结果不盲目重复提交 |
| 多用户隔离、邀请码预授权 | 已实现 | `internal/store/permissions.go`、`internal/api/owner_isolation_test.go` | 管理员在日常页面也只见本人账号 |
| 个人 / 站点验证码服务 | 已实现 | `internal/captcha/`、`internal/api/captcha_probe.go` | 自建服务为可选组件；后台探测有独立冷却 |
| QQ、微信及其他推送渠道 | 已实现，外部渠道待验收 | `internal/notify/`、`web/src/components/PushPanel.vue` | 收件人、渠道额度和有效期遵守官方限制 |
| 移动端弹窗、自动保存、SVG | 已实现 | `web/src/components/`、`web/tests/` | 持续限制首屏资源；密钥不写入浏览器缓存 |
| 游戏实时便笺、活动日历 | 规划 | 尚无 MiyoHub 实现 | MiyoSign 有参考实现；不与当前签到状态混为一谈 |
| 自动上传容器镜像 | 尚未启用 | 构建配置与发布工作流 | 需明确分发范围，且先核实第三方许可 |

参考代码：[MiyoQian](https://github.com/ytf211/MiyoQian)、[MiyoSign](https://github.com/ytf211/miyosign)、[MihoyoBBSTools](https://github.com/Womsxd/MihoyoBBSTools)。接口随上游变化；仅借鉴可验证的行为，不以参考项目能运行替代本项目验收。

## 条目完成的条件

实现、回归测试、权限/隐私检查、说明、提交和看板状态缺一不可。需要真实服务或特定设备的项目必须另列验收状态。新增问题补充版本、复现步骤、预期/实际行为和脱敏日志；不提交真实挑战、机器人绑定链接或账号凭据。

## English summary

This board separates implemented behavior from unresolved bugs and live acceptance. The current cycle targets `0.1.0-beta.1`: Geetest 4 SMS support, task cancellation fixes, configurable read retries, proxy configuration, MiyoQian exchange parity, and QQ official-page connection diagnostics. Game notes/calendars are planned, not implemented. See the table IDs in commits/PRs, and retain explicit live-service/device acceptance requirements.

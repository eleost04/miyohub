# 开发与维护规范

本规范适用于 MiyoHub；验证码服务独立发布，但采用相同的提交、审查和安全规则。功能进度以 [ROADMAP.md](ROADMAP.md) 为准，面向用户的改动维护在 `web/src/releases.json`。

开发入口仍是 `develop`。本轮按维护者要求将 `0.2.0-beta.2` 预览同步到 `main`，保留 Beta 标识，不因此创建正式 Release。首次参与可用 `git clone --branch develop https://github.com/eleost04/miyohub.git`；已有完整克隆先 `git fetch origin develop`，再 `git switch develop`，然后从最新开发分支创建自己的短期分支。

## 一次只交付一个可验证的改动

1. 在看板登记问题、影响范围、目标版本及验收条件。先区分上游限制、环境问题与项目缺陷。
2. 从 `develop` 创建短期分支。修复使用 `fix/<主题>`，新功能使用 `feat/<主题>`，文档使用 `docs/<主题>`，发布收尾使用 `release/<版本>`。
3. 修复先补可复现的失败测试；实现、相关测试和必要说明放在同一个提交。不要把无关重构混入修复。
4. 每个独立功能、修复或文案改动单独 GPG 签名提交，正文记录原因、行为和验证结果。
5. 通过测试后提出 PR，由审查确认权限、兼容性和风险；合入 `develop`。看板与更新日志应与实现同步。
6. 正式发布从经过验收的 `develop` 收尾，经 PR 合入 `main`。发布标签不可移动或覆盖。

`main` 是正式版本分支，`develop` 是下一版集成分支。正常维护不得重写共享分支、绕过检查或上传本地归档历史。仓库管理者应根据 GitHub 套餐配置受保护分支、必需检查及签名要求；此文档不代表这些远端规则已自动开启。

## 提交格式

标题采用 Conventional Commits：`类型(范围): 动词开头的简短说明`。说明可用中文或英文，同一条提交保持一致；避免 `update`、`修改一下` 等无法定位的标题。

| 类型 | 使用场景 | 示例 |
| --- | --- | --- |
| `fix` | 修复已有行为，包括错误提示和兼容性 | `fix(auth): support Geetest 4 SMS challenges` |
| `feat` | 新增用户可用能力 | `feat(network): add SOCKS5 proxy settings` |
| `perf` | 性能改进，不改变功能语义 | `perf(web): lazy-load account editors` |
| `refactor` | 内部结构调整，不声称修复或新增能力 | `refactor(shop): separate reservation validation` |
| `docs` | README、看板、维护说明 | `docs(roadmap): record exchange acceptance criteria` |
| `test` | 仅测试变化 | `test(tasks): cover cancellation during a state retry` |
| `build` / `ci` | 构建、依赖及持续集成 | `ci(release): verify signed tags before publishing` |
| `chore` / `revert` | 版本收尾或明确的回退 | `chore(release): prepare 0.1.0-beta.1` |

正文至少包含：问题或目的、具体变化、验证命令及结果、兼容性或剩余风险。不要把账号、手机号、Cookie、SToken、代理密码、机器人凭据、二维码链接或真实验证码写进提交。

```text
fix(tasks): preserve running tasks on unchanged settings

Why: An unchanged save must not cancel an in-flight task.
Change: Make equivalent settings idempotent and retain explicit cancellation.
Validation: Add a regression test; run the affected Go test packages.
Risk: Actual permission revocation must still stop further requests.
```

使用 `git commit -S`；发布使用 `git tag -s`。本地用 `git verify-commit HEAD` / `git verify-tag <标签>` 检查，推送后检查 GitHub 的 Verified 状态。不要为方便发布关闭签名检查。

## 版本规则

采用 SemVer。应用版本唯一来源为 `internal/buildinfo/VERSION`，同步前端包、锁文件与更新日志；运行 `node scripts/check-version.mjs` 校验。

| 改动 | 版本示例 |
| --- | --- |
| 仅兼容性修复 | `0.0.1` → `0.0.2` |
| 新增兼容功能，如代理配置 | `0.0.2` → `0.1.0` |
| 开发验收 | `0.1.0-beta.1`、`0.1.0-beta.2` |
| 功能冻结后的候选版 | `0.1.0-rc.1` |
| 验收通过的正式版 | `0.1.0` |

`0.x` 的不兼容变更至少递增次版本并提供迁移说明；`1.0.0` 之后不兼容变更递增主版本。不是每次提交都改版本号；一个发布周期统一版本。新增功能与修复同时交付时，采用其中最高级别的版本递增。

当前稳定基线为 `0.0.1`；`main` / `develop` 的本轮预览为 `0.2.0-beta.2`，已手动提供 Docker Hub 镜像，最新 GitHub 发布标签仍为 `0.1.0-beta.3`。历史 `0.1.0-beta.2` 标签与镜像保留，因 Release 暴露隔离代理测试的竞态，修复已通过新版本的完整验证；不覆盖旧标签，也不将测试夹具修复表述为代理业务修复。实际安装包状态以 GitHub Releases 为准。真实短信风控和客户端兼容性验收后再推进正式版。验证码服务只有自身代码或协议变化时才递增版本，不跟随主站机械改号。

## 验证门槛

后端：`go test -race ./...`、`go vet ./...`。流程与仓库边界：`node --test scripts/*.test.mjs`、`node scripts/check-version.mjs`、`node scripts/check-repository.mjs`。前端：`npm --prefix web ci`、`npm --prefix web run build`，然后在 `web` 目录运行 `npx playwright test`。

CI 是验证流水线，不是提交类型或目标分支：用户功能使用 `feat`，修复用 `fix`，文档用 `docs`；只有修改工作流才用 `ci`。每个小改动本地验证成功后立即签名提交并推送自己的特性分支，准备集成时提出面向 `develop` 的 PR。`main` / `develop` 的推送与 PR 保留自动 CI；纯文档改动只运行仓库、版本与凭据检查，手动 CI 始终运行完整验证。

不提供 GitHub 签到工作流，不将账号状态、Cookie 或加密密钥放入 Actions Secrets。签到、兑换和推送仅由自有主机运行；CI / Release 使用隔离模拟数据。修改工作流时固定第三方 Action 的完整提交 SHA，并核对来源，保持最小权限，不使用 `pull_request_target` 执行贡献者代码。

浏览器测试使用独立临时状态及原有鉴权，不关闭生产限流，不复用生产账号。至少覆盖 320/390 像素手机及桌面布局，检查弹窗、返回、自动保存、键盘操作和首屏资源预算。

外部接口使用保存结构的脱敏夹具或本地替身；不能为了测试发送真实短信、扣费打码、签到、兑换或通知。真实验收必须得到账号所有者授权，使用最小必要次数，并在看板区分「代码已验证」与「上游/设备已验收」。

## 发布、升级与回退

- Beta/RC 标签指向 `develop` 的已验证提交；正式标签必须属于 `main`。签名、版本、分支归属和测试任一未通过，不生成 Release。
- 发布前更新 README 的使用变化、看板状态和更新日志。更新日志只链接当前发布历史内的真实提交，不虚构编号，不把待验收项写成已完成。
- Actions 支持手动运行 `Build and test`；恢复发布时运行 `Publish signed release`，填写已存在的签名标签。禁止借恢复流程跳过检查或覆盖已有版本。
- 发布检查失败时先定位原因。源码或测试需要修复就使用新版本与新签名标签；只有确认是环境 / 下载等临时故障时才重跑原标签，不能反复重跑来掩盖竞态或断言失败。
- 部署前避开运行中的任务及临近兑换，成对备份加密状态和密钥，保留旧镜像；升级不使用 `docker compose down -v`。
- 仅推送明确的分支与标签，不使用 `--all` / `--mirror`。开发镜像由 `develop` 的完整 CI 通过后发布，需显式启用 `MIYOHUB_PUBLISH_DEV_IMAGE`；保留私有仓库 / 包校验及源码签名门槛。PR 不发布镜像，不上传运行数据或验证码模型；修改分发范围需再次核实授权。当前仓库关闭了该私有镜像开关，公开 Docker Hub 镜像单独发布，不自动调整 GHCR 包可见性。
- 开发镜像使用 `develop` 和完整提交 SHA 标签；严格锁定用 Actions 记录的 digest。连续 `develop` 流水线串行完成，避免旧镜像晚于新镜像覆盖跟踪标签；普通 PR 可取消过期 CI。

## 安全与工程边界

站点用户是数据归属边界；管理员权限不等于可读取其他用户凭据。关闭账号、撤销权限和主动停止必须继续生效。网络重试必须区分只读查询、明确拒绝和结果不确定的写操作，不盲目重放兑换。

运行数据、密钥、日志、模型、验证码图片、截图、备份与 `docs/` 临时资料不得提交。长期维护文档放在仓库根目录或 `.github/`。引用项目时记录来源和行为差异，不移植不明许可的代码或弱化原有安全检查。

## English maintainer summary

Clone `develop` for contribution (`git clone --branch develop https://github.com/eleost04/miyohub.git`); a normal clone defaults to `main`, which currently carries the maintainer-authorized `0.2.0-beta.2` preview without relabeling it as a stable release. Use focused branches and signed Conventional Commits (`fix`, `feat`, `docs`, etc.) with a body explaining the change and tests. Push each verified change to its topic branch, then integrate through a PR to `develop`; CI is the validation pipeline, not the commit type or target branch. Release stable versions from `main`. SemVer patch versions fix bugs, minor versions add features, and `-beta.N` / `-rc.N` mark pre-releases. Batch validated changes into a version; the latest GitHub release tag is `0.1.0-beta.3`, while `0.2.0-beta.2` is available as a separately published Docker Hub preview. The beta.2 tag was retained after its release-gate failure, and the fixture fix passed verification under a new signed tag; never hide a race by retrying until green.

Keep the roadmap and real commit-linked changelog current. Run isolated Go/browser tests and privacy checks; never use live accounts for automated tests. Verify signed tags before releasing, never rewrite published release tags, and preserve paired state/key backups for rollback. Production data and third-party models are never repository contents.

# MiyoHub 贡献与开发指南

感谢你对 MiyoHub 的关注与支持！我们欢迎任何形式的贡献，包括报告缺陷、提交改进建议以及提交代码 Pull Request。

为了保持项目代码质量、安全边界与提交历史的清晰，请在参与前阅读本指南。

---

## 🌿 分支工作流

- **`main`**：稳定发行分支。生产环境建议使用此分支的代码或带有 GPG 签名的发布标签（Release Tags）。
- **`develop`**：日常开发与集成分支。**所有的开发、功能 PR 与 Bug 修复均应基于 `develop` 分支进行**。
- **特性分支（Topic Branches）**：从 `develop` 切出，格式推荐：
  - 新功能：`feat/功能名`
  - 问题修复：`fix/问题名`
  - 文档改进：`docs/主题`

```bash
# 获取开发分支并创建自己的工作分支
git clone --branch develop https://github.com/eleost04/miyohub.git
cd miyohub
git switch -c feat/my-new-feature
```

---

## 📝 提交信息规范 (Conventional Commits)

本项目严格遵循 [Conventional Commits](https://www.conventionalcommits.org/) 规范，确保更新日志可自动追踪。

### 提交格式
```text
<类型>(<影响范围>): <简短描述>

[可选详细说明：原因、变更行为与验证方式]
```

### 常用类型表

| 类型 | 使用场景 | 示例 |
| :--- | :--- | :--- |
| `feat` | 新增功能与用户可见特性 | `feat(network): add SOCKS5 proxy support` |
| `fix` | 修复已有行为、兼容性或缺陷 | `fix(auth): support Geetest 4 SMS challenges` |
| `perf` | 性能优化（不改变功能语义） | `perf(web): lazy-load account editors` |
| `refactor` | 代码重构（不增加新功能也不修复 Bug） | `refactor(shop): extract reservation validator` |
| `docs` | 仅文档或看板改动 | `docs: polish README and contribution guide` |
| `test` | 增加或修正测试用例 | `test(tasks): add state retry timeout test` |
| `ci` / `build` | 构建系统、依赖更新或 CI 工作流调整 | `ci: optimize multi-arch docker build` |
| `chore` | 日常杂项或版本准备 | `chore(release): prepare 0.1.0-beta.3` |

### GPG 签名要求
为保证代码真实性与溯源安全，所有合并入主干的提交**强烈建议带有 GPG 签名**（`git commit -S`）。

---

## 🧪 本地测试与验证

提交 PR 前，请确保相关测试在本地顺利通过：

### 1. 后端代码验证 (Go)
```bash
# 运行单元测试与竞态检测
go test -race ./...

# 静态代码检查
go vet ./...
```

### 2. 前端代码验证 (Vue / TypeScript)
```bash
cd web
npm install
npm run build

# 运行 Playwright 浏览器端自动化回归
npx playwright test
```

### 3. 版本与仓库边界检查
```bash
# 检查版本号一致性
node scripts/check-version.mjs

# 检查仓库防泄漏边界
node scripts/check-repository.mjs
```

---

## 🔒 核心安全与红线

在提交代码时，请严格遵守以下安全红线：

1. **绝对禁止泄露任何凭据**：严禁将真实 Cookie、SToken、密码、短信验证码、手机号或私有 API Token 提交至代码、测试用例或注释中；
2. **防 SSRF 保护**：出站请求必须经过安全校验，默认禁止容器主动探测或连接未经验证的内网/私有网段；
3. **数据原子与并发保护**：所有持久化状态修改必须持有 `internal/processlock` 文件锁，杜绝多进程写入竞态；
4. **架构分层隔离**：第三方及米游社官方 HTTP 接口必须封装在 `internal/mihoyo` 领域服务内，严禁在外部 HTTP Handler 中直接硬编码调用。

---

## 📦 版本命名规范

本项目采用语义化版本 [SemVer](https://semver.org/)：
- **`v0.x.y`**：开发与迭代阶段；
- **`-beta.x` / `-rc.x`**：候选测试版本；
- 正式版发布从经过完整测试的 `develop` 经 PR 合入 `main`，并打上不可篡改的签名 Tag。

感谢你的贡献，一起让 MiyoHub 变得更好！

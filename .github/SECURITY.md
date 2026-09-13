# 安全政策 / Security policy

## 报告安全问题

权限绕过、跨用户数据访问、Cookie / Token 泄露、SSRF、邀请码越权等问题请勿在普通 Issue、PR、提交或截图中披露细节。

如果仓库的 **Security → Advisories** 页面提供 **Report a vulnerability**，请使用该私密入口。此功能受仓库可见性、GitHub 套餐和维护者设置影响；本文档不表示私密漏洞上报已经启用。

如果没有该入口，请只创建内容为“请求安全问题私密上报渠道”的 Issue，或通过已确认的维护者联系方式提出请求。不要先附漏洞步骤、攻击代码、日志或账号数据；等待维护者确认私密渠道后再提交。不要将任意第三方自称的联系方式视为维护者授权。

私密报告建议包含：受影响版本 / 提交、问题类型、最小复现、预期权限边界、影响与可能修复。仅用自己的隔离测试账号；无需提供生产凭据，也不要测试其他用户或第三方站点。

## 凭据可能泄露时

- 立即在凭据所属平台退出会话、撤销或轮换对应 Token / Cookie / 密码 / Webhook；仅删除 Issue 或提交不能撤销已经泄露的凭据。
- 通知站点维护者，限制受影响的账号、渠道和入口；保留脱敏的调查证据。
- 加密状态与 `state.json.key` 必须分权限妥善保管；不要上传 GitHub、Actions 日志 / 产物、Issue 或公开网盘。
- 脱敏不只删除 Cookie：二维码、扫码绑定 URL、验证码参数、代理密码、邮件凭据、手机号、游戏 UID 和收货地址也需要检查。

## 维护范围

优先维护最新稳定 Release 与最新 Beta / RC。报告中请给出确切版本；旧版本建议先在备份后升级或在隔离环境复现。不承诺固定响应期限或对所有历史版本回补。上游官方服务、第三方验证码服务和推送平台自身的问题应通过对应服务的安全渠道报告。

## Reporting in English

Do not disclose authorization bypasses, cross-user data access, credential exposure, SSRF or invitation escalation in regular issues, PRs, commits or screenshots. Use **Security → Advisories → Report a vulnerability** when available. Availability depends on GitHub and repository settings; this document does not claim it is enabled.

If unavailable, open an issue containing only a request for a private security contact, with no exploit details or account data, and wait for a maintainer-confirmed private channel. Provide the affected version, minimal isolated reproduction, expected security boundary and impact. Test only accounts and systems you control with permission.

Revoke or rotate exposed credentials at their issuer immediately; deleting a post or rewriting history does not revoke them. Never upload production state/key pairs, QR links or personal data. The latest stable and pre-release versions receive priority; no fixed response SLA or universal backport guarantee is implied.

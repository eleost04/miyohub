import type { PushChannel, PushProvider, PushSecretKey } from './types'

export type PushTextKey = Exclude<keyof PushChannel, 'id' | 'provider' | 'enable' | 'smtp_port' | 'smtp_ssl' | 'configured'>
export type PushField = { key: PushTextKey; label: string; placeholder?: string; required?: boolean }
export type PushProviderInfo = { key: PushProvider; name: string; mark: string; tone: string; hint: string; fields: PushField[] }
export const secretKeys: PushSecretKey[] = ['token', 'webhook', 'secret', 'api_url', 'client_secret', 'push_url', 'access_token', 'smtp_password', 'context_token', 'sync_cursor']
const token: PushField = { key: 'token', label: 'Token', required: true }
const webhook: PushField = { key: 'webhook', label: 'Webhook 地址', placeholder: 'https://…', required: true }
const signature: PushField = { key: 'secret', label: '签名密钥（可选）' }
export const pushProviders: PushProviderInfo[] = [
  { key: 'pushplus', name: 'PushPlus', mark: 'P+', tone: 'sage', hint: '使用 PushPlus Token；群组 Topic 可选。不填 Topic 时发送给 Token 所属用户。', fields: [token, { key: 'topic', label: '群组 Topic（可选）' }] },
  { key: 'telegram', name: 'Telegram', mark: 'TG', tone: 'blue', hint: '先与机器人发起对话，或将它加入目标群组。默认使用官方 API，可配置自建代理基础地址。', fields: [{ ...token, placeholder: '机器人编号:密钥' }, { key: 'chat_id', label: 'Chat ID', placeholder: '用户或群组 ID', required: true }, { key: 'api_url', label: 'API 基础地址（可选）', placeholder: '默认 https://api.telegram.org' }] },
  { key: 'wxpusher', name: 'WxPusher', mark: 'WX', tone: 'sage', hint: '填写应用的 AppToken 和订阅用户 UID；用户需已关注并订阅对应应用。', fields: [{ ...token, label: 'AppToken' }, { key: 'openid', label: '用户 UID', placeholder: 'UID_…', required: true }] },
  { key: 'dingrobot', name: '钉钉机器人', mark: '钉', tone: 'blue', hint: '支持加签。若机器人开启关键词校验，请将 MiyoHub 添加为允许的关键词。', fields: [webhook, signature] },
  { key: 'feishubot', name: '飞书机器人', mark: '飞', tone: 'blue', hint: '使用群聊自定义机器人的 Webhook，支持签名密钥。关键词校验可使用 MiyoHub。', fields: [webhook, signature] },
  { key: 'qqbot', name: 'QQ 官方机器人', mark: 'QQ', tone: 'blue', hint: '推荐使用 QQ 扫码，在官方页面创建或选择机器人并自动绑定。也可手动填写 AppID、密钥与 OpenID（不是 QQ 号）。主动消息受官方权限与额度限制。', fields: [{ key: 'app_id', label: 'AppID', required: true }, { key: 'client_secret', label: 'ClientSecret', required: true }, { key: 'openid', label: '用户 OpenID', required: true }] },
  { key: 'qq', name: 'QQ · OneBot', mark: 'OB', tone: 'sand', hint: '连接自建 OneBot HTTP 服务。填写基础地址，不含 /send_msg；请使用访问令牌保护服务。', fields: [{ key: 'push_url', label: 'OneBot 基础地址', placeholder: 'http://127.0.0.1:3000', required: true }, { key: 'access_token', label: 'Access Token', required: true }, { key: 'send_id', label: '接收 QQ 号 / 群号', required: true }] },
  { key: 'email', name: '电子邮件', mark: 'M', tone: 'sand', hint: '优先使用邮箱服务提供的授权码。仅支持加密传输：隐式 TLS 或 STARTTLS，不支持明文降级。', fields: [{ key: 'smtp_host', label: 'SMTP 主机', placeholder: 'smtp.example.com', required: true }, { key: 'smtp_user', label: 'SMTP 用户名', placeholder: '通常为发件邮箱' }, { key: 'smtp_password', label: 'SMTP 授权码 / 密码' }, { key: 'mail_from', label: '发件邮箱（可选）', placeholder: '默认使用 SMTP 用户名' }, { key: 'mail_to', label: '收件邮箱', placeholder: '多个邮箱以英文逗号分隔，最多 10 个', required: true }] },
  { key: 'wechat_claw', name: '微信 · 扫码直连', mark: '微', tone: 'sage', hint: '通过微信 iLink 扫码连接。绑定后先给机器人发送一条消息；会话失效时按提示重新发送消息或扫码。只保存通知所需会话信息，不保存聊天正文。', fields: [webhook, { key: 'token', label: 'Bearer Token（桥接可选）' }] },
  { key: 'webhook', name: '通用 Webhook', mark: '</>', tone: 'sand', hint: '向自定义地址发送 JSON，可选 Bearer Token。HTTP 2xx 且没有明确的 JSON 失败标记时，记录为服务已接收。请只使用可信地址，公网连接建议 HTTPS。', fields: [webhook, { key: 'token', label: 'Bearer Token（可选）' }] },
]

export function providerInfo(key: PushProvider): PushProviderInfo {
  return pushProviders.find(provider => provider.key === key) || { key, name: '未知渠道', mark: '?', tone: 'sand', hint: '此渠道类型不受支持，请删除后新建。', fields: [] }
}
export function emptyChannel(provider: PushProvider): PushChannel {
  return { id: '', name: providerInfo(provider).name, provider, enable: false, token: '', webhook: '', secret: '', chat_id: '', api_url: '', app_id: '', client_secret: '', openid: '', topic: '', push_url: '', access_token: '', send_id: '', msg_type: 'private', smtp_host: '', smtp_port: 465, smtp_user: '', smtp_password: '', mail_from: '', mail_to: '', smtp_ssl: true, mode: provider === 'wechat_claw' ? 'ilink' : '', bot_id: '', context_token: '', sync_cursor: '', binding_state: '', configured: [] }
}

export interface AuthStatus { need_auth: boolean; has_admin: boolean; registration_mode: string }
export interface Bootstrap { auth: AuthStatus; user: User | null; config?: Config; status?: Status; version?: string }
export interface User { id: string; username: string; role: string; status: string; permissions: { exchange: boolean; site_captcha: boolean }; onboarding_status?: 'pending' | 'dismissed' | 'complete' }
export interface InviteCode { code: string; created_at: string; expires_at?: string; max_uses: number; used_count: number; note?: string; created_by?: string; disabled: boolean; permissions?: User['permissions']; use_site_captcha?: boolean }
export interface TaskSummary { success: number; failed: number; skipped: number; status?: string; reason?: string; details?: string[] }
export interface TaskProgress { account_id: string; state: string; current: string; started_at: string }
export interface TaskRunSelection { account_ids: string[]; games_only?: boolean; bbs_only?: boolean; games?: string[] }
export interface LoginState { running: boolean; status: string; qr_url: string; error: string; account: string; account_id?: string }
export interface SMSChallenge { id: string; version?: 3 | 4; gt: string; challenge?: string; risk_type?: string; session_id?: string; new_captcha: boolean; expires_at: string; operation: 'send' | 'verify' }
export type SMSCaptchaSolution = { id: string } & ({ challenge: string; validate: string } | { captcha_id: string; lot_number: string; captcha_output: string; pass_token: string; gen_time: string })
export interface SMSState { status: 'idle' | 'sending' | 'captcha_required' | 'sent' | 'verifying' | 'verified' | 'failed'; message: string; phone: string; retry_at: string; expires_at: string; challenge?: SMSChallenge }
export interface Account { id: string; name: string; user_id: string; stuid: string; disabled: boolean; status: string; checked_at: string; has_cookie: boolean; has_stoken: boolean; cloud_configured: string[]; task_results: Record<string, TaskSummary>; last_task_at: string; task_settings: AccountTaskSettings; exchange_allowed: boolean }
export interface AccountTaskSettings { revision: number; automatic: boolean; schedule?: { time: string; timezone: string } | null; features: Config['features']; games: Config['games']; cloud_games: Config['cloud_games']; bbs: Config['bbs'] }
export interface ExchangePlan { state: string; phase?: string; attempt: number; revision: number; price: number; goods_type: number; id: string; enable: boolean; auto: boolean; account_id: string; goods_id: string; goods_name: string; device_fp: string; uid: string; region: string; game_biz: string; address_id: string; exchange_at: number; last_result: string; last_run: string }
export interface CaptchaChannel {
  id: string; provider: 'damagou' | 'custom'; enable: boolean; userkey: string; type: string; timeout: number
  endpoint: string; token: string; use_v3_model?: boolean; configured?: string[]; clear_token?: boolean
}
export interface CaptchaAttempt { at: string; source: string; provider: string; channel_id: string; kind: string; ok: boolean; code: string; duration_ms: number }
export interface CaptchaProbe { id: string; status: 'running' | 'succeeded' | 'failed' | 'interrupted'; started_at: string; finished_at?: string; retry_at: string; duration_ms: number; message: string }
export interface CaptchaSettings { source: 'off' | 'personal' | 'site'; revision: number; max_retries: number; channels: CaptchaChannel[]; site_allowed: boolean; site_available: boolean; activity?: CaptchaAttempt[] }
export interface ProxySettings { enable: boolean; url: string; username: string; password: string; has_password?: boolean; clear_password?: boolean }
export interface Config {
  enabled: boolean
  accounts: Account[]
  features: { game_checkin: boolean; cloud_game_checkin: boolean; bbs_tasks: boolean }
  games: { enabled: string[]; black_list: Record<string, string[]> }
  cloud_games: { enabled: string[] }
  bbs: { forums: number[]; checkin: boolean; read: boolean; like: boolean; share: boolean; cancel_like: boolean; post_limit: number; delay_seconds: number[] }
  network: { bbs_state_retries?: number; proxy: ProxySettings }
  schedule: { enable: boolean; time: string; timezone: string; jitter_minutes: number; run_on_start: boolean }
  push: { error_only: boolean; channels: Array<{ provider: string; enable: boolean }> }
  captcha: { max_retries: number; channels: CaptchaChannel[] }
  shop_exchange: { enable: boolean; retry_seconds: number; retry_interval: number; plans: ExchangePlan[] }
}
export interface LogEntry { at: string; component: string; message: string }
export interface SchedulerStatus {
  enabled: boolean
  running: boolean
  next_run: string
  last_run: string
  last_error: string
  schedule: Config['schedule']
}
export interface ExchangeStatus { enabled: boolean; running: number; next_run: number; server_time: string; clock: { offset_ms: number; rtt_ms: number; synced_at: string; error: string; source?: string; uncertainty_ms?: number } }
export interface Status { user?: User; running: boolean; logs: LogEntry[]; accounts?: Account[]; tasks?: TaskProgress[]; scheduler?: SchedulerStatus; exchange?: Config['shop_exchange']; exchange_scheduler?: ExchangeStatus }
export interface ShopGame { key: string; name: string }
export interface ShopGood {
  type: number
  requires_address: boolean
  requires_role: boolean
  goods_id: string
  goods_name: string
  price: number
  icon: string
  game_biz: string
  stock: string
  sold_out: boolean
  exchange_timestamp: number
  exchange_time: string
  time_needs_detail?: boolean
  display_status: string
  limit: string
}
export interface ShopAddress { id: string; name: string; phone: string; address: string }
export interface ShopRole { uid: string; region: string; nickname: string; level: string; region_name: string }
export interface ShopResult { games: ShopGame[]; goods: ShopGood[] }
export type PushProvider = 'pushplus' | 'telegram' | 'wxpusher' | 'dingrobot' | 'feishubot' | 'qqbot' | 'qq' | 'email' | 'wechat_claw' | 'webhook'
export type PushSecretKey = 'token' | 'webhook' | 'secret' | 'api_url' | 'client_secret' | 'push_url' | 'access_token' | 'smtp_password' | 'context_token' | 'sync_cursor'
export interface PushChannel {
  id: string; name: string; provider: PushProvider; enable: boolean
  token: string; webhook: string; secret: string; chat_id: string; api_url: string
  app_id: string; client_secret: string; openid: string; topic: string
  push_url: string; access_token: string; send_id: string; msg_type: string
  smtp_host: string; smtp_port: number; smtp_user: string; smtp_password: string; mail_from: string; mail_to: string; smtp_ssl: boolean
  mode: string; bot_id: string; context_token: string; sync_cursor: string; binding_state: string
  binding_error?: string
  configured: PushSecretKey[]
}
export interface PushSettings { enable: boolean; tasks: boolean; exchange: boolean; error_only: boolean; revision: number; channels: PushChannel[] }
export interface PushResult { channel_id: string; name: string; provider: PushProvider; ok: boolean; error?: string; uncertain?: boolean }
export interface PushTestResult { all_ok: boolean; results: PushResult[] }
export interface PushBindingState { session_id: string; provider: PushProvider; channel_id: string; revision: number; running: boolean; status: string; qr_image: string; qr_url: string; message: string; expires_at: string }
export interface PushDelivery { id: string; channel_id: string; channel_name: string; provider: PushProvider; kind: string; title: string; status: string; error?: string; created_at: string; updated_at: string }

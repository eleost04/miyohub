<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import AccountsPanel from './components/AccountsPanel.vue'
import ActivityPanel from './components/ActivityPanel.vue'
import AppIcon from './components/AppIcon.vue'
import BrandMark from './components/BrandMark.vue'
import ModalShell from './components/ModalShell.vue'
import PageLoading from './components/PageLoading.vue'
import PageLoadError from './components/PageLoadError.vue'
import { api, resetApiSession } from './api'
import { formatDate } from './time'
import { accountTasks, hasTasks } from './taskSettings'
import { changePage, initHistory } from './history'
import { appVersion } from './version'
import type { Account, AuthStatus, Bootstrap, Config, Status, TaskRunSelection, User } from './types'

const pageLoadOptions = { loadingComponent: PageLoading, errorComponent: PageLoadError, delay: 150, timeout: 30000 }
const AccountTasksModal = defineAsyncComponent({ ...pageLoadOptions, loader: () => import('./components/AccountTasksModal.vue') })
const RunTasksModal = defineAsyncComponent({ ...pageLoadOptions, loader: () => import('./components/RunTasksModal.vue') })
const BindAccountModal = defineAsyncComponent({ ...pageLoadOptions, loader: () => import('./components/BindAccountModal.vue') })
const SettingsPanel = defineAsyncComponent({ ...pageLoadOptions, loader: () => import('./components/SettingsPanel.vue') })
const PushPanel = defineAsyncComponent({ ...pageLoadOptions, loader: () => import('./components/PushPanel.vue') })
const CaptchaPanel = defineAsyncComponent({ ...pageLoadOptions, loader: () => import('./components/CaptchaPanel.vue') })
const ShopPanel = defineAsyncComponent({ ...pageLoadOptions, loader: () => import('./components/ShopPanel.vue') })
const AdminPanel = defineAsyncComponent({ ...pageLoadOptions, loader: () => import('./components/AdminPanel.vue') })
const ProfilePanel = defineAsyncComponent({ ...pageLoadOptions, loader: () => import('./components/ProfilePanel.vue') })
const OnboardingGuide = defineAsyncComponent({ ...pageLoadOptions, loader: () => import('./components/OnboardingGuide.vue') })
const ChangelogPanel = defineAsyncComponent({ ...pageLoadOptions, loader: () => import('./components/ChangelogPanel.vue') })

const auth = ref<AuthStatus | null>(null), user = ref<User | null>(null), config = ref<Config | null>(null)
const serverVersion = ref('')
const status = ref<Status>({ running: false, logs: [] })
const mode = ref<'login' | 'setup' | 'register'>('login')
type View = 'dashboard' | 'shop' | 'notifications' | 'captcha' | 'logs' | 'settings' | 'profile' | 'admin' | 'changelog'
const view = ref<View>('dashboard')
const navigation = computed(() => [
  { key: 'dashboard' as View, icon: 'home', label: '任务总览', group: '任务管理' },
  { key: 'shop' as View, icon: 'gift', label: '商品兑换', group: '任务管理' },
  { key: 'logs' as View, icon: 'activity', label: '运行日志', group: '任务管理' },
  { key: 'notifications' as View, icon: 'bell', label: '消息推送', group: '个人服务' },
  { key: 'captcha' as View, icon: 'shield', label: '打码服务', group: '个人服务' },
  { key: 'profile' as View, icon: 'user', label: '个人账号', group: '个人服务' },
  ...(user.value?.role === 'admin' ? [{ key: 'settings' as View, icon: 'settings', label: '系统设置', group: '站点管理' }, { key: 'admin' as View, icon: 'user', label: '后台管理', group: '站点管理' }] : []),
  { key: 'changelog' as View, icon: 'activity', label: '更新日志', group: '项目信息' },
])
const mobileNavigation = computed(() => navigation.value.filter(item => ['dashboard', 'shop', 'notifications'].includes(item.key)))
const moreItems = computed(() => navigation.value.filter(item => !mobileNavigation.value.some(primary => primary.key === item.key)))
const pageDescriptions: Record<View, string> = { dashboard: '查看账号状态，配置并执行签到任务。', shop: '浏览米游币商品，管理兑换预约与结果。', notifications: '配置签到、兑换结果的通知渠道。', captcha: '选择个人打码渠道或已获授权的站点服务。', logs: '查看任务执行记录与异常原因。', settings: '管理站点运行、每日调度与公共基础服务。', profile: '管理站点登录账号与密码。', admin: '管理用户、服务权限与邀请码。', changelog: '查看版本、功能改动和关联提交。' }
const pushPanel = ref<InstanceType<typeof PushPanel> | null>(null), captchaPanel = ref<InstanceType<typeof CaptchaPanel> | null>(null), settingsPanel = ref<InstanceType<typeof SettingsPanel> | null>(null)
const moreNavigation = ref(false), pendingView = ref<View | null>(null)
const taskAccount = ref<Account | null>(null), selectTaskAccount = ref(false)
function preferencePanel() { return view.value === 'notifications' ? pushPanel.value : view.value === 'captcha' ? captchaPanel.value : view.value === 'settings' ? settingsPanel.value : null }
function pageDirty() { return preferencePanel()?.dirty || false }
async function navigate(next: View) {
  moreNavigation.value = false
  if (['admin', 'settings'].includes(next) && user.value?.role !== 'admin') return
  if (next !== view.value && pageDirty() && !await preferencePanel()?.flush()) { pendingView.value = next; return }
  await changePage(next)
  view.value = next
}
async function leavePush() { const next = pendingView.value; pendingView.value = null; if (next) { await changePage(next); view.value = next } }
const disposeHistory = initHistory(next => {
  if (!navigation.value.some(item => item.key === next)) return false
  if (next !== view.value && pageDirty()) { pendingView.value = next as View; return false }
  moreNavigation.value = false; view.value = next as View; return true
})
const form = ref({ username: '', password: '' })
const inviteCode = ref(new URLSearchParams(window.location.search).get('invite') || new URLSearchParams(window.location.search).get('invite_code') || '')
const busy = ref(false), booting = ref(true), connected = ref(true), error = ref(''), notice = ref('')
const refreshing = ref(false)
const showLogin = ref(false), binding = ref<Account | null>(null)
const showTaskSelection = ref(false)
const showGuide = ref(false)
let guidedUserID = ''
const timezone = computed(() => config.value?.schedule.timezone || 'Asia/Shanghai')
const automaticAccounts = computed(() => config.value?.accounts.filter(a => !a.disabled && accountTasks(a).automatic && hasTasks(a)) || [])
const scheduledAccounts = computed(() => automaticAccounts.value.filter(a => accountTasks(a).schedule || config.value?.schedule.enable))
const nextCheckinTime = computed(() => {
  const value = status.value.scheduler?.next_run
  if (!config.value?.enabled || !scheduledAccounts.value.length || !value || value.startsWith('0001')) return '--:--'
  return new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit', hourCycle: 'h23', timeZone: timezone.value }).format(new Date(value))
})
const metrics = computed(() => [
  { label: '已绑定账号', value: config.value?.accounts.length ?? 0, tone: 'blue' },
  { label: '每日签到', value: status.value.scheduler?.enabled && automaticAccounts.value.length ? (status.value.scheduler.running ? '执行中' : '已安排') : '手动执行', tone: 'sand' },
  { label: '待执行兑换', value: config.value?.shop_exchange.plans.filter(p => p.enable && ['pending', 'running'].includes(p.state)).length ?? 0, tone: 'pink' },
  { label: '任务状态', value: status.value.running ? '进行中' : '已就绪', tone: 'green' },
])
let pollTimer: number | undefined, polling = false
function applyStatus(next: Status) {
  const finished = status.value.running && !next.running
  status.value = next
  if (next.user) { user.value = next.user; if (user.value.role !== 'admin' && ['settings', 'admin'].includes(view.value)) view.value = 'dashboard' }
  if (config.value && next.accounts) config.value.accounts = next.accounts
  if (config.value && next.exchange) config.value.shop_exchange = next.exchange
  connected.value = true
  if (finished) notice.value = '本轮任务已结束，账号结果已更新。'
}
function applyWorkspace(data: Bootstrap) {
  if (!data.user || !data.config || !data.status) throw new Error('初始化数据不完整，请重新加载。')
  serverVersion.value = data.version || ''
  auth.value = data.auth; config.value = data.config; applyStatus({ ...data.status, user: data.user })
  if (guidedUserID !== data.user.id) { guidedUserID = data.user.id; showGuide.value = data.user.onboarding_status === 'pending' }
}
async function refresh() {
  const data = await api<Bootstrap>('/api/v1/bootstrap')
  if (!data.user) { window.dispatchEvent(new Event('miyohub:unauthorized')); throw new Error('登录已过期，请重新登录。') }
  applyWorkspace(data)
}
async function safeRefresh() {
  if (refreshing.value) return
  refreshing.value = true
  try { await refresh(); error.value = '' }
  catch (e) { connected.value = false; error.value = e instanceof Error ? e.message : '刷新失败' }
  finally { refreshing.value = false }
}
function stopPolling() { window.clearTimeout(pollTimer); pollTimer = undefined }
function beginPolling() { stopPolling(); pollTimer = window.setTimeout(poll, 1500) }
async function poll() {
  if (!user.value || polling) return
  polling = true
  try { applyStatus(await api<Status>('/api/v1/status')) }
  catch { if (user.value) connected.value = false }
  finally {
    polling = false
    if (user.value) { stopPolling(); pollTimer = window.setTimeout(poll, document.hidden ? 15000 : status.value.running ? 1500 : 5000) }
  }
}
async function loadAuth() {
  try {
    const data = await api<Bootstrap>('/api/v1/bootstrap')
    auth.value = data.auth
    if (!auth.value.has_admin) { mode.value = 'setup'; return }
    if (inviteCode.value || window.location.pathname === '/register') mode.value = 'register'
    if (!data.user) { user.value = null; return }
    applyWorkspace(data)
    const requested = window.location.hash.slice(1)
    if (!showGuide.value && navigation.value.some(item => item.key === requested)) view.value = requested as View
    await changePage(view.value, true)
    beginPolling()
  } catch (e) { error.value = e instanceof Error ? e.message : '加载失败，请稍后刷新' }
  finally { booting.value = false }
}
async function submitAuth() {
  busy.value = true; error.value = ''
  try {
    const endpoint = mode.value === 'setup' ? '/api/v1/auth/setup' : mode.value === 'register' ? '/api/v1/auth/register' : '/api/v1/auth/login'
    const data = await api<{ user: User; pending?: boolean }>(endpoint, { method: 'POST', body: JSON.stringify(mode.value === 'register' ? { ...form.value, invite_code: inviteCode.value.trim() } : form.value) })
    form.value.password = ''
    if (data.pending || data.user.status === 'pending') { mode.value = 'login'; notice.value = '注册申请已提交，管理员审核通过后即可登录。'; return }
    user.value = data.user; notice.value = ''; await refresh(); beginPolling()
  } catch (e) { error.value = e instanceof Error ? e.message : '请求失败' }
  finally { busy.value = false }
}
async function runNow(ids?: string[], selection?: TaskRunSelection) {
  busy.value = true; error.value = ''; notice.value = ''
  try {
    await api('/api/v1/run', { method: 'POST', body: JSON.stringify(selection || (ids ? { account_ids: ids } : {})) })
    showTaskSelection.value = false
    notice.value = '任务已开始，进度会自动更新。'; await refresh(); beginPolling()
  } catch (e) { error.value = e instanceof Error ? e.message : '任务启动失败' }
  finally { busy.value = false }
}
async function stopRun(ids?: string[]) {
  busy.value = true; error.value = ''
  try {
    await api('/api/v1/run/cancel', { method: 'POST', body: JSON.stringify(ids ? { account_ids: ids } : {}) })
    notice.value = '已请求停止，完成的签到结果会保留。'; await refresh()
  } catch (e) { error.value = e instanceof Error ? e.message : '停止失败' }
  finally { busy.value = false }
}
function openBind(account?: Account) { binding.value = account || null; showLogin.value = true }
async function bound() { notice.value = '账号已绑定，可以开始今天的签到了。'; await safeRefresh() }
async function openGuide() { await navigate('dashboard'); if (view.value === 'dashboard') showGuide.value = true }
async function finishGuide(state: 'dismissed' | 'complete') {
  showGuide.value = false
  try { user.value = await api<User>('/api/v1/profile/onboarding', { method: 'POST', body: JSON.stringify({ status: state }) }); notice.value = state === 'complete' ? '引导已完成，之后可随时修改各项配置。' : '引导已关闭，可从「个人账号」重新打开。' }
  catch { error.value = '引导已关闭，但未能保存关闭状态；下次登录可能再次出现。' }
}
function resetPrivateState() {
  resetApiSession(); stopPolling(); user.value = null; config.value = null; status.value = { running: false, logs: [] }
  view.value = 'dashboard'; showLogin.value = false; showTaskSelection.value = false; binding.value = null; form.value.password = ''; notice.value = ''; error.value = ''
  moreNavigation.value = false; pendingView.value = null
  taskAccount.value = null; selectTaskAccount.value = false
  showGuide.value = false; guidedUserID = ''
  void nextTick(() => changePage('dashboard', true))
}
function unauthorized() { if (user.value) { resetPrivateState(); mode.value = 'login'; notice.value = '登录已过期，请重新登录。' } }
async function logout() {
  try { await api('/api/v1/auth/logout', { method: 'POST', body: '{}' }); resetPrivateState(); mode.value = 'login' }
  catch (e) { error.value = e instanceof Error ? e.message : '退出失败' }
}
function visible() { if (!document.hidden && user.value) { stopPolling(); void poll() } }
watch(view, () => { error.value = ''; notice.value = ''; window.scrollTo({ top: 0, behavior: 'instant' }) })
onMounted(() => { window.addEventListener('miyohub:unauthorized', unauthorized); document.addEventListener('visibilitychange', visible); void loadAuth() })
onUnmounted(() => { disposeHistory(); window.removeEventListener('miyohub:unauthorized', unauthorized); document.removeEventListener('visibilitychange', visible); stopPolling() })
</script>

<template>
  <div v-if="!user" class="auth-page">
    <div class="auth-glow"></div>
    <div class="auth-card">
      <div class="brand-mark"><BrandMark /><div><strong>MiyoHub</strong><small>签到与兑换管理</small></div></div>
      <template v-if="booting"><div class="empty rich-empty"><span class="loading-orbit"></span><p>正在加载…</p></div></template>
      <template v-else>
        <p class="eyebrow auth-eyebrow">{{ mode === 'setup' ? '首次设置' : '站点账号' }}</p>
        <h1>{{ mode === 'setup' ? '创建管理员账号' : mode === 'register' ? '注册站点账号' : '登录 MiyoHub' }}</h1>
        <p class="muted">{{ mode === 'setup' ? '首次使用，请创建站点管理员。' : mode === 'register' ? '用于登录本站，米游社账号登录后单独绑定。' : '登录后管理米游社账号、签到任务与商品兑换。' }}</p>
        <p v-if="notice" class="notice" role="status">{{ notice }}</p>
        <form class="auth-form" @submit.prevent="submitAuth">
          <label>用户名<input v-model="form.username" autocomplete="username" maxlength="64" placeholder="输入用户名" required /></label>
          <label>密码<input v-model="form.password" type="password" :autocomplete="mode === 'login' ? 'current-password' : 'new-password'" :minlength="mode === 'login' ? 1 : 8" maxlength="128" :placeholder="mode === 'login' ? '输入密码' : '至少 8 位'" required /></label>
          <label v-if="mode === 'register'">{{ auth?.registration_mode === 'closed' ? '邀请码' : '邀请码（可选）' }}<input v-model="inviteCode" autocomplete="off" placeholder="粘贴管理员提供的邀请码" :required="auth?.registration_mode === 'closed'" /></label>
          <p v-if="error" class="error-text" role="alert">{{ error }}</p>
          <button class="button primary wide" :disabled="busy">{{ busy ? '处理中…' : mode === 'setup' ? '创建并进入' : mode === 'register' ? '注册' : '进入控制台' }}<AppIcon name="arrow" /></button>
        </form>
        <button v-if="auth?.has_admin" class="link-button" @click="mode = mode === 'login' ? 'register' : 'login'; error = ''">{{ mode === 'login' ? '还没有账号？创建一个' : '已有账号，返回登录' }}</button>
      </template>
    </div>
  </div>

  <div v-else class="app-shell">
    <aside class="sidebar">
      <div class="brand-mark compact"><BrandMark :size="30" /><div><strong>MiyoHub</strong><small>签到与兑换管理</small></div></div>
      <nav aria-label="主导航"><template v-for="(item, index) in navigation" :key="item.key"><p v-if="index === 0 || navigation[index - 1]?.group !== item.group" class="eyebrow nav-group">{{ item.group }}</p><button :class="{ active: view === item.key }" :aria-current="view === item.key ? 'page' : undefined" @click="navigate(item.key)"><AppIcon :name="item.icon" /><span>{{ item.label }}</span><span v-if="item.key === 'admin'" class="nav-admin-dot"></span></button></template></nav>
      <div class="sidebar-foot"><span class="status-dot" :class="{ offline: !connected }"></span>{{ connected ? '数据自动刷新' : '刷新失败 · 自动重试' }}</div>
    </aside>
    <main class="main-content">
      <header class="topbar"><div><p class="eyebrow">{{ navigation.find(item => item.key === view)?.group }}</p><h1>{{ navigation.find(item => item.key === view)?.label }}</h1><p class="topbar-subtitle">{{ pageDescriptions[view] }}</p></div><div class="top-actions"><button class="icon-button refresh-button" title="刷新数据" aria-label="刷新数据" :aria-busy="refreshing" :disabled="refreshing" :class="{ 'is-refreshing': refreshing }" @click="safeRefresh"><AppIcon name="refresh" :size="19" /></button><button class="avatar" title="个人账号" aria-label="个人账号" @click="navigate('profile')">{{ user.username.slice(0, 1).toUpperCase() }}</button><button class="icon-button logout-button" title="退出登录" aria-label="退出登录" @click="logout"><AppIcon name="logout" :size="16" /></button></div></header>
      <div v-if="notice" class="notice" role="status">{{ notice }}</div><div v-if="error" class="error-banner" role="alert">{{ error }}</div>
      <p v-if="!connected" class="error-banner" role="status">暂时无法更新数据，正在自动重试。<button class="text-button" @click="safeRefresh">立即刷新</button></p>

      <template v-if="view === 'dashboard' && config">
        <section class="metrics" aria-label="任务概况"><article v-for="metric in metrics" :key="metric.label" class="metric-card" :data-tone="metric.tone"><span>{{ metric.label }}</span><strong>{{ metric.value }}</strong></article></section>
        <section class="hero-card">
          <div class="hero-copy"><h2>执行签到任务</h2><p class="muted">按账号配置执行游戏签到、云游戏签到和米游币任务。</p></div>
          <div class="hero-action"><button v-if="status.running" class="button dark" :disabled="busy" @click="stopRun()"><AppIcon name="stop" />停止本轮任务</button><button v-else class="button dark" :disabled="busy || !config.enabled || !config.accounts.some(a => !a.disabled && hasTasks(a))" @click="runNow()">{{ busy ? '正在启动…' : '开始今日签到' }}<AppIcon name="arrow" /></button><small>{{ !config.enabled ? '管理员已暂停站点签到任务' : status.running ? '正在逐个处理账号，请稍候' : '每个账号按自己的签到设置执行' }}</small><button class="text-button hero-select" :disabled="busy || !config.enabled || !config.accounts.some(a => !a.disabled && hasTasks(a))" @click="error = ''; showTaskSelection = true"><AppIcon name="filter" :size="14" />自选账号与任务</button></div>
        </section>
        <section class="content-grid">
          <AccountsPanel :accounts="config.accounts" :tasks="status.tasks || []" :user-id="user.id" :enabled="config.enabled" :timezone="timezone" :action-busy="busy" @changed="safeRefresh" @bind="openBind" @configure="account => taskAccount = account" @run="id => runNow([id])" @stop="id => stopRun([id])" />
          <article class="panel schedule-panel"><div class="panel-title"><div><p class="eyebrow">自动执行</p><h2>我的自动签到</h2></div><span class="pill" :class="{ soft: config.enabled && scheduledAccounts.length }">{{ !config.enabled ? '站点已暂停' : !scheduledAccounts.length ? '尚未安排' : scheduledAccounts.length + ' 个账号参与' }}</span></div><div class="schedule-clock"><AppIcon name="clock" :size="24" /><div class="schedule-time">{{ nextCheckinTime }}</div></div><p class="muted">下次执行时间 · {{ timezone }}</p><div class="schedule-divider"></div><p class="schedule-caption">下一次执行</p><p class="schedule-next">{{ !config.enabled ? '站点签到已暂停' : !scheduledAccounts.length ? '请在账号设置中开启自动签到并选择时间' : formatDate(status.scheduler?.next_run || '', timezone) }}</p><p v-if="status.scheduler?.last_error" class="error-text">{{ status.scheduler.last_error }}</p><p class="muted">按各账号的个人时间安排；未设置个人时间时使用站点默认时间。这里展示你的账号中最近的一次调度。</p><button class="text-button schedule-link" :disabled="!config.accounts.length" @click="selectTaskAccount = true">调整账号签到设置<AppIcon name="arrow" :size="15" /></button><button v-if="user.role === 'admin'" class="text-button schedule-link" @click="navigate('settings')">管理站点默认时间<AppIcon name="arrow" :size="15" /></button></article>
        </section>
        <ActivityPanel :logs="status.logs" :running="status.running" :timezone="timezone" compact @all="navigate('logs')" />
      </template>

      <ShopPanel v-else-if="view === 'shop' && config" :config="config" :user="user" @changed="safeRefresh" />
      <PushPanel v-else-if="view === 'notifications'" ref="pushPanel" :timezone="timezone" />
      <CaptchaPanel v-else-if="view === 'captcha'" ref="captchaPanel" :user="user" />
      <ActivityPanel v-else-if="view === 'logs'" :logs="status.logs" :running="status.running" :timezone="timezone" />
      <SettingsPanel v-else-if="view === 'settings' && config && user.role === 'admin'" ref="settingsPanel" :config="config" :admin="true" @saved="safeRefresh" @navigate="navigate" />
      <ProfilePanel v-else-if="view === 'profile'" :user="user" @guide="openGuide" />
      <AdminPanel v-else-if="view === 'admin' && user.role === 'admin'" :user="user" :timezone="timezone" />
      <ChangelogPanel v-else-if="view === 'changelog'" :server-version="serverVersion" />
      <div v-else class="panel empty"><p>正在加载控制台…</p><button class="small-button" @click="safeRefresh">重新加载</button></div>
      <footer class="app-footer"><span><BrandMark :size="15" /> MiyoHub <button class="text-button" @click="navigate('changelog')">v{{ appVersion }} · 更新日志</button></span><span>站点时区：{{ timezone }}</span></footer>
    </main>
    <OnboardingGuide v-if="showGuide && config" :user="user" :config="config" @changed="safeRefresh" @done="finishGuide" />
    <BindAccountModal v-if="showLogin" :account="binding" @close="showLogin = false" @changed="bound" />
    <AccountTasksModal v-if="taskAccount && config" :account="taskAccount" :schedule="config.schedule" @close="taskAccount = null" @saved="notice = '账号签到设置已保存，只对这个账号生效。'; safeRefresh()" />
    <ModalShell v-if="selectTaskAccount && config" title="选择要配置的账号" @close="selectTaskAccount = false"><div class="account-picker"><button v-for="account in config.accounts" :key="account.id" @click="selectTaskAccount = false; taskAccount = account"><span class="account-avatar">{{ account.name.slice(0, 1) }}</span><span><strong>{{ account.name }}</strong><small>{{ accountTasks(account).automatic ? '参加每日自动签到' : '仅手动签到' }}</small></span><AppIcon name="arrow" :size="16" /></button></div></ModalShell>
    <RunTasksModal v-if="showTaskSelection && config" :config="config" :tasks="status.tasks || []" :busy="busy" :error="error" @close="showTaskSelection = false; error = ''" @run="selection => runNow(undefined, selection)" />
    <nav class="mobile-nav" aria-label="移动导航"><button v-for="item in mobileNavigation" :key="item.key" :class="{ active: view === item.key }" :aria-current="view === item.key ? 'page' : undefined" @click="navigate(item.key)"><AppIcon :name="item.icon" /><small>{{ item.label }}</small></button><button :class="{ active: !['dashboard', 'shop', 'notifications'].includes(view) }" :aria-expanded="moreNavigation" @click="moreNavigation = true"><AppIcon name="grid" /><small>更多</small></button></nav>
    <ModalShell v-if="moreNavigation" title="更多功能" @close="moreNavigation = false"><nav class="workspace-menu" aria-label="更多导航"><button v-for="item in moreItems" :key="item.key" :class="{ active: view === item.key }" @click="navigate(item.key)"><AppIcon :name="item.icon" :size="21" /><span><strong>{{ item.label }}</strong></span><AppIcon name="chevron" :size="14" /></button></nav></ModalShell>
    <ModalShell v-if="pendingView" :title="view === 'notifications' ? '推送配置尚未保存' : '配置尚未保存'" @close="pendingView = null"><p class="muted">返回编辑可继续保存；直接离开将放弃未保存的修改。</p><div class="modal-actions"><button class="small-button" @click="pendingView = null">返回编辑</button><button class="button primary" @click="leavePush">放弃修改并离开</button></div></ModalShell>
  </div>
</template>

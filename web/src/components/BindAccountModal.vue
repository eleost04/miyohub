<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { api, ApiError } from '../api'
import type { Account, LoginState, SMSCaptchaSolution, SMSState } from '../types'
import AppIcon from './AppIcon.vue'
import ModalShell from './ModalShell.vue'
import ChoiceSelect from './ChoiceSelect.vue'
const HumanCaptcha = defineAsyncComponent(() => import('./HumanCaptcha.vue'))

const props = withDefaults(defineProps<{ account: Account | null; initialTab?: 'qr' | 'sms' }>(), { initialTab: 'qr' })
const emit = defineEmits<{ close: []; changed: [] }>()
const tab = ref<'qr' | 'sms'>(props.initialTab), name = ref(props.account?.name || '')
const phone = ref(''), code = ref(''), busy = ref(false), error = ref(''), notice = ref('')
const state = ref<LoginState>({ running: false, status: '', qr_url: '', error: '', account: '' })
const sms = ref<SMSState | null>(null), canvas = ref<HTMLCanvasElement | null>(null), now = ref(Date.now())
const captchaMode = ref('manual'), human = ref(false)
const smsSent = computed(() => sms.value?.status === 'sent' || sms.value?.challenge?.operation === 'verify')
const smsLocked = computed(() => smsSent.value || sms.value?.status === 'captcha_required')
const captchaOptions = [{ value: 'manual', label: '手动完成验证', description: '在当前页面点选或滑动，不调用打码服务' }, { value: 'auto', label: '使用我的打码服务', description: '按个人配置调用；未成功时可改为手动验证' }]
const life = new AbortController()
let timer: number | undefined, countdownTimer: number | undefined, completed = false, qrRequested = false, smsRequested = false
const countdown = computed(() => sms.value ? Math.max(0, Math.ceil((new Date(sms.value.retry_at).getTime() - now.value) / 1000)) : 0)
const qrLabels: Record<string, string> = { starting: '正在生成二维码', waiting: '请使用米游社 APP 扫码', Init: '请使用米游社 APP 扫码', Scanned: '已扫码，请在手机上确认', Confirmed: '正在完成绑定', success: '账号绑定成功', timeout: '二维码已过期，请刷新', cancelled: '扫码已取消', error: '暂时无法完成绑定' }
const label = computed(() => qrLabels[state.value.status] || '用米游社 APP，轻松绑定账号')
const body = () => ({ account_name: name.value.trim(), account_id: props.account?.id || '' })
const request = <T,>(path: string, init: RequestInit = {}) => api<T>(path, { ...init, signal: life.signal })
function fail(e: unknown) {
  if (life.signal.aborted) return
  notice.value = ''
  if (e instanceof ApiError && e.data && typeof e.data === 'object' && 'status' in e.data) sms.value = e.data as SMSState
  error.value = e instanceof Error ? e.message : '登录失败，请稍后重试'
}
function acceptSMS(next: SMSState) {
  if (next.status === 'verified') { done(); return }
  sms.value = next; notice.value = next.message || ''
  human.value = !!next.challenge
}
function done() { completed = true; code.value = ''; phone.value = ''; emit('changed'); emit('close') }
async function displayQR() {
  if (!state.value.qr_url || life.signal.aborted) return
  const source = state.value.qr_url
  const { default: QRCode } = await import('qrcode')
  await nextTick()
  if (!life.signal.aborted && canvas.value && source === state.value.qr_url) await QRCode.toCanvas(canvas.value, source, { width: 224, margin: 2, color: { dark: '#303b30', light: '#ffffff' } })
}
async function poll() {
  try {
    state.value = await request<LoginState>('/api/v1/login/qr')
    if (state.value.status === 'success') { done(); return }
    if (state.value.error) error.value = state.value.error
    await displayQR()
  } catch (e) { fail(e) }
  finally { if (!life.signal.aborted && state.value.running) timer = window.setTimeout(poll, 1500) }
}
async function startQR(refresh = false) {
  if (!name.value.trim()) { error.value = '先给这个账号取个名字吧'; return }
  busy.value = true; error.value = ''; window.clearTimeout(timer)
  qrRequested = true
  try {
    state.value = await request<LoginState>('/api/v1/login/qr/' + (refresh ? 'refresh' : 'start'), { method: 'POST', body: JSON.stringify(body()) })
    await displayQR(); timer = window.setTimeout(poll, 1500)
  } catch (e) { fail(e) } finally { busy.value = false }
}
async function switchTab(next: 'qr' | 'sms') {
  if (next === tab.value || busy.value) return
  window.clearTimeout(timer); error.value = ''; notice.value = ''
  try { if (state.value.running) await request('/api/v1/login/qr/cancel', { method: 'POST', body: '{}' }) } catch (e) { fail(e); return }
  state.value = { running: false, status: '', qr_url: '', error: '', account: '' }; tab.value = next
}
async function sendSMS() {
  if (!name.value.trim()) { error.value = '请填写账号名称'; return }
  if (!/^1[3-9]\d{9}$/.test(phone.value.trim())) { error.value = '请输入有效的中国大陆手机号'; return }
  busy.value = true; error.value = ''; notice.value = ''; code.value = ''
  smsRequested = true
  try {
    acceptSMS(await request<SMSState>('/api/v1/login/sms/send', { method: 'POST', body: JSON.stringify({ ...body(), phone: phone.value.trim(), captcha_mode: captchaMode.value }) }))
  } catch (e) { fail(e) } finally { busy.value = false }
}
async function verifySMS() {
  busy.value = true; error.value = ''; notice.value = ''
  try { acceptSMS(await request<SMSState>('/api/v1/login/sms/verify', { method: 'POST', body: JSON.stringify({ captcha: code.value.trim() }) })) }
  catch (e) { fail(e) } finally { busy.value = false }
}
async function completeCaptcha(solution: SMSCaptchaSolution) {
  if (busy.value) return
  busy.value = true; error.value = ''; notice.value = ''
  try { acceptSMS(await request<SMSState>('/api/v1/login/sms/captcha', { method: 'POST', body: JSON.stringify(solution) })) }
  catch (e) { human.value = false; fail(e) }
  finally { busy.value = false }
}
onMounted(() => { countdownTimer = window.setInterval(() => { now.value = Date.now() }, 1000) })
onUnmounted(() => {
  window.clearTimeout(timer); window.clearInterval(countdownTimer); life.abort()
  if (qrRequested && (!completed || tab.value !== 'qr')) void api('/api/v1/login/qr/cancel', { method: 'POST', body: '{}' }).catch(() => {})
  if (smsRequested && (!completed || tab.value !== 'sms')) void api('/api/v1/login/sms/cancel', { method: 'POST', body: '{}' }).catch(() => {})
})
</script>
<template>
  <ModalShell :title="account ? '重新绑定账号' : '绑定米游社账号'" @close="emit('close')">
    <div class="segmented" aria-label="登录方式"><button :class="{ active: tab === 'qr' }" :disabled="busy" @click="switchTab('qr')"><AppIcon name="scan" />扫码登录</button><button :class="{ active: tab === 'sms' }" :disabled="busy" @click="switchTab('sms')"><AppIcon name="phone" />短信登录</button></div>
    <label class="field-label">账号名称<input v-model="name" :disabled="!!account || busy || !!state.status || !!sms" maxlength="64" placeholder="例如：我的主账号" autocomplete="off" /></label>
    <p v-if="account" class="muted">{{ account.stuid ? '请登录米游社 UID ' + account.stuid + ' 对应的账号。' : '请登录原来绑定的米游社账号。' }}已有兑换计划会保留。</p>
    <template v-if="tab === 'qr'">
      <div class="qr-box"><canvas v-show="state.qr_url" ref="canvas" aria-label="米游社登录二维码"></canvas><div v-if="!state.qr_url" class="qr-placeholder"><AppIcon name="scan" :size="72" /><span>{{ busy ? '二维码生成中…' : '点击下方按钮生成登录二维码' }}</span></div><p :class="{ 'live-label': state.running }">{{ label }}</p></div>
      <div class="modal-actions"><button class="button primary" :disabled="busy" @click="startQR(!!state.status)"><AppIcon :name="state.status ? 'refresh' : 'scan'" />{{ busy ? '正在生成…' : state.status ? '刷新二维码' : '生成二维码' }}</button></div>
      <p class="muted center-text">二维码有效期 2 分钟，扫码后在手机上确认登录。</p>
    </template>
    <form v-else class="sms-form" @submit.prevent="verifySMS">
      <label class="field-label">手机号<div class="phone-field"><span>+86</span><input v-model="phone" type="tel" inputmode="tel" autocomplete="tel-national" maxlength="11" placeholder="输入绑定米游社的手机号" :disabled="busy || smsLocked" required /></div></label>
      <label class="field-label">人机验证方式<ChoiceSelect v-model="captchaMode" label="人机验证方式" :options="captchaOptions" :disabled="busy || smsLocked" /></label>
      <label class="field-label">短信验证码<div class="code-field"><input v-model="code" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]{4,8}" maxlength="8" placeholder="输入验证码" :disabled="busy || !smsSent" required /><button type="button" class="small-button" :disabled="busy || countdown > 0 || sms?.status === 'captcha_required'" @click="sendSMS">{{ countdown ? countdown + ' 秒后可重发' : busy ? '处理中…' : sms?.status === 'captcha_required' ? '等待安全验证' : smsSent ? '重新发送' : '获取验证码' }}</button></div></label>
      <p v-if="notice" class="notice" role="status">{{ notice }}</p>
      <button v-if="sms?.challenge" type="button" class="button primary wide" :disabled="busy" @click="human = true"><AppIcon name="shield" />继续人机验证</button>
      <button v-else class="button primary wide" :disabled="busy || !smsSent || !code">{{ busy ? '处理中…' : '验证并绑定账号' }}<AppIcon name="arrow" /></button>
      <p class="muted">用于绑定米游社账号，不是站点登录。短信发送成功后才能输入验证码；遇到人机验证可在页面手动完成，无需配置打码服务。短信会话最多保留 10 分钟，人机验证 2 分钟内有效。</p>
    </form>
    <p v-if="error" class="error-banner" role="alert">{{ error }}</p>
  </ModalShell>
  <HumanCaptcha v-if="human && sms?.challenge" :key="sms.challenge.id" :challenge="sms.challenge" :busy="busy" @close="human = false" @solved="completeCaptcha" />
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api } from '../api'
import type { PushBindingState } from '../types'
import AppIcon from './AppIcon.vue'
import ModalShell from './ModalShell.vue'

const props = defineProps<{ provider: 'qqbot' | 'wechat_claw'; channelId: string; revision: number }>()
const emit = defineEmits<{ close: []; bound: [message: string] }>()
const state = ref<PushBindingState | null>(null), error = ref(''), code = ref(''), busy = ref(false), now = ref(Date.now())
const isQQ = computed(() => props.provider === 'qqbot')
const remaining = computed(() => Math.max(0, Math.ceil((Date.parse(state.value?.expires_at || '') - now.value) / 1000)) || 0)
let timer: number | undefined, clock: number | undefined, active = true, reported = false
const controller = new AbortController()
function schedule() { window.clearTimeout(timer); if (active && state.value?.running) timer = window.setTimeout(poll, 1500) }
async function start() {
  busy.value = true; error.value = ''; code.value = ''; window.clearTimeout(timer)
  try {
    if (state.value?.running) await cancel()
    const data = await api<PushBindingState>('/api/v1/push/qr/start', { method: 'POST', body: JSON.stringify({ provider: props.provider, channel_id: props.channelId, revision: props.revision }), signal: controller.signal })
    if (active) { state.value = data; reported = false; schedule() }
  } catch (e) { if (active) error.value = e instanceof Error ? e.message : '二维码获取失败' }
  finally { if (active) busy.value = false }
}
async function poll() {
  try {
    const data = await api<PushBindingState>('/api/v1/push/qr', { signal: controller.signal })
    if (!active) return
    if (data.session_id !== state.value?.session_id) { error.value = '其他窗口已开始新的扫码，请关闭此窗口后重试。'; return }
    state.value = data; error.value = ''
    if (data.status === 'confirmed' && !reported) { reported = true; emit('bound', data.message) }
    schedule()
  } catch (e) { if (active) { error.value = e instanceof Error ? e.message : '暂时无法读取扫码状态'; schedule() } }
}
async function verify() {
  if (!state.value || busy.value) return
  busy.value = true; error.value = ''
  try {
    await api('/api/v1/push/qr/verify', { method: 'POST', body: JSON.stringify({ session_id: state.value.session_id, code: code.value.trim() }), signal: controller.signal })
    code.value = ''; await poll()
  } catch (e) { if (active) error.value = e instanceof Error ? e.message : '配对码提交失败' }
  finally { if (active) busy.value = false }
}
async function cancel() {
  if (state.value?.running) { const id = state.value.session_id; await api('/api/v1/push/qr/cancel', { method: 'POST', body: JSON.stringify({ session_id: id }), keepalive: true }); if (state.value?.session_id === id) state.value.running = false }
}
async function close() {
  try { await cancel(); emit('close') }
  catch (e) { error.value = e instanceof Error ? e.message : '取消失败，请重试' }
}
onMounted(() => { void start(); clock = window.setInterval(() => { now.value = Date.now() }, 1000) })
onUnmounted(() => { active = false; window.clearTimeout(timer); window.clearInterval(clock); controller.abort(); void cancel().catch(() => {}) })
</script>

<template>
  <ModalShell :title="isQQ ? '扫码连接 QQ 机器人' : '扫码连接微信'" @close="close">
    <p class="muted">{{ isQQ ? '用手机 QQ 扫码，在官方页面创建或选择机器人。确认后保存机器人凭据；若官方未返回接收者 OpenID，需在渠道配置中补充。' : '用手机微信扫码并确认连接。如果手机显示数字配对码，请在下方输入。' }}</p>
    <div class="push-qr-display" :class="{ 'is-confirmed': state?.status === 'confirmed' }" aria-live="polite">
      <template v-if="state?.status === 'confirmed'"><span class="binding-success-icon"><AppIcon name="check" :size="34" /></span><h3>绑定成功</h3><p>{{ state.message }}</p></template>
      <template v-else><img v-if="state?.qr_image" :src="state.qr_image" :alt="isQQ ? 'QQ 官方机器人绑定二维码' : '微信绑定二维码'" width="240" height="240" /><span v-else-if="busy || state?.status === 'starting'" class="loading-orbit"></span><AppIcon v-else name="scan" :size="44" /><p>{{ state?.message || '正在获取绑定信息…' }}</p><small v-if="state?.running && remaining">二维码剩余 {{ remaining }} 秒</small></template>
    </div>
    <form v-if="state?.status === 'need_verifycode'" class="push-verify-form" @submit.prevent="verify"><label>微信数字配对码<input v-model="code" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]{1,12}" maxlength="12" required placeholder="输入手机微信显示的数字" autofocus /></label><button class="button primary" :disabled="busy">{{ busy ? '确认中…' : '确认配对码' }}</button></form>
    <p v-if="error || state?.status === 'error'" class="error-banner" role="alert">{{ error || state?.message }}</p>
    <p v-if="!isQQ" class="push-privacy"><AppIcon name="info" :size="15" /><span>绑定后，先在微信给机器人发一条消息以建立通知会话。会话失效时按提示重新发送消息或扫码；不保存聊天正文。</span></p>
    <div class="modal-actions"><button v-if="state?.status !== 'confirmed'" class="small-button" :disabled="busy" @click="start"><AppIcon name="refresh" :size="15" />刷新二维码</button><button class="button primary" :disabled="busy" @click="close">{{ state?.status === 'confirmed' ? '完成' : '取消绑定' }}</button></div>
    <a v-if="state?.qr_url" :href="state.qr_url" target="_blank" rel="noopener noreferrer" class="text-button push-official-link">在官方页面打开连接<AppIcon name="arrow" :size="13" /></a>
  </ModalShell>
</template>

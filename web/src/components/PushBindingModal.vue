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
const remaining = computed(() => Math.min(300, Math.max(0, Math.ceil((Date.parse(state.value?.expires_at || '') - now.value) / 1000))) || 0)
let timer: number | undefined, clock: number | undefined, active = true, reported = false, generation = 0, polling = false
const controller = new AbortController()
function schedule() { window.clearTimeout(timer); if (active && !document.hidden && state.value?.running) timer = window.setTimeout(poll, 1500) }
async function start(refresh = false) {
  if (busy.value) return
  const current = ++generation
  busy.value = true; error.value = ''; code.value = ''; window.clearTimeout(timer)
  try {
    if (refresh) await cancel()
    else {
      const existing = await api<PushBindingState>('/api/v1/push/qr', { signal: controller.signal })
      if (!active || current !== generation) return
      if (existing.running && existing.provider === props.provider && existing.channel_id === props.channelId && existing.revision === props.revision && Date.parse(existing.expires_at) > Date.now()) {
        state.value = existing; reported = false; schedule(); return
      }
    }
    if (!active || current !== generation) return
    const data = await api<PushBindingState>('/api/v1/push/qr/start', { method: 'POST', body: JSON.stringify({ provider: props.provider, channel_id: props.channelId, revision: props.revision }), signal: controller.signal })
    if (active && current === generation) { state.value = data; reported = false; schedule() }
  } catch (e) { if (active && current === generation) error.value = e instanceof Error ? e.message : '二维码获取失败' }
  finally { if (active && current === generation) busy.value = false }
}
async function poll() {
  if (!active || polling) { schedule(); return }
  const current = generation
  polling = true
  try {
    const data = await api<PushBindingState>('/api/v1/push/qr', { signal: controller.signal })
    if (!active || current !== generation) return
    if (data.session_id !== state.value?.session_id) { error.value = '其他窗口已开始新的扫码，请关闭此窗口后重试。'; return }
    state.value = data; error.value = ''
    if (data.status === 'confirmed' && !reported) { reported = true; emit('bound', data.message) }
    schedule()
  } catch (e) { if (active && current === generation) { error.value = e instanceof Error ? e.message : '暂时无法读取扫码状态'; schedule() } }
  finally { polling = false }
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
  if (state.value?.running) {
    const id = state.value.session_id
    await api('/api/v1/push/qr/cancel', { method: 'POST', body: JSON.stringify({ session_id: id }), signal: controller.signal })
    if (active && state.value?.session_id === id) state.value = { ...state.value, running: false, status: 'cancelled', qr_image: '', qr_url: '', message: '扫码已取消' }
  }
}
async function cancelAndClose() {
  if (busy.value) return
  busy.value = true; ++generation; window.clearTimeout(timer)
  try { await cancel(); emit('close') }
  catch (e) { if (active) { error.value = e instanceof Error ? e.message : '取消失败，请重试'; schedule() } }
  finally { if (active) busy.value = false }
}
function visibilityChanged() { if (!document.hidden && state.value?.running && !busy.value) { now.value = Date.now(); window.clearTimeout(timer); void poll() } }
onMounted(() => { void start(); clock = window.setInterval(() => { now.value = Date.now() }, 1000); document.addEventListener('visibilitychange', visibilityChanged) })
onUnmounted(() => { active = false; ++generation; window.clearTimeout(timer); window.clearInterval(clock); controller.abort(); document.removeEventListener('visibilitychange', visibilityChanged) })
</script>

<template>
  <ModalShell :title="isQQ ? '扫码连接 QQ 机器人' : '扫码连接微信'" @close="emit('close')">
    <p class="muted">{{ isQQ ? '用手机 QQ 扫码，在官方页面创建或选择机器人。确认后保存机器人凭据；若官方未返回接收者 OpenID，需在渠道配置中补充。' : '用手机微信扫码并确认连接。如果手机显示数字配对码，请在下方输入。' }}</p>
    <div class="push-qr-display" :class="{ 'is-confirmed': state?.status === 'confirmed' }" aria-live="polite">
      <template v-if="state?.status === 'confirmed'"><span class="binding-success-icon"><AppIcon name="check" :size="34" /></span><h3>{{ isQQ ? '机器人凭据已保存' : '绑定成功' }}</h3><p>{{ state.message }}</p></template>
      <template v-else><img v-if="state?.qr_image" :src="state.qr_image" :alt="isQQ ? 'QQ 官方机器人绑定二维码' : '微信绑定二维码'" width="240" height="240" /><span v-else-if="busy || state?.status === 'starting'" class="loading-orbit"></span><AppIcon v-else name="scan" :size="44" /><p>{{ state?.message || '正在获取绑定信息…' }}</p><small v-if="state?.running && remaining">二维码剩余 {{ remaining }} 秒</small></template>
    </div>
    <form v-if="state?.status === 'need_verifycode'" class="push-verify-form" @submit.prevent="verify"><label>微信数字配对码<input v-model="code" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]{1,12}" maxlength="12" required placeholder="输入手机微信显示的数字" autofocus /></label><button class="button primary" :disabled="busy">{{ busy ? '确认中…' : '确认配对码' }}</button></form>
    <p v-if="error || state?.status === 'error'" class="error-banner" role="alert">{{ error || state?.message }}</p>
    <p v-if="state?.running" class="push-privacy"><AppIcon name="info" :size="15" /><span>关闭弹窗或切换应用不会取消绑定，可在本次 5 分钟有效期内重新打开继续。点击“取消绑定”才会作废，刷新二维码会替换当前会话。</span></p>
    <p v-if="!isQQ" class="push-privacy"><AppIcon name="info" :size="15" /><span>绑定后，先在微信给机器人发一条消息以建立通知会话。会话失效时按提示重新发送消息或扫码；不保存聊天正文。</span></p>
    <div class="modal-actions"><button v-if="state?.status !== 'confirmed'" class="small-button" :disabled="busy" @click="start(true)"><AppIcon name="refresh" :size="15" />刷新二维码</button><button v-if="state?.status === 'confirmed'" class="button primary" @click="emit('close')">完成</button><button v-else class="button" :disabled="busy" @click="cancelAndClose">取消绑定</button></div>
    <a v-if="state?.qr_url" :href="state.qr_url" target="_blank" rel="noopener noreferrer" class="text-button push-official-link">在官方页面打开连接<AppIcon name="arrow" :size="13" /></a>
  </ModalShell>
</template>

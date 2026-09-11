<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import type { SMSCaptchaSolution, SMSChallenge } from '../types'
import ModalShell from './ModalShell.vue'
import AppIcon from './AppIcon.vue'
const props = defineProps<{ challenge: SMSChallenge; busy: boolean }>()
const emit = defineEmits<{ close: []; solved: [solution: SMSCaptchaSolution] }>()
const frame = ref<HTMLIFrameElement | null>(null), loading = ref(true), failed = ref(false), generation = ref(0), height = ref(460), now = ref(Date.now())
const remaining = computed(() => Math.max(0, Math.ceil((Date.parse(props.challenge.expires_at) - now.value) / 1000)))
let timer: number | undefined, clock: number | undefined, submitted = false
function init() { frame.value?.contentWindow?.postMessage({ type: 'miyohub:captcha:init', ...props.challenge }, '*') }
function receive(event: MessageEvent) {
  if (event.source !== frame.value?.contentWindow || event.origin !== 'null' || !event.data || typeof event.data.type !== 'string') return
  const data = event.data
  if (data.type === 'miyohub:captcha:ready') { init(); return }
  if (data.id !== props.challenge.id) return
  if (data.type === 'miyohub:captcha:loaded') { loading.value = false; failed.value = false; window.clearTimeout(timer) }
  if (data.type === 'miyohub:captcha:error') { loading.value = false; failed.value = true; window.clearTimeout(timer) }
  if (data.type === 'miyohub:captcha:height' && Number.isFinite(data.height)) height.value = Math.min(620, Math.max(140, data.height))
  if (data.type === 'miyohub:captcha:solution' && !props.busy && !submitted && remaining.value) {
    if (props.challenge.version === 4) {
      if (typeof data.captcha_id !== 'string' || data.captcha_id !== props.challenge.gt || typeof data.lot_number !== 'string' || typeof data.captcha_output !== 'string' || typeof data.pass_token !== 'string' || typeof data.gen_time !== 'string') return
      submitted = true
      emit('solved', { id: props.challenge.id, captcha_id: data.captcha_id, lot_number: data.lot_number, captcha_output: data.captcha_output, pass_token: data.pass_token, gen_time: data.gen_time })
    } else if (typeof data.challenge === 'string' && typeof data.validate === 'string') {
      submitted = true
      emit('solved', { id: props.challenge.id, challenge: data.challenge, validate: data.validate })
    }
  }
}
function reload() { submitted = false; loading.value = true; failed.value = false; generation.value++; window.clearTimeout(timer); timer = window.setTimeout(() => { failed.value = true; loading.value = false }, 22000) }
onMounted(() => { window.addEventListener('message', receive); clock = window.setInterval(() => { now.value = Date.now() }, 1000); reload() })
onUnmounted(() => { window.removeEventListener('message', receive); window.clearTimeout(timer); window.clearInterval(clock) })
</script>
<template>
  <ModalShell title="米游社安全验证" :busy="busy" @close="emit('close')">
    <p class="muted">{{ challenge.operation === 'send' ? '短信尚未发送。完成下方验证后，将继续发送验证码。' : '完成下方验证后，将继续绑定米游社账号。' }}</p>
    <p v-if="!remaining" class="error-banner" role="alert">验证已过期，请返回后重新获取验证码。</p>
    <template v-else><p v-if="busy" class="notice" role="status">正在提交验证结果…</p><p v-else-if="loading" class="muted" role="status">正在加载验证组件…</p><p v-if="failed" class="error-banner" role="alert">验证组件加载失败，请检查网络后重试，或返回使用扫码登录。</p><iframe :key="generation" ref="frame" class="human-captcha-frame" src="/captcha-frame.html" title="米游社人机验证" sandbox="allow-scripts" referrerpolicy="no-referrer" :style="{ height: height + 'px' }" @load="init" /></template>
    <div class="modal-actions"><button v-if="failed && remaining" type="button" class="small-button" :disabled="busy" @click="reload"><AppIcon name="refresh" :size="15" />重新加载</button><button type="button" class="small-button" :disabled="busy" @click="emit('close')">返回登录</button><small class="muted">{{ remaining ? remaining + ' 秒内有效' : '已失效' }}</small></div>
  </ModalShell>
</template>
<style scoped>
.human-captcha-frame{display:block;width:100%;border:0;max-width:100%;margin:16px 0;background:#fdfbf7;border-radius:12px}
</style>

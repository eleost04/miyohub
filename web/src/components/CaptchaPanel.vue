<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api } from '../api'
import type { CaptchaAttempt, CaptchaProbe, CaptchaSettings, Config, User } from '../types'
import { formatDate } from '../time'
import CaptchaEditor from './CaptchaEditor.vue'
import ModalShell from './ModalShell.vue'
import AppIcon from './AppIcon.vue'
import FloatingSave from './FloatingSave.vue'
import AutoSaveStatus from './AutoSaveStatus.vue'
import { reconcileCaptcha, useAutoSave } from '../autosave'

const props = defineProps<{ user: User }>()
const draft = ref<CaptchaSettings | null>(null), baseline = ref(''), busy = ref(false), error = ref(''), notice = ref(''), discard = ref(false)
const controller = new AbortController()
const activity = ref<CaptchaAttempt[]>([]), activityError = ref('')
const confirmTest = ref(false), submittingTest = ref(false), testError = ref(''), probe = ref<CaptchaProbe | null>(null), now = ref(Date.now())
const testing = computed(() => submittingTest.value || probe.value?.status === 'running')
const testCooldown = computed(() => probe.value ? Math.max(0, Math.ceil((Date.parse(probe.value.retry_at) - now.value) / 1000)) : 0)
let probeTimer: number | undefined, clockTimer: number | undefined
const dirty = computed(() => !!draft.value && JSON.stringify(draft.value) !== baseline.value)
const siteAllowed = computed(() => props.user.role === 'admin' || !!props.user.permissions?.site_captcha)
const form = ref<HTMLFormElement | null>(null)
const autosave = useAutoSave(() => JSON.stringify(draft.value), dirty, busy, () => !!form.value?.checkValidity() && (draft.value?.source !== 'site' || siteAllowed.value), save)
const channels = computed<Config['captcha']>({ get: () => ({ max_retries: draft.value?.max_retries || 0, channels: draft.value?.channels || [] }), set: value => { if (draft.value) { draft.value.max_retries = value.max_retries; draft.value.channels = value.channels } } })
function apply(data: CaptchaSettings) { const { activity: records, ...config } = data; activity.value = records || []; draft.value = config; baseline.value = JSON.stringify(config) }
async function refreshActivity() {
  try { const data = await api<CaptchaSettings>('/api/v1/captcha/config', { signal: controller.signal }); activity.value = data.activity || []; activityError.value = '' }
  catch { if (!controller.signal.aborted) activityError.value = '调用记录刷新失败，请稍后重试。' }
}
async function testService() {
  if (testing.value || dirty.value || testCooldown.value) return
  submittingTest.value = true; testError.value = ''
  try {
    const result = await api<{ probe: CaptchaProbe }>('/api/v1/captcha/test', { method: 'POST', body: '{}', signal: controller.signal })
    probe.value = result.probe; confirmTest.value = false
  } catch (e) { if (!controller.signal.aborted) testError.value = e instanceof Error ? e.message : '提交结果未确认，请刷新测试状态；不要连续重试。' }
  finally { submittingTest.value = false; if (!controller.signal.aborted) await refreshTest() }
}
async function refreshTest() {
  window.clearTimeout(probeTimer)
  try {
    const result = await api<{ probe: CaptchaProbe | null }>('/api/v1/captcha/test', { signal: controller.signal })
    probe.value = result.probe
    if (probe.value?.status === 'running') { testError.value = ''; confirmTest.value = false }
    else await refreshActivity()
  } catch { if (!controller.signal.aborted) testError.value = '测试状态暂时无法读取。后台任务不受页面断连影响，可稍后刷新查看。' }
  finally { if (!controller.signal.aborted && probe.value?.status === 'running') probeTimer = window.setTimeout(refreshTest, document.hidden ? 5000 : 2000) }
}
async function load() {
  busy.value = true; error.value = ''; discard.value = false
  try { const data = await api<CaptchaSettings>('/api/v1/captcha/config', { signal: controller.signal }); apply(data) }
  catch (e) { if (!controller.signal.aborted) error.value = e instanceof Error ? e.message : '加载失败' } finally { busy.value = false }
}
async function save() {
  if (!draft.value || busy.value) return
  busy.value = true; error.value = ''; notice.value = ''
  const sent = JSON.parse(JSON.stringify(draft.value)) as CaptchaSettings
  try {
    const { source, revision, max_retries, channels } = sent
    const saved = await api<CaptchaSettings>('/api/v1/captcha/config', { method: 'PUT', body: JSON.stringify({ source, revision, max_retries, channels: channels.map(({ configured: _configured, ...ch }) => ch) }), signal: controller.signal })
    if (JSON.stringify(draft.value) === JSON.stringify(sent)) apply(saved)
    else { const { activity: records, ...config } = saved; activity.value = records || []; draft.value.revision = saved.revision; reconcileCaptcha(draft.value.channels, sent.channels, saved.channels); baseline.value = JSON.stringify(config) }
    notice.value = '个人打码配置已保存，仅对你的账号生效。'
  } catch (e) { if (!controller.signal.aborted) error.value = e instanceof Error ? e.message : '保存失败' } finally { busy.value = false }
}
function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value) { event.preventDefault(); event.returnValue = '' } }
defineExpose({ dirty, flush: autosave.flush })
onMounted(() => { void load(); void refreshTest(); clockTimer = window.setInterval(() => { now.value = Date.now() }, 1000); window.addEventListener('beforeunload', beforeUnload) })
onUnmounted(() => { controller.abort(); window.clearTimeout(probeTimer); window.clearInterval(clockTimer); window.removeEventListener('beforeunload', beforeUnload) })
</script>

<template>
  <section class="panel personal-captcha-panel captcha-panel">
    <div class="panel-title"><div><p class="eyebrow">验证码设置</p><h2>我的打码服务</h2></div><span class="pill soft">个人配置</span></div>
    <p class="muted">用于游戏签到、米游币任务与短信安全验证。仅在米游社要求验证码时调用，普通网络超时不会触发打码。</p>
    <div v-if="!draft && busy" class="empty" role="status">正在读取配置…</div>
    <form v-if="draft" ref="form" @submit.prevent="save">
      <FloatingSave label="保存个人打码配置" :active="dirty || busy" :busy="busy" :disabled="draft.source === 'site' && !siteAllowed" />
      <AutoSaveStatus :state="autosave.state.value" :error="error" />
      <fieldset class="settings-fields">
        <div class="captcha-source-grid" aria-label="打码来源">
          <label class="source-card" :class="{ selected: draft.source === 'off' }"><AppIcon name="stop" :size="21" /><strong>暂不使用</strong><small>遇到安全验证时提示处理</small><input v-model="draft.source" type="radio" name="captcha-source" value="off" aria-label="暂不使用打码" /></label>
          <label class="source-card" :class="{ selected: draft.source === 'personal' }"><AppIcon name="settings" :size="21" /><strong>个人打码服务</strong><small>自行配置接口地址或打码狗</small><input v-model="draft.source" type="radio" name="captcha-source" value="personal" aria-label="使用自己的打码服务" /></label>
          <label class="source-card" :class="{ selected: draft.source === 'site', locked: !siteAllowed }"><AppIcon name="shield" :size="21" /><strong>站点提供的服务</strong><small>{{ !siteAllowed ? '需管理员单独开通权限' : draft.site_available ? '已授权 · 无需填写服务地址' : '已授权 · 管理员尚未启用服务' }}</small><input v-model="draft.source" type="radio" name="captcha-source" value="site" :disabled="!siteAllowed" aria-label="使用站点打码服务" /></label>
        </div>
        <p v-if="!siteAllowed" class="scope-note"><AppIcon name="info" :size="17" />站点服务需单独授权；配置自己的公网打码服务不需要管理员许可。</p>
        <div v-if="draft.source !== 'off'" class="captcha-policy"><label>验证码重试上限<input v-model.number="draft.max_retries" type="number" min="0" max="10" required /></label><p>{{ draft.source === 'site' ? '实际次数不超过管理员设置的站点上限。授权被收回后，不会继续调用站点服务。' : '按渠道顺序尝试，成功后停止。打码狗可能产生平台费用，请合理设置。' }}设为 0 时不自动打码。</p></div>
        <CaptchaEditor v-if="draft.source === 'personal'" v-model="channels" scope="personal" />
        <div v-else-if="draft.source === 'site'" class="subtle-card"><h3>{{ !siteAllowed ? '未获站点服务授权' : draft.site_available ? '已选择站点打码服务' : '站点服务暂不可用' }}</h3><p class="muted">{{ !siteAllowed ? '请联系管理员授权，或改用个人服务。当前不会调用站点打码。' : '服务地址和密钥由管理员维护。选择站点服务后，不会调用你的个人渠道。' }}</p></div>
      </fieldset>
      <p v-if="notice" class="notice" role="status">{{ notice }}</p><p v-if="error" class="error-banner" role="alert">{{ error }}</p>
      <div class="settings-save-bar"><button data-save-inline class="button primary" :disabled="busy || !dirty || draft.source === 'site' && !siteAllowed">{{ busy ? '处理中…' : '保存个人打码配置' }}<AppIcon name="check" :size="16" /></button><button type="button" class="small-button" :disabled="busy" @click="dirty ? discard = true : load()">重新加载</button><small>{{ dirty ? '有未保存的修改' : '配置已保存' }}</small></div>
    </form>
    <p v-if="error && !draft" class="error-banner" role="alert">{{ error }}<button class="text-button" @click="load">重试</button></p>
    <section class="captcha-activity" aria-labelledby="captcha-activity-title">
      <div class="push-section-title"><h3 id="captcha-activity-title">验证码调用记录</h3><button class="text-button" @click="refreshTest"><AppIcon name="refresh" :size="14" />刷新记录与测试状态</button></div>
      <button class="small-button" :disabled="busy || dirty || testing || testCooldown > 0 || !draft || draft.source === 'off'" @click="confirmTest = true; testError = ''"><AppIcon name="shield" :size="15" />{{ testing ? '测试正在后台运行' : testCooldown ? testCooldown + ' 秒后可再次测试' : '测试自定义打码服务' }}</button>
      <div v-if="probe" class="captcha-probe-status subtle-card" :class="{ 'error-ink': probe.status === 'failed' || probe.status === 'interrupted' }" role="status"><strong>{{ probe.status === 'running' ? '后台测试中' : probe.status === 'succeeded' ? '测试已完成' : probe.status === 'interrupted' ? '测试已中断' : '测试未通过' }}</strong><p>{{ probe.message }}</p><small>{{ formatDate(probe.started_at, 'Asia/Shanghai') }}<template v-if="probe.status !== 'running'"> · 耗时 {{ (probe.duration_ms / 1000).toFixed(1) }} 秒</template></small></div>
      <p v-if="testError && !confirmTest" class="error-banner" role="alert">{{ testError }}</p>
      <p class="muted">记录最近 50 次打码调用，不保存验证码内容或密钥。「已返回结果」表示服务生成了校验参数；验证码是否通过，请查看对应任务的后续结果。</p>
      <p v-if="activityError" class="error-text">{{ activityError }}</p>
      <div v-if="activity.length" class="captcha-activity-list"><article v-for="(entry, index) in [...activity].reverse()" :key="entry.at + index"><div><strong>{{ entry.provider === 'damagou' ? '打码狗' : '自定义服务' }} · {{ entry.source === 'site' ? '站点渠道' : '个人渠道' }}</strong><small>{{ formatDate(entry.at, 'Asia/Shanghai') }} · {{ entry.duration_ms }} ms · {{ entry.kind === 'test' ? '匿名测试' : '任务验证' }}</small></div><span class="pill" :class="{ soft: entry.ok, warning: !entry.ok }">{{ entry.ok ? '已返回结果' : entry.code === 'balance' ? '余额不足' : entry.code === 'permission' ? '授权已变化' : '未获取到结果' }}</span></article></div>
      <p v-else class="scope-note">暂无调用记录。已完成的任务、未遇验证码的任务不会调用打码服务；旧版本的调用不会补记到这里。</p>
    </section>
  </section>
  <ModalShell v-if="discard" title="放弃未保存的打码修改？" @close="discard = false"><p class="muted">重新加载会读取服务器上的最新配置，并清除当前草稿。</p><div class="modal-actions"><button class="small-button" @click="discard = false">继续编辑</button><button class="button primary" @click="load">放弃并重新加载</button></div></ModalShell>
  <ModalShell v-if="confirmTest" title="测试自定义打码服务" @close="confirmTest = false"><p class="muted">获取一个匿名验证码，测试当前已保存来源中的第一个启用的自定义渠道。不会使用打码狗，不执行签到、兑换、短信或推送；自定义服务的计费规则由提供方决定。</p><p class="muted">后台执行，关闭页面不会取消。每次测试间隔至少 60 秒，最长运行 65 秒，不自动重试。结果保存在此页和运行日志；仅检查能否获取校验参数，不验证实际账号任务。</p><p v-if="testError" class="error-banner" role="alert">{{ testError }}</p><div class="modal-actions"><button class="button primary" :disabled="testing || dirty || testCooldown > 0" @click="testService">{{ submittingTest ? '正在提交…' : '开始测试' }}</button><button class="small-button" @click="confirmTest = false">暂不测试</button></div></ModalShell>
</template>

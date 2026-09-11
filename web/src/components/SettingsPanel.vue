<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api } from '../api'
import AppIcon from './AppIcon.vue'
import CaptchaEditor from './CaptchaEditor.vue'
import FloatingSave from './FloatingSave.vue'
import AutoSaveStatus from './AutoSaveStatus.vue'
import TimeField from './TimeField.vue'
import { reconcileCaptcha, useAutoSave } from '../autosave'
import type { Config } from '../types'
const props = defineProps<{ config: Config; admin: boolean }>()
const emit = defineEmits<{ saved: []; navigate: [view: 'dashboard' | 'captcha' | 'notifications' | 'admin'] }>()
function siteConfig(config: Config) { const { enabled, schedule, captcha, shop_exchange } = config; return JSON.parse(JSON.stringify({ enabled, schedule, captcha, network: { ...config.network, bbs_state_retries: config.network?.bbs_state_retries ?? 5 }, shop_exchange: { enable: shop_exchange.enable, retry_seconds: shop_exchange.retry_seconds, retry_interval: shop_exchange.retry_interval } })) as Pick<Config, 'enabled' | 'schedule' | 'captcha' | 'network'> & { shop_exchange: Omit<Config['shop_exchange'], 'plans'> } }
const draft = ref(siteConfig(props.config)), baseline = ref(JSON.stringify(draft.value))
const busy = ref(false), error = ref(''), notice = ref('')
const dirty = computed(() => JSON.stringify(draft.value) !== baseline.value)
const form = ref<HTMLFormElement | null>(null)
const autosave = useAutoSave(() => JSON.stringify(draft.value), dirty, busy, () => props.admin && !!form.value?.checkValidity(), save)
defineExpose({ dirty, flush: autosave.flush })
async function save() {
  if (busy.value) return
  const sent = JSON.parse(JSON.stringify(draft.value)) as typeof draft.value
  busy.value = true; error.value = ''; notice.value = ''
  try {
    const saved = siteConfig(await api<Config>('/api/v1/config', { method: 'PUT', body: JSON.stringify({ ...sent, captcha: { ...sent.captcha, channels: sent.captcha.channels.map(({ configured: _configured, ...ch }) => ch) } }) }))
    if (JSON.stringify(draft.value) === JSON.stringify(sent)) draft.value = saved
    else reconcileCaptcha(draft.value.captcha.channels, sent.captcha.channels, saved.captcha.channels)
    baseline.value = JSON.stringify(saved)
    notice.value = '设置已保存并生效'; emit('saved')
  } catch (e) { error.value = e instanceof Error ? e.message : '保存失败' } finally { busy.value = false }
}
function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value) { event.preventDefault(); event.returnValue = '' } }
onMounted(() => window.addEventListener('beforeunload', beforeUnload))
onUnmounted(() => window.removeEventListener('beforeunload', beforeUnload))
</script>

<template>
  <form ref="form" class="settings-form" @submit.prevent="save">
    <FloatingSave v-if="admin" label="保存设置" :active="dirty || busy" :busy="busy" />
    <AutoSaveStatus :state="autosave.state.value" :error="error" />
    <div class="settings-scope"><AppIcon name="settings" :size="21" /><div><strong>站点基础服务</strong><p>这里只管理全站运行与公共资源。具体签到项目由每个账号自己选择。</p></div><span class="pill">管理员</span></div>
    <div class="settings-shortcuts"><button type="button" @click="emit('navigate', 'dashboard')"><AppIcon name="check" :size="17" />账号签到设置<AppIcon name="arrow" :size="14" /></button><button type="button" @click="emit('navigate', 'captcha')"><AppIcon name="shield" :size="17" />我的打码服务<AppIcon name="arrow" :size="14" /></button><button type="button" @click="emit('navigate', 'admin')"><AppIcon name="user" :size="17" />用户与服务授权<AppIcon name="arrow" :size="14" /></button></div>
    <fieldset :disabled="!admin" class="settings-fields">
      <article class="panel"><div class="panel-title"><div><p class="eyebrow">运行设置</p><h2>运行与每日调度</h2></div></div>
        <label class="preference-switch"><span><strong>允许执行签到任务</strong><small>站点维护总开关；关闭时暂停所有账号的签到任务，不修改个人任务选择。</small></span><span class="push-switch"><input v-model="draft.enabled" type="checkbox" aria-label="允许执行签到任务" /><span aria-hidden="true"></span></span></label>
        <div class="check-grid"><label><input v-model="draft.schedule.enable" type="checkbox" />开启站点每日调度</label><label><input v-model="draft.schedule.run_on_start" type="checkbox" />服务启动时运行一次</label></div>
        <p class="muted">这是未设自定义时间的账号所用的默认调度。已设置个人时间的账号优先按自己的时间执行，不受此处默认调度开关影响。</p>
        <div class="form-grid"><label>默认签到时间<TimeField v-model="draft.schedule.time" label="默认签到时间" /></label><label>默认时区<input v-model="draft.schedule.timezone" required placeholder="Asia/Shanghai" /></label><label>默认调度随机延迟上限（分钟）<input v-model.number="draft.schedule.jitter_minutes" type="number" min="0" max="720" required /></label></div>
      </article>
      <article class="panel"><div class="panel-title"><div><p class="eyebrow">站点网络</p><h2>连接与重试</h2></div></div>
        <label>米游币状态查询重试次数<input v-model.number="draft.network.bbs_state_retries" type="number" min="0" max="10" required /></label>
        <p class="muted">默认重试 5 次（最多查询 6 次），设为 0 则不重试。只对网络异常或上游暂时不可用的状态查询生效，不重发签到、点赞、短信或兑换请求；验证码重试在下方单独配置。</p>
      </article>
      <article class="panel captcha-panel"><div class="panel-title"><div><p class="eyebrow">共享服务</p><h2>站点打码服务</h2></div><span class="pill soft">需单独授权</span></div><p class="muted">供已授权用户选择使用。个人渠道在「打码服务」里自行配置，不在这里共享。</p>
        <div class="captcha-policy"><label>站点验证码重试上限<input v-model.number="draft.captcha.max_retries" type="number" min="0" max="10" required /></label><p>这是站点服务的调用上限，用户可以设置更低的次数。设为 0 则停用站点自动打码。</p></div>
        <CaptchaEditor v-model="draft.captcha" scope="site" :disabled="!admin" />
      </article>
      <article class="panel"><div class="panel-title"><div><p class="eyebrow">兑换设置</p><h2>兑换基础服务</h2></div><span class="pill soft">需单独授权</span></div><label class="preference-switch"><span><strong>允许商品兑换</strong><small>开启后仍需在「后台管理」为普通用户授予兑换权限；关闭不会删除历史计划。</small></span><span class="push-switch"><input v-model="draft.shop_exchange.enable" type="checkbox" aria-label="允许商品兑换" /><span aria-hidden="true"></span></span></label>
        <details class="preference-advanced"><summary>重试与保护参数<AppIcon name="chevron" :size="14" /></summary><p class="muted">明确返回兑换失败、尚未开售或繁忙时，在窗口内自动重试；每个计划最多请求 60 次。成功、余额不足、限购或需要验证时停止。超时等无法确认结果的请求不会重发。</p><div class="form-grid"><label>重试窗口（秒，0 为仅请求一次）<input v-model.number="draft.shop_exchange.retry_seconds" type="number" min="0" max="120" step="0.1" required /></label><label>重试间隔（秒，实际不低于 0.2）<input v-model.number="draft.shop_exchange.retry_interval" type="number" min="0.05" max="30" step="0.05" required /></label></div><p class="muted">同一账号的不同商品会同时准备，实际请求依次发送。重试窗口结束后，不再开始新请求，但会等待已发出的请求返回（最多 15 秒）。</p></details>
      </article>
    </fieldset>
    <p v-if="error" class="error-banner" role="alert">{{ error }}</p><p v-if="notice" class="notice" role="status">{{ notice }}</p><div class="settings-save-bar"><button v-if="admin" data-save-inline class="button primary" :disabled="busy || !dirty">{{ busy ? '保存中…' : '保存设置' }}</button><small>{{ dirty ? '有未保存的站点修改' : '设置已保存' }}</small></div>
  </form>
</template>

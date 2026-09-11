<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api } from '../api'
import { emptyChannel, providerInfo, pushProviders, secretKeys, type PushTextKey } from '../push'
import type { PushChannel, PushDelivery, PushProvider, PushResult, PushSecretKey, PushSettings, PushTestResult } from '../types'
import { formatDate } from '../time'
import AppIcon from './AppIcon.vue'
import ProviderIcon from './ProviderIcon.vue'
import ModalShell from './ModalShell.vue'
import PushBindingModal from './PushBindingModal.vue'
import ChoiceSelect from './ChoiceSelect.vue'
import AdaptiveEditor from './AdaptiveEditor.vue'
import FloatingSave from './FloatingSave.vue'
import AutoSaveStatus from './AutoSaveStatus.vue'
import { useAutoSave } from '../autosave'

const props = withDefaults(defineProps<{ timezone?: string }>(), { timezone: 'Asia/Shanghai' })
const emit = defineEmits<{ saved: [] }>()
type ChannelDraft = PushChannel & { ui_key: string; clear_fields: PushSecretKey[] }
type Draft = Omit<PushSettings, 'channels'> & { channels: ChannelDraft[] }
const draft = ref<Draft | null>(null), baseline = ref('')
const loading = ref(false), saving = ref(false), testing = ref(''), error = ref(''), notice = ref('')
const expanded = ref<Record<string, boolean>>({}), results = ref<Record<string, PushResult>>({})
const candidate = ref<PushProvider | null>(null), dialogChannel = ref(''), candidateEnabled = ref(true)
const candidateInfo = computed(() => candidate.value ? providerInfo(candidate.value) : null)
const removeKey = ref(''), confirmReload = ref(false)
const binding = ref<{ provider: 'qqbot' | 'wechat_claw'; channelID: string; revision: number } | null>(null)
const history = ref<PushDelivery[]>([]), historyError = ref(''), bindingStates = ref<Record<string, string>>({})
const bindingErrors = ref<Record<string, string>>({})
const mobile = ref(window.innerWidth <= 640)
function resized() { const next = window.innerWidth <= 640; if (next !== mobile.value) { expanded.value = {}; mobile.value = next } }
let activityTimer: number | undefined, activityBusy = false
const controller = new AbortController()
let active = true, nextKey = 0
const dirty = computed(() => !!draft.value && JSON.stringify(draft.value) !== baseline.value)
const busy = computed(() => loading.value || saving.value || !!testing.value)
const editLocked = computed(() => loading.value || !!testing.value)
const form = ref<HTMLFormElement | null>(null)
function validDraft() {
  return !!draft.value && !!form.value?.checkValidity() && (!draft.value.enable || draft.value.channels.some(channel => channel.enable)) && draft.value.channels.every(channel => channel.id && (!channel.enable || channelFields(channel).every(field => !field.required || !!channel[field.key]?.trim() || configured(channel, field.key))))
}
const autosave = useAutoSave(() => JSON.stringify(draft.value), dirty, busy, validDraft, save)
const enabledCount = computed(() => draft.value?.channels.filter(channel => channel.enable).length ?? 0)
const removing = computed(() => draft.value?.channels.find(channel => channel.ui_key === removeKey.value))
const options = [
  { key: 'tasks', icon: 'check', title: '签到结果', description: '游戏、云游戏与米游币，按账号汇总' },
  { key: 'exchange', icon: 'gift', title: '兑换结果', description: '成功、失败、停止与结果待确认' },
  { key: 'error_only', icon: 'filter', title: '仅提醒异常', description: '不发送正常完成的自动通知' },
] as const
const deliveryLabels: Record<string, string> = { pending: '等待发送', sending: '发送中', accepted: '服务已接收', failed: '发送失败', unknown: '发送结果待确认', skipped: '已跳过' }
const bindingLabels: Record<string, string> = { ready: '通知会话已就绪', waiting_message: '请先在微信发送一条消息', expired: '登录已过期，请重新扫码', reconnecting: '连接中断，正在重连' }
const qqLabels: Record<string, string> = { connecting: '正在连接 QQ 网关', ready: 'QQ 机器人已上线', reconnecting: 'QQ 连接中断，正在重试', disconnected: 'QQ 网关已断开', error: 'QQ 连接需要处理' }
defineExpose({ dirty, flush: autosave.flush })

function channelFields(channel: ChannelDraft) { return channel.provider === 'wechat_claw' && channel.mode === 'ilink' ? [] : providerInfo(channel.provider).fields }
function openBinding(provider: 'qqbot' | 'wechat_claw', channelID = '') {
  if (!draft.value || busy.value) return
  if (dirty.value) { error.value = '请先保存或放弃当前修改，再开始扫码。'; return }
  if (mobile.value) expanded.value = {}
  binding.value = { provider, channelID, revision: draft.value.revision }
}
async function bound(message: string) { await load(); notice.value = message; await syncActivity() }
async function syncActivity() {
  if (activityBusy || !active || document.hidden) return
  activityBusy = true
  try {
    const [records, config] = await Promise.all([api<PushDelivery[]>('/api/v1/push/history', { signal: controller.signal }), api<PushSettings>('/api/v1/push/config', { signal: controller.signal })])
    if (active) {
      history.value = records; historyError.value = ''; bindingStates.value = Object.fromEntries(config.channels.map(c => [c.id, c.binding_state])); bindingErrors.value = Object.fromEntries(config.channels.map(c => [c.id, c.binding_error || '']))
      // A QR task may complete after its dialog is closed or in another tab.
      // Synchronize saved channels without overwriting an unfinished edit.
      if (draft.value && config.revision > draft.value.revision && !dirty.value && !busy.value) apply(config)
    }
  } catch { if (active) historyError.value = '发送记录暂时无法刷新，正在重试。' }
  finally { activityBusy = false }
}

function apply(data: PushSettings) {
  const open = draft.value?.channels.map(channel => !!expanded.value[channel.ui_key]) || []
  draft.value = { ...data, channels: data.channels.map(channel => ({ ...channel, ui_key: channel.id || 'new_' + nextKey++, clear_fields: [] })) }
  draft.value.channels.forEach((channel, index) => { if (open[index]) expanded.value[channel.ui_key] = true })
  baseline.value = JSON.stringify(draft.value)
  results.value = {}
}
async function load() {
  confirmReload.value = false; loading.value = true; error.value = ''; notice.value = ''
  try {
    const data = await api<PushSettings>('/api/v1/push/config', { signal: controller.signal })
    if (active) { apply(data); if (data.channels[0] && !mobile.value) expanded.value[data.channels[0].id] = true }
  } catch (e) { if (active) error.value = e instanceof Error ? e.message : '推送配置加载失败' }
  finally { if (active) loading.value = false }
}
function reload() { if (dirty.value) confirmReload.value = true; else void load() }
function selectProvider(provider: PushProvider) { candidate.value = provider; candidateEnabled.value = true }
function addChannel() {
  if (!draft.value || !candidate.value || draft.value.channels.length >= 10 || dialogChannel.value) return
  const ui_key = 'new_' + nextKey++
  draft.value.channels.push({ ...emptyChannel(candidate.value), enable: candidateEnabled.value, ui_key, clear_fields: [] })
  expanded.value[ui_key] = true; dialogChannel.value = ui_key; notice.value = ''; error.value = ''
}
function closeChannel(channel: ChannelDraft) {
  expanded.value[channel.ui_key] = false
  if (dialogChannel.value === channel.ui_key) {
    if (!channel.id && draft.value) draft.value.channels = draft.value.channels.filter(item => item.ui_key !== channel.ui_key)
    dialogChannel.value = ''
  }
}
async function candidateBinding(provider: 'qqbot' | 'wechat_claw') {
  if (dirty.value && !await autosave.flush()) { error.value = '请先保存当前配置再扫码绑定。'; return }
  candidate.value = null; openBinding(provider)
}
function removeChannel() {
  if (draft.value) draft.value.channels = draft.value.channels.filter(channel => channel.ui_key !== removeKey.value)
  removeKey.value = ''; notice.value = ''
}
function isSecret(key: PushTextKey): key is PushSecretKey { return (secretKeys as string[]).includes(key) }
function configured(channel: ChannelDraft, key: PushTextKey) { return isSecret(key) && channel.configured.includes(key) && !channel.clear_fields.includes(key) }
function setField(channel: ChannelDraft, key: PushTextKey, event: Event) {
  channel[key] = (event.target as HTMLInputElement).value
  if (isSecret(key) && channel[key]) channel.clear_fields = channel.clear_fields.filter(field => field !== key)
}
function toggleClear(channel: ChannelDraft, key: PushTextKey) {
  if (!isSecret(key)) return
  if (channel.clear_fields.includes(key)) channel.clear_fields = channel.clear_fields.filter(field => field !== key)
  else { channel[key] = ''; channel.clear_fields.push(key) }
}
function setTLS(channel: ChannelDraft, value: string) {
  channel.smtp_ssl = value === 'tls'
  if ([465, 587].includes(channel.smtp_port)) channel.smtp_port = channel.smtp_ssl ? 465 : 587
}
async function saveAndClose() { await save(); if (!error.value && !dirty.value) { expanded.value = {}; dialogChannel.value = ''; candidate.value = null } }
async function save() {
  if (!draft.value || busy.value) return
  const sent = JSON.parse(JSON.stringify(draft.value)) as Draft
  saving.value = true; error.value = ''; notice.value = ''
  try {
    const { channels, ...settings } = sent
    const payload = { ...settings, channels: channels.map(({ configured: _configured, ui_key: _key, ...channel }) => channel) }
    const data = await api<PushSettings>('/api/v1/push/config', { method: 'PUT', body: JSON.stringify(payload), signal: controller.signal })
    if (active) {
      if (JSON.stringify(draft.value) === JSON.stringify(sent)) apply(data)
      else {
        const saved: Draft = { ...data, channels: data.channels.map((channel, index) => ({ ...channel, ui_key: sent.channels[index]!.ui_key, clear_fields: [] })) }
        draft.value.revision = saved.revision
        for (const channel of draft.value.channels) {
          const before = sent.channels.find(item => item.ui_key === channel.ui_key), after = saved.channels.find(item => item.ui_key === channel.ui_key)
          if (!before || !after) continue
          channel.id = after.id; channel.configured = after.configured
          for (const key of secretKeys) if (channel[key] === before[key]) channel[key] = after[key]
          if (JSON.stringify(channel.clear_fields) === JSON.stringify(before.clear_fields)) channel.clear_fields = []
        }
        baseline.value = JSON.stringify(saved)
      }
      notice.value = '推送配置已保存，密钥不会回传到浏览器。'; emit('saved')
    }
  } catch (e) { if (active) error.value = e instanceof Error ? e.message : '保存失败' }
  finally { if (active) saving.value = false }
}
async function test(channel: ChannelDraft) {
  if (!draft.value || dirty.value || busy.value || !channel.id) return
  testing.value = channel.id; error.value = ''; notice.value = ''; delete results.value[channel.id]
  try {
    const result = await api<PushTestResult>('/api/v1/push/test', { method: 'POST', body: JSON.stringify({ channel_id: channel.id, revision: draft.value.revision }), signal: controller.signal })
    if (active) { result.results.forEach(item => { results.value[item.channel_id] = item }); await syncActivity() }
  } catch (e) { if (active) error.value = e instanceof Error ? e.message : '测试失败' }
  finally { if (active) testing.value = '' }
}
function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value) { event.preventDefault(); event.returnValue = '' } }
onMounted(() => { void load(); void syncActivity(); activityTimer = window.setInterval(syncActivity, 5000); window.addEventListener('beforeunload', beforeUnload); window.addEventListener('resize', resized) })
onUnmounted(() => { active = false; window.clearInterval(activityTimer); controller.abort(); window.removeEventListener('beforeunload', beforeUnload); window.removeEventListener('resize', resized) })
</script>

<template>
  <section class="panel push-panel" aria-labelledby="push-heading">
    <div class="panel-title push-heading">
      <div class="push-heading-copy"><span class="push-emblem"><AppIcon name="bell" :size="23" /></span><div><p class="eyebrow">通知配置</p><h2 id="push-heading">通知设置</h2></div></div>
      <span v-if="draft" class="pill" :class="{ soft: draft.enable }">{{ dirty ? '有未保存修改' : draft.enable ? '自动推送已开启' : '自动推送已关闭' }}</span>
    </div>
      <div v-if="loading && !draft" class="push-loading" role="status"><span class="loading-orbit small"></span>正在读取推送配置…</div>
      <form v-if="draft" ref="form" @submit.prevent="save">
        <FloatingSave label="保存推送配置" :active="dirty || saving" :busy="saving" :disabled="busy" />
        <AutoSaveStatus :state="autosave.state.value" :error="error" />
        <fieldset :disabled="editLocked" class="push-fields">
          <div class="push-quick-bind" aria-label="扫码连接通知">
            <button type="button" class="quick-bind-card" :disabled="dirty || draft.channels.length >= 10" @click="openBinding('qqbot')"><span class="push-provider-mark" data-tone="blue"><ProviderIcon name="qqbot" /></span><span><strong>QQ 扫码绑定</strong><small>扫码创建或选择机器人</small></span><AppIcon name="scan" :size="21" /></button>
            <button type="button" class="quick-bind-card weixin-bind" :disabled="dirty || draft.channels.length >= 10" @click="openBinding('wechat_claw')"><span class="push-provider-mark" data-tone="sage"><ProviderIcon name="wechat_claw" /></span><span><strong>微信扫码绑定</strong><small>通过微信 iLink 连接</small></span><AppIcon name="scan" :size="21" /></button>
          </div>
          <label class="push-master"><span><strong>自动发送任务结果</strong><small>按下方选项，发送你绑定的米游社账号的任务结果。关闭后仍可在站内查看。</small></span><span class="push-switch"><input v-model="draft.enable" type="checkbox" aria-label="启用自动推送" /><span aria-hidden="true"></span></span></label>
          <div class="push-options"><label v-for="option in options" :key="option.key" class="push-option"><AppIcon :name="option.icon" :size="19" /><span><strong>{{ option.title }}</strong><small>{{ option.description }}</small></span><input v-model="draft[option.key]" type="checkbox" :aria-label="option.title" /></label></div>
          <div class="push-section-title"><h3>选择接收方式</h3><small>点击卡片配置 · 可同时启用</small></div>
          <div class="push-provider-grid" role="group" aria-label="选择推送渠道">
            <button v-for="provider in pushProviders" :key="provider.key" type="button" class="push-provider-tile" :data-provider="provider.key" @click="selectProvider(provider.key)"><span class="push-provider-mark" :data-tone="provider.tone"><ProviderIcon :name="provider.key" /></span><span class="push-provider-name">{{ provider.name }}</span><small>{{ draft.channels.some(channel => channel.provider === provider.key) ? '已添加 · 管理或新增' : '点击配置' }}</small></button>
          </div>
          <div class="push-section-title"><h3>接收渠道 <span class="count-label">{{ draft.channels.length }} / 10</span></h3><small>{{ enabledCount }} 个已启用</small></div>
          <p v-if="!draft.channels.length" class="muted">尚未配置渠道，请点击上方的接收方式进行配置。</p>
          <div class="push-channel-list">
            <article v-for="channel in draft.channels" :key="channel.ui_key" class="push-channel" :class="{ 'is-open': expanded[channel.ui_key] }">
              <div class="push-channel-heading">
                <button type="button" class="push-channel-expand" :aria-expanded="!!expanded[channel.ui_key]" :aria-controls="'push-body-' + channel.ui_key" @click="expanded[channel.ui_key] = !expanded[channel.ui_key]">
                  <span class="push-provider-mark" :data-tone="providerInfo(channel.provider).tone"><ProviderIcon :name="channel.provider" /></span><span class="push-channel-identity"><strong>{{ channel.name || providerInfo(channel.provider).name }}</strong><small>{{ providerInfo(channel.provider).name }} · {{ channel.id ? '已保存' : '新渠道，待保存' }}</small></span><AppIcon name="chevron" :size="15" />
                </button>
                <label class="push-channel-toggle"><input v-model="channel.enable" type="checkbox" :aria-label="'启用渠道 ' + channel.name" />启用</label>
              </div>
              <AdaptiveEditor v-if="expanded[channel.ui_key]" :mobile="mobile || dialogChannel === channel.ui_key" :content-id="'push-body-' + channel.ui_key" :title="'配置 ' + (channel.name || providerInfo(channel.provider).name)" :busy="editLocked" @close="closeChannel(channel)" @save="saveAndClose">
                <FloatingSave v-if="mobile || dialogChannel === channel.ui_key" label="保存渠道配置" :active="dirty || saving" :busy="saving" :disabled="busy" />
                <label v-if="mobile || dialogChannel === channel.ui_key" class="push-master push-channel-master"><span><strong>启用这个接收渠道</strong><small>{{ draft.enable ? '保存后接收已勾选的任务结果。' : '自动推送当前关闭，保存渠道不会打开总开关。' }}</small></span><span class="push-switch"><input v-model="channel.enable" type="checkbox" aria-label="启用这个接收渠道" /><span aria-hidden="true"></span></span></label>
                <p class="push-channel-hint">{{ providerInfo(channel.provider).hint }}</p>
                <div v-if="channel.provider === 'qqbot' || channel.provider === 'wechat_claw'" class="push-binding-status">
                  <span v-if="channel.provider === 'qqbot'" class="pill" :class="{ soft: (bindingStates[channel.id] || channel.binding_state) === 'ready' }">{{ qqLabels[bindingStates[channel.id] || channel.binding_state] || '等待 QQ 连接状态' }}</span>
                  <span v-if="channel.provider === 'wechat_claw' && channel.mode === 'ilink'" class="pill" :class="{ soft: channel.enable && bindingStates[channel.id] === 'ready' }">{{ !channel.enable ? '渠道已关闭 · 接收已暂停' : bindingLabels[bindingStates[channel.id] || channel.binding_state] || '等待连接' }}</span>
                  <button type="button" class="small-button" :disabled="dirty || !channel.id" @click="openBinding(channel.provider, channel.id)"><AppIcon name="scan" :size="15" />{{ channel.configured.includes('token') || channel.configured.includes('client_secret') ? '重新扫码绑定' : '扫码配置' }}</button>
                </div>
                <p v-if="channel.provider === 'wechat_claw' && channel.mode === 'ilink'" class="push-channel-hint">绑定用户：{{ channel.openid || '尚未绑定' }}。启用渠道时会保持官方消息连接；关闭渠道即可停止连接。</p>
                <p v-if="channel.provider === 'qqbot'" class="push-channel-hint">绑定后保持 QQ 官方连接，不回复或保存聊天。渠道开关只控制通知发送；删除渠道或清除 ClientSecret 后断开连接。</p>
                <p v-if="channel.provider === 'qqbot' && (bindingErrors[channel.id] ?? channel.binding_error)" class="error-text" role="status">{{ bindingErrors[channel.id] ?? channel.binding_error }}</p>
                <p v-if="channel.provider === 'wechat_claw' && channel.mode === 'ilink' && channel.enable && (bindingErrors[channel.id] ?? channel.binding_error)" class="error-text" role="status">{{ bindingErrors[channel.id] ?? channel.binding_error }}</p>
                <div class="push-input-grid">
                  <label class="push-input"><span>渠道名称</span><input v-model="channel.name" maxlength="64" required :aria-label="providerInfo(channel.provider).name + ' 渠道名称'" /></label>
                  <label v-if="channel.provider === 'qq'" class="push-input"><span>消息类型</span><ChoiceSelect v-model="channel.msg_type" label="消息类型" :options="[{ value: 'private', label: '私聊' }, { value: 'group', label: '群消息' }]" :disabled="busy" /></label>
                  <label v-if="channel.provider === 'email'" class="push-input"><span>传输加密</span><ChoiceSelect :model-value="channel.smtp_ssl ? 'tls' : 'starttls'" label="传输加密" :options="[{ value: 'tls', label: '隐式 TLS · 通常为 465' }, { value: 'starttls', label: 'STARTTLS · 通常为 587' }]" :disabled="busy" @change="setTLS(channel, $event)" /></label>
                  <label v-if="channel.provider === 'email'" class="push-input"><span>SMTP 端口</span><input v-model.number="channel.smtp_port" type="number" min="1" max="65535" required /></label>
                  <div v-for="field in channelFields(channel)" :key="field.key" class="push-input-wrap">
                    <label class="push-input"><span>{{ field.label }}<small v-if="configured(channel, field.key)" class="push-configured">已配置</small></span><input :value="channel[field.key]" :type="isSecret(field.key) ? 'password' : 'text'" :autocomplete="isSecret(field.key) ? 'new-password' : 'off'" :placeholder="configured(channel, field.key) ? '留空保留已保存值' : field.placeholder || (field.required ? '启用或测试前填写' : '可选')" :required="channel.enable && field.required && !configured(channel, field.key)" maxlength="4096" spellcheck="false" @input="setField(channel, field.key, $event)" /></label>
                    <button v-if="isSecret(field.key) && channel.configured.includes(field.key)" type="button" class="push-clear text-button" :class="{ 'error-ink': channel.clear_fields.includes(field.key) }" @click="toggleClear(channel, field.key)">{{ channel.clear_fields.includes(field.key) ? '保存后清除 · 撤销' : '清除已保存值' }}</button>
                  </div>
                </div>
                <div v-if="results[channel.id]" class="push-test-result" :class="{ 'is-error': !results[channel.id]?.ok }" role="status"><AppIcon :name="results[channel.id]?.ok ? 'check' : 'info'" :size="16" /><span>{{ results[channel.id]?.ok ? '推送服务已接收测试消息。' : results[channel.id]?.error || '推送服务未确认结果' }}</span></div>
                <div class="push-channel-actions"><button type="button" class="small-button" :disabled="dirty || !channel.id || busy" @click="test(channel)"><span v-if="testing && testing === channel.id" class="loading-orbit small"></span><AppIcon v-else name="send" :size="14" />{{ testing && testing === channel.id ? '发送中…' : '发送测试消息' }}</button><span class="push-test-hint">{{ dirty || !channel.id ? '保存后可测试' : '仅测试已保存的配置' }}</span><button type="button" class="text-button error-ink push-remove" @click="removeKey = channel.ui_key"><AppIcon name="trash" :size="14" />删除渠道</button></div>
                <template v-if="mobile || dialogChannel === channel.ui_key"><p v-if="error" class="error-banner" role="alert">{{ error }}</p><div class="modal-actions sticky-modal-actions"><button data-save-inline type="submit" class="button primary" :disabled="busy || !dirty">保存推送配置</button><button type="button" class="small-button" :disabled="busy" @click="closeChannel(channel)">{{ channel.id ? '返回' : '取消添加' }}</button></div></template>
              </AdaptiveEditor>
            </article>
          </div>
        </fieldset>
        <p class="push-privacy"><AppIcon name="lock" :size="14" /><span>密钥与 Webhook 地址不回显，留空保留。通知不包含登录凭据或收货信息；推送失败不会影响任务结果。</span></p>
        <p v-if="error" class="error-banner" role="alert">{{ error }}</p><p v-if="notice" class="notice" role="status">{{ notice }}</p>
        <div class="push-footer"><button data-save-inline class="button primary" :disabled="busy || !dirty"><AppIcon v-if="!saving" name="check" :size="16" />{{ saving ? '保存中…' : '保存推送配置' }}</button><span v-if="dirty" class="push-unsaved">修改尚未生效</span><button type="button" class="text-button" :disabled="busy" @click="reload"><AppIcon name="refresh" :size="14" />重新加载</button></div>
        <p class="push-footnote">手动测试会立即发送，不受自动推送开关影响。重启后继续处理尚未发送的通知；已发出但未确认结果的请求不自动重发。</p>
      </form>
      <div v-else-if="error"><p class="error-banner" role="alert">{{ error }}</p><button class="small-button" :disabled="loading" @click="load">重新加载</button></div>
    <section v-if="draft" class="push-history" aria-labelledby="push-history-title">
      <div class="push-section-title"><h3 id="push-history-title">最近发送记录</h3><button type="button" class="text-button" @click="syncActivity"><AppIcon name="refresh" :size="14" />刷新记录</button></div>
      <p class="muted">记录推送服务的响应，实际接收情况请在对应渠道查看。</p>
      <p v-if="historyError" class="error-text" role="status">{{ historyError }}</p>
      <div v-if="history.length" class="push-history-list"><article v-for="entry in history" :key="entry.id" class="push-delivery"><span class="push-provider-mark" :data-tone="providerInfo(entry.provider).tone"><ProviderIcon :name="entry.provider" /></span><div><strong>{{ entry.title }} · {{ entry.channel_name }}</strong><small>{{ formatDate(entry.created_at, props.timezone) }}</small><p v-if="entry.error">{{ entry.error }}</p></div><span class="pill" :class="{ soft: entry.status === 'accepted', warning: ['failed', 'unknown'].includes(entry.status) }">{{ deliveryLabels[entry.status] || entry.status }}</span></article></div>
      <p v-else class="muted push-history-empty">还没有发送记录。保存渠道后可发送一条测试消息。</p>
    </section>
    <PushBindingModal v-if="binding" :provider="binding.provider" :channel-id="binding.channelID" :revision="binding.revision" @close="binding = null" @bound="bound" />
    <ModalShell v-if="candidateInfo && draft" :title="candidateInfo.name + ' · 接收方式'" :hidden="!!dialogChannel" @close="candidate = null">
      <div class="provider-introduction"><span class="push-provider-mark" :data-tone="candidateInfo.tone"><ProviderIcon :name="candidateInfo.key" /></span><p class="muted">{{ candidateInfo.hint }}</p></div>
      <div v-if="draft.channels.some(channel => channel.provider === candidate)" class="provider-existing"><p class="muted">已添加的渠道</p><button v-for="channel in draft.channels.filter(channel => channel.provider === candidate)" :key="channel.ui_key" class="small-button" @click="candidate = null; expanded[channel.ui_key] = true; dialogChannel = channel.ui_key">{{ channel.name }}<AppIcon name="settings" :size="14" /></button></div>
      <template v-if="draft.channels.length < 10">
        <label v-if="candidate !== 'wechat_claw'" class="check-row"><input v-model="candidateEnabled" type="checkbox" />加入后启用此渠道</label>
        <p class="muted">填写完整并保存后才会加入接收渠道；不会自动发送测试消息。</p>
        <div class="modal-actions"><button v-if="candidate === 'qqbot' || candidate === 'wechat_claw'" class="button primary" :disabled="busy" @click="candidateBinding(candidate)">扫码绑定并加入<AppIcon name="scan" :size="16" /></button><button v-if="candidate !== 'wechat_claw'" class="button" :class="{ primary: candidate !== 'qqbot' }" :disabled="busy" @click="addChannel">{{ candidate === 'qqbot' ? '手动配置并添加' : '配置并添加' }}</button><button class="small-button" @click="candidate = null">暂不添加</button></div>
      </template><p v-else class="scope-note">已达到 10 个渠道上限，可管理已有渠道或先移除不用的渠道。</p>
    </ModalShell>
    <ModalShell v-if="removing" title="删除这个推送渠道？" @close="removeKey = ''"><p class="muted">“{{ removing.name }}”将从列表中移除。保存推送配置后，服务器才会删除该渠道与密钥；已发出的通知无法撤回。</p><div class="modal-actions"><button class="small-button" @click="removeKey = ''">保留渠道</button><button class="button danger-button" @click="removeChannel">确认移除</button></div></ModalShell>
    <ModalShell v-if="confirmReload" title="放弃未保存的推送修改？" @close="confirmReload = false"><p class="muted">重新加载会清除当前草稿，并读取服务器上的最新配置。</p><div class="modal-actions"><button class="small-button" @click="confirmReload = false">继续编辑</button><button class="button primary" @click="load">放弃并重新加载</button></div></ModalShell>
  </section>
</template>

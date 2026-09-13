<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { api } from '../api'
import type { Account } from '../types'
import { calendarKindName, calendarKinds, recordGames, roleKey, type CalendarEvent, type CalendarReminder, type CalendarReminderState, type RecordRole, type RecordSnapshot } from '../gameRecord'
import { formatDate, zonedEpoch } from '../time'
import ChoiceSelect from './ChoiceSelect.vue'
import DateTimeField from './DateTimeField.vue'
import ModalShell from './ModalShell.vue'
import AppIcon from './AppIcon.vue'

const props = defineProps<{ accounts: Account[]; timezone: string }>()
const emit = defineEmits<{ navigate: [view: 'notifications'] }>()
const accountID = ref(props.accounts.find(a => !a.disabled)?.id || ''), game = ref('genshin'), selectedRole = ref(''), kind = ref('all')
const roles = ref<RecordRole[]>([]), snapshot = ref<RecordSnapshot | null>(null), custom = ref<CalendarEvent[]>([])
const busy = ref(false), localBusy = ref(false), error = ref(''), localError = ref(''), now = ref(Date.now())
const editing = ref(false), removing = ref<CalendarEvent | null>(null), saving = ref(false), editError = ref('')
const title = ref(''), eventKind = ref('version'), startAt = ref(''), endAt = ref('')
const reminderState = ref<CalendarReminderState>({ reminders: [], enabled: false, channel_count: 0 })
const reminding = ref<CalendarEvent | null>(null), cancelling = ref<CalendarReminder | null>(null), clearing = ref(false)
const reminderTarget = ref('end'), leadMinutes = ref(60)
const reminderLabels: Record<string, string> = { pending: '待提醒', queued: '发送处理中', processed: '已处理', expired: '已过期', skipped: '已跳过', cancelled: '已取消' }
const reminderTargetOptions = computed(() => [{ value: 'start', label: '开始时间', disabled: !reminding.value?.start_at || Date.parse(reminding.value.start_at) <= now.value }, { value: 'end', label: '结束时间', disabled: !reminding.value?.end_at || Date.parse(reminding.value.end_at) <= now.value }])
const targetAt = computed(() => reminderTarget.value === 'start' ? reminding.value?.start_at : reminding.value?.end_at)
const remindAt = computed(() => targetAt.value ? Date.parse(targetAt.value) - leadMinutes.value * 60000 : 0)
const leadOptions = computed(() => [{ value: 0, label: '准时' }, { value: 10, label: '提前 10 分钟' }, { value: 30, label: '提前 30 分钟' }, { value: 60, label: '提前 1 小时' }, { value: 1440, label: '提前 1 天' }].map(option => ({ ...option, disabled: !targetAt.value || Date.parse(targetAt.value) - option.value * 60000 <= now.value })))
const account = computed(() => props.accounts.find(a => a.id === accountID.value))
const accountOptions = computed(() => props.accounts.map(a => ({ value: a.id, label: a.name, description: a.disabled ? '账号已停用' : a.group || undefined, disabled: a.disabled })))
const roleOptions = computed(() => roles.value.map(r => ({ value: roleKey(r), label: r.nickname || '游戏角色', description: `${r.region_name || r.region} · ${r.uid}` })))
const kindOptions = [{ value: 'all', label: '全部日程' }, ...calendarKinds]
const waitSeconds = computed(() => Math.max(0, Math.ceil((Date.parse(snapshot.value?.refresh_at || '') - now.value) / 1000)) || 0)
const events = computed(() => [...(snapshot.value?.calendar?.events || []), ...custom.value].filter(e => kind.value === 'all' || e.kind === kind.value).sort((a, b) => Date.parse(a.start_at || a.end_at || '') - Date.parse(b.start_at || b.end_at || '')))
let generation = 0, localGeneration = 0, controller: AbortController | undefined
const localController = new AbortController()
const timer = window.setInterval(() => { now.value = Date.now() }, 1000)
const activityTimer = window.setInterval(() => { if (!document.hidden && !localBusy.value && !saving.value && !editing.value && !reminding.value && !cancelling.value) void loadLocal() }, 30000)
function reset() { generation++; controller?.abort(); snapshot.value = null; roles.value = []; selectedRole.value = ''; busy.value = false; error.value = '' }
async function loadLocal(clear = false) {
  const current = ++localGeneration
  if (clear) { custom.value = []; reminderState.value = { reminders: [], enabled: false, channel_count: 0 } }
  localError.value = ''
  if (!account.value) { localBusy.value = false; return }
  localBusy.value = true
  try {
    const query = new URLSearchParams({ account_id: accountID.value, game: game.value })
    const [data, reminders] = await Promise.all([api<CalendarEvent[]>('/api/v1/calendar/custom?' + query, { signal: localController.signal }), api<CalendarReminderState>('/api/v1/calendar/reminders?' + query, { signal: localController.signal })])
    if (current === localGeneration) { custom.value = data; reminderState.value = reminders }
  } catch (e) { if (current === localGeneration) localError.value = e instanceof Error ? e.message : '本地日程加载失败' }
  finally { if (current === localGeneration) localBusy.value = false }
}
function chooseAccount(value: string) { if (value !== accountID.value) { reset(); accountID.value = value; void loadLocal(true) } }
function chooseGame(value: string) { if (value !== game.value) { reset(); game.value = value; void loadLocal(true) } }
function chooseRole(value: string) { if (value !== selectedRole.value) { generation++; controller?.abort(); busy.value = false; error.value = ''; snapshot.value = null; selectedRole.value = value } }
async function read() {
  if (busy.value || !account.value || account.value.disabled || waitSeconds.value) return
  const current = ++generation
  controller?.abort(); controller = new AbortController(); busy.value = true; error.value = ''
  const query = new URLSearchParams({ account_id: accountID.value, game: game.value })
  const role = roles.value.find(r => roleKey(r) === selectedRole.value)
  if (role) { query.set('role_id', role.uid); query.set('server', role.region) }
  try {
    const data = await api<RecordSnapshot>('/api/v1/game-record/calendar?' + query, { signal: controller.signal })
    if (current !== generation) return
    snapshot.value = data; roles.value = data.roles || []; selectedRole.value = data.role ? roleKey(data.role) : ''; now.value = Date.now()
  } catch (e) { if (current === generation) error.value = e instanceof Error ? e.message : '日历读取失败' }
  finally { if (current === generation) busy.value = false }
}
function create() { title.value = ''; eventKind.value = 'version'; startAt.value = ''; endAt.value = ''; editError.value = ''; editing.value = true }
function iso(value: string) { return value ? new Date(zonedEpoch(value, props.timezone) * 1000).toISOString() : null }
async function save() {
  if (saving.value) return
  saving.value = true; editError.value = ''
  try {
    await api('/api/v1/calendar/custom', { method: 'POST', signal: localController.signal, body: JSON.stringify({ account_id: accountID.value, game: game.value, title: title.value.trim(), kind: eventKind.value, start_at: iso(startAt.value), end_at: iso(endAt.value) }) })
    editing.value = false; await loadLocal()
  } catch (e) { editError.value = e instanceof Error ? e.message : '日程保存失败' }
  finally { saving.value = false }
}
async function remove() {
  if (!removing.value || saving.value) return
  saving.value = true; editError.value = ''
  try { await api('/api/v1/calendar/custom', { method: 'DELETE', signal: localController.signal, body: JSON.stringify({ id: removing.value.id }) }); removing.value = null; await loadLocal() }
  catch (e) { editError.value = e instanceof Error ? e.message : '删除失败' }
  finally { saving.value = false }
}
function eventState(event: CalendarEvent) {
  if (event.end_at && Date.parse(event.end_at) < now.value) return '已结束'
  if (event.start_at && Date.parse(event.start_at) > now.value) return '未开始'
  return event.start_at ? event.end_at ? '进行中' : '开始时间已到' : '开始时间未提供'
}
function canRemind(event: CalendarEvent) { return [event.start_at, event.end_at].some(at => at && Date.parse(at) > now.value) }
function openReminder(event: CalendarEvent) {
  reminding.value = event; editError.value = ''; reminderTarget.value = event.end_at && Date.parse(event.end_at) > now.value ? 'end' : 'start'
  leadMinutes.value = targetAt.value && Date.parse(targetAt.value) - 3600000 > now.value ? 60 : 0
}
async function saveReminder() {
  if (!reminding.value || saving.value || remindAt.value <= now.value) return
  saving.value = true; editError.value = ''
  try {
    await api('/api/v1/calendar/reminders', { method: 'POST', signal: localController.signal, body: JSON.stringify({ account_id: accountID.value, game: game.value, event_id: reminding.value.id, target: reminderTarget.value, lead_minutes: leadMinutes.value }) })
    reminding.value = null; await loadLocal()
  } catch (e) { editError.value = e instanceof Error ? e.message : '提醒保存失败' }
  finally { saving.value = false }
}
async function cancelReminder() {
  if (!cancelling.value || saving.value) return
  saving.value = true; editError.value = ''
  try { await api('/api/v1/calendar/reminders', { method: 'DELETE', signal: localController.signal, body: JSON.stringify({ id: cancelling.value.id }) }); cancelling.value = null; await loadLocal() }
  catch (e) { editError.value = e instanceof Error ? e.message : '取消失败' }
  finally { saving.value = false }
}
async function clearHistory() {
  saving.value = true; editError.value = ''
  try { await api('/api/v1/calendar/reminders/history', { method: 'DELETE', signal: localController.signal, body: JSON.stringify({ account_id: accountID.value, game: game.value }) }); clearing.value = false; await loadLocal() }
  catch (e) { editError.value = e instanceof Error ? e.message : '清理失败' }
  finally { saving.value = false }
}
function configurePush() { reminding.value = null; emit('navigate', 'notifications') }
watch(() => account.value?.disabled, () => { if (!account.value || account.value.disabled) { reset(); void loadLocal(true) } })
void loadLocal()
onUnmounted(() => { generation++; localGeneration++; controller?.abort(); localController.abort(); window.clearInterval(timer); window.clearInterval(activityTimer) })
</script>

<template>
  <section class="calendar-workspace">
    <article class="panel">
      <div class="panel-title"><div><p class="eyebrow">游戏日程</p><h2>活动日历</h2></div><AppIcon name="clock" /></div>
      <p class="muted">查看已公布的卡池、活动与挑战时间；官方未提供的版本更新时间可手动记录，不推测未来日期。</p>
      <form class="calendar-controls" @submit.prevent="read"><label>米游社账号<ChoiceSelect :model-value="accountID" :options="accountOptions" label="日历账号" @update:model-value="chooseAccount" /></label><label>游戏<ChoiceSelect :model-value="game" :options="recordGames" label="日历游戏" @update:model-value="chooseGame" /></label><label v-if="roles.length">游戏角色<ChoiceSelect :model-value="selectedRole" :options="roleOptions" label="日历角色" @update:model-value="chooseRole" /></label><button class="button primary" :disabled="busy || !account || account.disabled || waitSeconds > 0"><AppIcon name="refresh" :size="16" />{{ busy ? '正在读取…' : waitSeconds ? `${Math.ceil(waitSeconds / 60)} 分钟后可更新` : '读取官方活动' }}</button></form>
      <p class="muted calendar-hint">官方活动缓存 30 分钟，与便笺共用账号冷却。打开页面只加载本站日程，不查询米游社。</p>
      <p v-if="game === 'zzz'" class="notice">绝区零官方活动接口暂未接入，可添加自定义日程。</p>
      <p v-if="error" class="error-banner" role="alert">{{ error }}</p>
    </article>
    <div class="calendar-toolbar"><ChoiceSelect v-model="kind" :options="kindOptions" label="日程类型筛选" /><button class="small-button" :disabled="!account || account.disabled" @click="create"><AppIcon name="plus" :size="15" />添加自定义日程</button></div>
    <p v-if="snapshot?.message" class="notice" role="status">{{ snapshot.message }}<span v-if="snapshot.refresh_at"> · 可重试时间：{{ formatDate(snapshot.refresh_at, timezone) }}</span></p>
    <p v-if="snapshot?.calendar && snapshot.observed_at" class="muted calendar-hint">官方数据记录于 {{ formatDate(snapshot.observed_at, timezone) }}{{ snapshot.stale ? ' · 上次快照，非当前状态' : snapshot.cached ? ' · 缓存' : '' }}<span v-if="snapshot.calendar.skipped"> · {{ snapshot.calendar.skipped }} 项参数不完整，未展示</span></p>
    <p v-if="localError" class="error-banner" role="alert">{{ localError }} <button class="text-button" @click="loadLocal()">重新加载本站日程</button></p>
    <div v-if="!events.length" class="panel empty">{{ localBusy || busy ? '正在加载…' : snapshot?.calendar ? '没有符合筛选的日程。' : '暂无已加载日程，可读取官方活动或添加自定义日程。' }}</div>
    <div class="calendar-events"><article v-for="event in events" :key="event.id" class="panel calendar-event"><div class="calendar-event-top"><span class="pill soft">{{ calendarKindName(event.kind) }}</span><span>{{ event.source === 'manual' ? '自行录入' : '米游社活动数据' }} · {{ eventState(event) }}</span></div><h3>{{ event.title }}</h3><dl><div><dt>开始</dt><dd>{{ event.start_at ? formatDate(event.start_at, timezone) : '未提供' }}</dd></div><div><dt>结束</dt><dd>{{ event.end_at ? formatDate(event.end_at, timezone) : '未提供' }}</dd></div></dl><div class="calendar-event-actions"><small v-if="event.finished !== null" class="muted">{{ event.finished ? '该角色已完成' : '该角色尚未完成' }}</small><button class="small-button" :disabled="!canRemind(event) || account?.disabled" @click="openReminder(event)"><AppIcon name="bell" :size="14" />设置提醒</button><button v-if="event.source === 'manual'" class="text-button" @click="editError = ''; removing = event"><AppIcon name="trash" :size="14" />删除日程</button></div></article></div>
    <article class="panel calendar-reminders"><div class="panel-title"><h2>日历提醒</h2><button class="text-button" @click="loadLocal()"><AppIcon name="refresh" :size="15" />刷新记录</button></div><p class="muted">按保存的日程时间发送一次；日期有变更时需自行取消并重新安排。服务每 30 秒检查本站提醒，不额外查询游戏数据。</p><p v-if="!reminderState.enabled || !reminderState.channel_count" class="notice">尚未开启日历推送或未配置接收渠道，到期提醒将跳过。<button class="text-button" @click="configurePush">去消息推送配置</button></p><p v-if="!reminderState.reminders.length" class="empty">尚未安排提醒，点击日程卡片上的「设置提醒」。</p><div v-for="reminder in reminderState.reminders" :key="reminder.id" class="calendar-reminder"><div><strong>{{ reminder.title }}</strong><small>{{ reminder.target === 'start' ? '开始' : '结束' }}提醒 · {{ formatDate(reminder.remind_at, timezone) }}</small><small v-if="reminder.detail">{{ reminder.detail }}</small></div><span class="pill soft">{{ reminderLabels[reminder.status] || '待确认' }}</span><button v-if="['pending', 'queued'].includes(reminder.status)" class="text-button" @click="editError = ''; cancelling = reminder">取消提醒</button></div><button v-if="reminderState.reminders.some(r => !['pending', 'queued'].includes(r.status))" class="text-button" @click="editError = ''; clearing = true">清理已结束的提醒记录</button></article>
    <ModalShell v-if="editing" title="添加自定义日程" :busy="saving" @close="editing = false"><form class="calendar-editor" @submit.prevent="save"><p class="muted">{{ account?.name }} · {{ recordGames.find(g => g.value === game)?.label }}。时间按 {{ timezone }} 录入；请以官方公告为准。</p><label>日程名称<input v-model="title" required maxlength="160" placeholder="例如：已公告的版本更新时间" /></label><label>日程类型<ChoiceSelect v-model="eventKind" :options="calendarKinds" label="日程类型" /></label><label>开始时间<DateTimeField v-model="startAt" label="日程开始时间" :timezone="timezone" /></label><label>结束时间（可不填）<DateTimeField v-model="endAt" label="日程结束时间" :timezone="timezone" /></label><button v-if="endAt" type="button" class="text-button" @click="endAt = ''">清除结束时间</button><p v-if="editError" class="error-banner" role="alert">{{ editError }}</p><button class="button primary" :disabled="saving || !title.trim() || (!startAt && !endAt)">{{ saving ? '保存中…' : '保存日程' }}</button></form></ModalShell>
    <ModalShell v-if="removing" title="删除自定义日程" :busy="saving" @close="removing = null"><p>删除「{{ removing.title }}」并取消尚未发送的提醒？已提交的通知无法撤回。</p><p v-if="editError" class="error-banner" role="alert">{{ editError }}</p><div class="modal-actions"><button class="button primary" :disabled="saving" @click="remove">确认删除</button><button class="small-button" :disabled="saving" @click="removing = null">保留</button></div></ModalShell>
    <ModalShell v-if="reminding" title="设置日历提醒" :busy="saving" @close="reminding = null"><form class="calendar-editor" @submit.prevent="saveReminder"><p>{{ reminding.title }}</p><label>提醒节点<ChoiceSelect v-model="reminderTarget" :options="reminderTargetOptions" label="提醒节点" /></label><label>提前时间<ChoiceSelect v-model="leadMinutes" :options="leadOptions" label="提前提醒时间" /></label><p class="muted">{{ remindAt ? '计划提醒：' + formatDate(new Date(remindAt).toISOString(), timezone) : '请选择有效时间' }} · {{ timezone }}。后台时间已过或推送未开启时不会补发。</p><p v-if="!reminderState.enabled || !reminderState.channel_count" class="notice">当前不能自动发送，请另行开启日历推送并配置渠道。保存提醒不会替你开启推送。<button type="button" class="text-button" @click="configurePush">去消息推送配置</button></p><p v-if="editError" class="error-banner" role="alert">{{ editError }}</p><button class="button primary" :disabled="saving || remindAt <= now">{{ saving ? '保存中…' : '保存提醒' }}</button></form></ModalShell>
    <ModalShell v-if="cancelling" title="取消日历提醒" :busy="saving" @close="cancelling = null"><p>取消「{{ cancelling.title }}」的提醒？已经提交给推送服务的通知不能撤回。</p><p v-if="editError" class="error-banner" role="alert">{{ editError }}</p><div class="modal-actions"><button class="button primary" :disabled="saving" @click="cancelReminder">确认取消提醒</button><button class="small-button" :disabled="saving" @click="cancelling = null">保留</button></div></ModalShell>
    <ModalShell v-if="clearing" title="清理提醒记录" :busy="saving" @close="clearing = false"><p>清理当前游戏已取消、跳过、过期，以及目标时间已过的处理记录。待发送的提醒会保留。</p><p v-if="editError" class="error-banner" role="alert">{{ editError }}</p><div class="modal-actions"><button class="button primary" :disabled="saving" @click="clearHistory">确认清理记录</button><button class="small-button" :disabled="saving" @click="clearing = false">保留</button></div></ModalShell>
  </section>
</template>

<style scoped>
.calendar-workspace{display:grid;gap:18px}.calendar-controls{display:flex;flex-wrap:wrap;align-items:end;gap:14px;margin-top:20px}.calendar-controls>label{display:grid;gap:8px;min-width:170px;flex:1;font-size:12px}.calendar-controls>.button{min-height:43px;font-size:12px}.calendar-hint{font-size:11px;line-height:1.8;margin:15px 0 0}.calendar-toolbar{display:flex;align-items:center;flex-wrap:wrap;gap:12px;justify-content:space-between}.calendar-toolbar>.choice-trigger{width:180px}.calendar-events{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px}.calendar-event{min-width:0}.calendar-event-top{display:flex;align-items:center;gap:10px;flex-wrap:wrap}.calendar-event-top>span:last-child{font-size:10px;color:#858775}.calendar-event h3{font-size:16px;line-height:1.7;margin:18px 0;overflow-wrap:anywhere}.calendar-event dl{display:grid;gap:10px;font-size:12px}.calendar-event dl>div{display:flex;gap:16px}.calendar-event dt{flex-shrink:0;color:#8a8a79}.calendar-event dd{margin:0;overflow-wrap:anywhere}.calendar-event-actions{display:flex;gap:10px;justify-content:space-between;align-items:center;flex-wrap:wrap;margin-top:16px}.calendar-editor{display:grid;gap:16px}.calendar-editor>label{display:grid;gap:8px;font-size:12px}@media(max-width:700px){.calendar-events{grid-template-columns:1fr}.calendar-controls>label{min-width:100%}.calendar-controls>.button{width:100%}.calendar-toolbar>.choice-trigger{width:150px}}
</style>

<style scoped>
.calendar-reminders>.muted{font-size:12px}.calendar-reminder{display:flex;align-items:center;flex-wrap:wrap;gap:12px;padding:16px 0;border-top:1px solid #ebe8dd}.calendar-reminder>div{flex:1;min-width:150px;display:grid;gap:7px}.calendar-reminder strong{font-size:13px;font-weight:500;overflow-wrap:anywhere}.calendar-reminder small{font-size:11px;color:#878978;line-height:1.7}.calendar-reminders>.text-button{margin-top:14px}
</style>

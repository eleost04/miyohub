<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { api } from '../api'
import type { Account } from '../types'
import { calendarKindName, calendarKinds, recordGames, roleKey, type CalendarEvent, type RecordRole, type RecordSnapshot } from '../gameRecord'
import { formatDate, zonedEpoch } from '../time'
import ChoiceSelect from './ChoiceSelect.vue'
import DateTimeField from './DateTimeField.vue'
import ModalShell from './ModalShell.vue'
import AppIcon from './AppIcon.vue'

const props = defineProps<{ accounts: Account[]; timezone: string }>()
const accountID = ref(props.accounts.find(a => !a.disabled)?.id || ''), game = ref('genshin'), selectedRole = ref(''), kind = ref('all')
const roles = ref<RecordRole[]>([]), snapshot = ref<RecordSnapshot | null>(null), custom = ref<CalendarEvent[]>([])
const busy = ref(false), localBusy = ref(false), error = ref(''), localError = ref(''), now = ref(Date.now())
const editing = ref(false), removing = ref<CalendarEvent | null>(null), saving = ref(false), editError = ref('')
const title = ref(''), eventKind = ref('version'), startAt = ref(''), endAt = ref('')
const account = computed(() => props.accounts.find(a => a.id === accountID.value))
const accountOptions = computed(() => props.accounts.map(a => ({ value: a.id, label: a.name, description: a.disabled ? '账号已停用' : a.group || undefined, disabled: a.disabled })))
const roleOptions = computed(() => roles.value.map(r => ({ value: roleKey(r), label: r.nickname || '游戏角色', description: `${r.region_name || r.region} · ${r.uid}` })))
const kindOptions = [{ value: 'all', label: '全部日程' }, ...calendarKinds]
const waitSeconds = computed(() => Math.max(0, Math.ceil((Date.parse(snapshot.value?.refresh_at || '') - now.value) / 1000)) || 0)
const events = computed(() => [...(snapshot.value?.calendar?.events || []), ...custom.value].filter(e => kind.value === 'all' || e.kind === kind.value).sort((a, b) => Date.parse(a.start_at || a.end_at || '') - Date.parse(b.start_at || b.end_at || '')))
let generation = 0, localGeneration = 0, controller: AbortController | undefined
const localController = new AbortController()
const timer = window.setInterval(() => { now.value = Date.now() }, 1000)
function reset() { generation++; controller?.abort(); snapshot.value = null; roles.value = []; selectedRole.value = ''; busy.value = false; error.value = '' }
async function loadLocal() {
  const current = ++localGeneration
  custom.value = []; localError.value = ''
  if (!account.value) return
  localBusy.value = true
  try {
    const data = await api<CalendarEvent[]>('/api/v1/calendar/custom?' + new URLSearchParams({ account_id: accountID.value, game: game.value }), { signal: localController.signal })
    if (current === localGeneration) custom.value = data
  } catch (e) { if (current === localGeneration) localError.value = e instanceof Error ? e.message : '本地日程加载失败' }
  finally { if (current === localGeneration) localBusy.value = false }
}
function chooseAccount(value: string) { if (value !== accountID.value) { reset(); accountID.value = value; void loadLocal() } }
function chooseGame(value: string) { if (value !== game.value) { reset(); game.value = value; void loadLocal() } }
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
watch(() => account.value?.disabled, () => { if (!account.value || account.value.disabled) reset() })
void loadLocal()
onUnmounted(() => { generation++; localGeneration++; controller?.abort(); localController.abort(); window.clearInterval(timer) })
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
    <p v-if="localError" class="error-banner" role="alert">{{ localError }} <button class="text-button" @click="loadLocal">重新加载本站日程</button></p>
    <div v-if="!events.length" class="panel empty">{{ localBusy || busy ? '正在加载…' : snapshot?.calendar ? '没有符合筛选的日程。' : '暂无已加载日程，可读取官方活动或添加自定义日程。' }}</div>
    <div class="calendar-events"><article v-for="event in events" :key="event.id" class="panel calendar-event"><div class="calendar-event-top"><span class="pill soft">{{ calendarKindName(event.kind) }}</span><span>{{ event.source === 'manual' ? '自行录入' : '米游社活动数据' }} · {{ eventState(event) }}</span></div><h3>{{ event.title }}</h3><dl><div><dt>开始</dt><dd>{{ event.start_at ? formatDate(event.start_at, timezone) : '未提供' }}</dd></div><div><dt>结束</dt><dd>{{ event.end_at ? formatDate(event.end_at, timezone) : '未提供' }}</dd></div></dl><div class="calendar-event-actions"><small v-if="event.finished !== null" class="muted">{{ event.finished ? '该角色已完成' : '该角色尚未完成' }}</small><button v-if="event.source === 'manual'" class="text-button" @click="editError = ''; removing = event"><AppIcon name="trash" :size="14" />删除日程</button></div></article></div>
    <ModalShell v-if="editing" title="添加自定义日程" :busy="saving" @close="editing = false"><form class="calendar-editor" @submit.prevent="save"><p class="muted">{{ account?.name }} · {{ recordGames.find(g => g.value === game)?.label }}。时间按 {{ timezone }} 录入；请以官方公告为准。</p><label>日程名称<input v-model="title" required maxlength="160" placeholder="例如：已公告的版本更新时间" /></label><label>日程类型<ChoiceSelect v-model="eventKind" :options="calendarKinds" label="日程类型" /></label><label>开始时间<DateTimeField v-model="startAt" label="日程开始时间" :timezone="timezone" /></label><label>结束时间（可不填）<DateTimeField v-model="endAt" label="日程结束时间" :timezone="timezone" /></label><button v-if="endAt" type="button" class="text-button" @click="endAt = ''">清除结束时间</button><p v-if="editError" class="error-banner" role="alert">{{ editError }}</p><button class="button primary" :disabled="saving || !title.trim() || (!startAt && !endAt)">{{ saving ? '保存中…' : '保存日程' }}</button></form></ModalShell>
    <ModalShell v-if="removing" title="删除自定义日程" :busy="saving" @close="removing = null"><p>删除「{{ removing.title }}」？</p><p v-if="editError" class="error-banner" role="alert">{{ editError }}</p><div class="modal-actions"><button class="button primary" :disabled="saving" @click="remove">确认删除</button><button class="small-button" :disabled="saving" @click="removing = null">保留</button></div></ModalShell>
  </section>
</template>

<style scoped>
.calendar-workspace{display:grid;gap:18px}.calendar-controls{display:flex;flex-wrap:wrap;align-items:end;gap:14px;margin-top:20px}.calendar-controls>label{display:grid;gap:8px;min-width:170px;flex:1;font-size:12px}.calendar-controls>.button{min-height:43px;font-size:12px}.calendar-hint{font-size:11px;line-height:1.8;margin:15px 0 0}.calendar-toolbar{display:flex;align-items:center;flex-wrap:wrap;gap:12px;justify-content:space-between}.calendar-toolbar>.choice-trigger{width:180px}.calendar-events{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px}.calendar-event{min-width:0}.calendar-event-top{display:flex;align-items:center;gap:10px;flex-wrap:wrap}.calendar-event-top>span:last-child{font-size:10px;color:#858775}.calendar-event h3{font-size:16px;line-height:1.7;margin:18px 0;overflow-wrap:anywhere}.calendar-event dl{display:grid;gap:10px;font-size:12px}.calendar-event dl>div{display:flex;gap:16px}.calendar-event dt{flex-shrink:0;color:#8a8a79}.calendar-event dd{margin:0;overflow-wrap:anywhere}.calendar-event-actions{display:flex;gap:10px;justify-content:space-between;align-items:center;flex-wrap:wrap;margin-top:16px}.calendar-editor{display:grid;gap:16px}.calendar-editor>label{display:grid;gap:8px;font-size:12px}@media(max-width:700px){.calendar-events{grid-template-columns:1fr}.calendar-controls>label{min-width:100%}.calendar-controls>.button{width:100%}.calendar-toolbar>.choice-trigger{width:150px}}
</style>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { api } from '../api'
import type { Account } from '../types'
import { recordGames, roleKey, type RecordMetric, type RecordRole, type RecordSnapshot } from '../gameRecord'
import { formatDate } from '../time'
import AppIcon from './AppIcon.vue'
import ChoiceSelect from './ChoiceSelect.vue'

const props = defineProps<{ accounts: Account[]; timezone: string }>()
const accountID = ref(props.accounts.find(a => !a.disabled)?.id || ''), game = ref('genshin'), selectedRole = ref('')
const roles = ref<RecordRole[]>([]), snapshot = ref<RecordSnapshot | null>(null), busy = ref(false), error = ref(''), now = ref(Date.now())
const accountOptions = computed(() => props.accounts.map(a => ({ value: a.id, label: a.name, description: a.disabled ? '账号已停用' : a.group || undefined, disabled: a.disabled })))
const roleOptions = computed(() => roles.value.map(r => ({ value: roleKey(r), label: r.nickname || '游戏角色', description: `${r.region_name || r.region} · ${r.uid}` })))
const waitSeconds = computed(() => Math.max(0, Math.ceil((Date.parse(snapshot.value?.refresh_at || '') - now.value) / 1000)) || 0)
const account = computed(() => props.accounts.find(a => a.id === accountID.value))
let generation = 0, controller: AbortController | undefined
const timer = window.setInterval(() => { now.value = Date.now() }, 1000)
function reset(clearRoles = true) {
  generation++; controller?.abort(); busy.value = false; error.value = ''; snapshot.value = null
  if (clearRoles) { roles.value = []; selectedRole.value = '' }
}
function chooseAccount(value: string) { if (value !== accountID.value) { reset(); accountID.value = value } }
function chooseGame(value: string) { if (value !== game.value) { reset(); game.value = value } }
function chooseRole(value: string) { if (value !== selectedRole.value) { reset(false); selectedRole.value = value } }
async function read() {
  if (busy.value || !account.value || account.value.disabled || waitSeconds.value) return
  const current = ++generation
  controller?.abort(); controller = new AbortController(); busy.value = true; error.value = ''
  const query = new URLSearchParams({ account_id: accountID.value, game: game.value })
  const role = roles.value.find(r => roleKey(r) === selectedRole.value)
  if (role) { query.set('role_id', role.uid); query.set('server', role.region) }
  try {
    const data = await api<RecordSnapshot>('/api/v1/game-record/note?' + query, { signal: controller.signal })
    if (current !== generation) return
    snapshot.value = data; roles.value = data.roles || []; selectedRole.value = data.role ? roleKey(data.role) : ''
    now.value = Date.now()
  } catch (e) { if (current === generation) error.value = e instanceof Error ? e.message : '读取失败' }
  finally { if (current === generation) busy.value = false }
}
function recovery(metric: RecordMetric) {
  if (metric.current != null && metric.max != null && metric.current >= metric.max) return '记录时已达上限'
  if (!metric.recovery_seconds || !snapshot.value?.observed_at) return ''
  const at = Date.parse(snapshot.value.observed_at) + metric.recovery_seconds * 1000
  if (at <= now.value) return '预计恢复时间已到，请更新确认'
  return '预计恢复至上限：' + formatDate(new Date(at).toISOString(), props.timezone)
}
watch(() => account.value?.disabled, () => { if (!account.value || account.value.disabled) reset() })
onUnmounted(() => { generation++; controller?.abort(); window.clearInterval(timer) })
</script>

<template>
  <section class="notes-workspace">
    <article class="panel">
      <div class="panel-title"><div><p class="eyebrow">游戏数据</p><h2>实时便笺</h2></div><AppIcon name="activity" /></div>
      <p class="muted">按需读取官方便笺，查看体力、每日进度与派遣。打开页面不会查询上游，也不执行游戏内操作。</p>
      <form class="note-controls" @submit.prevent="read">
        <label>米游社账号<ChoiceSelect :model-value="accountID" :options="accountOptions" label="便笺账号" searchable @update:model-value="chooseAccount" /></label>
        <label>游戏<ChoiceSelect :model-value="game" :options="recordGames" label="便笺游戏" @update:model-value="chooseGame" /></label>
        <label v-if="roles.length">游戏角色<ChoiceSelect :model-value="selectedRole" :options="roleOptions" label="便笺角色" @update:model-value="chooseRole" /></label>
        <button class="button primary" :disabled="busy || !account || account.disabled || waitSeconds > 0"><AppIcon name="refresh" :size="16" />{{ busy ? '正在读取…' : waitSeconds ? `${Math.ceil(waitSeconds / 60)} 分钟后可更新` : '读取便笺' }}</button>
      </form>
      <p class="muted note-hint">便笺缓存 3 分钟，角色缓存 15 分钟。缺失数据标为「未提供」；安全验证需在官方客户端完成，本站不自动重试验证。</p>
      <p v-if="!accounts.length" class="empty">请先在任务总览绑定米游社账号。</p>
      <p v-if="error" class="error-banner" role="alert">{{ error }}</p>
    </article>
    <p v-if="snapshot?.message" class="notice" role="status">{{ snapshot.message }}<span v-if="snapshot.refresh_at"> · 可重试时间：{{ formatDate(snapshot.refresh_at, timezone) }}</span></p>
    <div v-if="snapshot?.note" class="note-result">
      <div class="note-observed"><strong>{{ snapshot.role?.nickname || '游戏角色' }}</strong><span v-if="snapshot.observed_at">记录于 {{ formatDate(snapshot.observed_at, timezone) }}{{ snapshot.stale ? ' · 上次快照，非实时数据' : snapshot.cached ? ' · 缓存' : '' }}</span></div>
      <div class="note-metrics"><article v-for="metric in snapshot.note.metrics" :key="metric.key" class="panel note-metric" :class="{ 'note-energy': metric.key === 'energy' }"><span>{{ metric.label }}</span><p><strong>{{ metric.current ?? '未提供' }}</strong><small v-if="metric.max != null"> / {{ metric.max }}</small></p><div v-if="metric.current != null && metric.max != null" class="note-meter" :aria-label="`${metric.label} ${metric.current} / ${metric.max}`"><span :style="{ width: Math.min(100, metric.current / metric.max * 100) + '%' }"></span></div><small v-if="recovery(metric)" class="muted">{{ recovery(metric) }}</small></article></div>
      <article v-if="snapshot.note.flags.length" class="panel note-flags"><div v-for="flag in snapshot.note.flags" :key="flag.key"><span>{{ flag.label }}</span><strong>{{ flag.value === null ? '未提供' : flag.value ? '是' : '否' }}</strong></div></article>
    </div>
    <div v-else-if="!busy && !snapshot" class="panel empty">选择账号与游戏后读取；后台不会定时刷新便笺。</div>
  </section>
</template>

<style scoped>
.notes-workspace{display:grid;gap:18px}.note-controls{display:flex;flex-wrap:wrap;gap:14px;align-items:end;margin-top:20px}.note-controls>label{display:grid;gap:8px;flex:1;min-width:170px;font-size:12px}.note-controls>.button{min-height:43px;font-size:12px}.note-hint{font-size:11px;margin:16px 0 0}.note-result{display:grid;gap:16px}.note-observed{display:flex;align-items:baseline;flex-wrap:wrap;gap:8px 16px}.note-observed>span{font-size:11px;color:#808272}.note-metrics{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:15px}.note-metric{min-width:0;padding:22px;display:grid;align-content:start;gap:12px}.note-metric>span{font-size:12px;color:#7f8375}.note-metric>p{margin:0}.note-metric strong{font-size:28px;font-weight:550}.note-metric small{font-size:11px;line-height:1.8}.note-energy{background:#eef1e8}.note-meter{height:5px;background:#e1e5d9;border-radius:8px;overflow:hidden}.note-meter>span{display:block;height:100%;background:#93a57e;border-radius:8px}.note-flags{display:grid;gap:15px;font-size:12px}.note-flags>div{display:flex;justify-content:space-between;gap:16px}.note-flags strong{white-space:nowrap;font-weight:500}@media(max-width:700px){.note-metrics{grid-template-columns:repeat(2,minmax(0,1fr))}.note-controls>label{min-width:100%}.note-controls>.button{width:100%}.note-metric{padding:17px}.note-metric strong{font-size:23px}}@media(max-width:360px){.note-metrics{grid-template-columns:1fr}}
</style>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { api } from '../api'
import type { Account } from '../types'
import { accountTasks, hasTasks } from '../taskSettings'
import AppIcon from './AppIcon.vue'
import ChoiceSelect from './ChoiceSelect.vue'
import ModalShell from './ModalShell.vue'
import TimeField from './TimeField.vue'

const props = defineProps<{ accounts: Account[]; initialGroup: string | null; enabled: boolean; actionBusy: boolean; timezone: string }>()
const emit = defineEmits<{ close: []; changed: []; run: [ids: string[]] }>()
const selected = ref<string[]>([]), filter = ref(props.initialGroup === null ? '*' : 'group:' + props.initialGroup)
const action = ref('group'), group = ref(''), time = ref('09:00'), zone = ref(props.timezone)
const busy = ref(false), error = ref('')
const revisions = Object.fromEntries(props.accounts.map(a => [a.id, accountTasks(a).revision]))
const groups = computed(() => [...new Set(props.accounts.map(a => a.group || '').filter(Boolean))].sort())
const filters = computed(() => [{ value: '*', label: '全部账号' }, { value: 'group:', label: '未分组' }, ...groups.value.map(value => ({ value: 'group:' + value, label: value }))])
const visible = computed(() => props.accounts.filter(a => filter.value === '*' || 'group:' + (a.group || '') === filter.value))
const actions = [
  { value: 'group', label: '设置分组' }, { value: 'schedule', label: '设置个人签到时间' }, { value: 'default', label: '使用站点默认时间' },
  { value: 'auto_on', label: '开启自动签到' }, { value: 'auto_off', label: '关闭自动签到' },
  { value: 'run', label: '执行所选账号' }, { value: 'pause', label: '停用所选账号' }, { value: 'resume', label: '启用所选账号' },
]
const zones = computed(() => [...new Set([props.timezone, 'Asia/Shanghai', 'Asia/Tokyo', 'Asia/Singapore', 'Europe/London', 'America/New_York', 'UTC'])].map(value => ({ value, label: value })))
const runnable = computed(() => selected.value.length && props.enabled && !props.actionBusy && selected.value.every(id => props.accounts.some(a => a.id === id && !a.disabled && hasTasks(a))))
function selectVisible() { selected.value = visible.value.slice(0, 50).map(a => a.id) }
async function apply() {
  if (!selected.value.length || selected.value.length > 50 || busy.value) return
  error.value = ''
  if (action.value === 'run') {
    if (!runnable.value) { error.value = '所选账号须已启用并配置签到项目，且站点未暂停'; return }
    emit('run', [...selected.value]); emit('close'); return
  }
  const input: Record<string, unknown> = { account_ids: selected.value, revisions }
  if (action.value === 'group') input.group = group.value.trim()
  if (action.value === 'schedule') input.schedule = { time: time.value, timezone: zone.value }
  if (action.value === 'default') input.follow_default = true
  if (action.value.startsWith('auto_')) input.automatic = action.value === 'auto_on'
  if (['pause', 'resume'].includes(action.value)) input.disabled = action.value === 'pause'
  busy.value = true
  try { await api('/api/v1/accounts/batch', { method: 'PUT', body: JSON.stringify(input) }); emit('changed'); emit('close') }
  catch (e) { error.value = e instanceof Error ? e.message : '批量操作失败' }
  finally { busy.value = false }
}
</script>

<template>
  <ModalShell title="分组与批量操作" :busy="busy" wide @close="emit('close')">
    <form class="batch-form" @submit.prevent="apply">
      <p class="muted">每次最多 50 个账号。分组用于筛选，签到时间仍分别保存在各账号，可再单独修改。</p>
      <ChoiceSelect v-model="filter" label="筛选账号分组" :options="filters" />
      <div class="batch-toolbar"><span>已选 {{ selected.length }} 个</span><button type="button" class="text-button" @click="selectVisible">选择当前分组</button><button type="button" class="text-button" @click="selected = []">清空</button></div>
      <div class="batch-accounts" role="group" aria-label="批量账号选择">
        <label v-for="account in visible" :key="account.id" class="batch-account"><input v-model="selected" type="checkbox" :value="account.id" :disabled="!selected.includes(account.id) && selected.length >= 50" /><span><strong>{{ account.name }}</strong><small>{{ account.group || '未分组' }} · {{ account.disabled ? '已停用' : accountTasks(account).automatic ? '自动签到' : '手动签到' }}</small></span></label>
        <p v-if="!visible.length" class="muted">这个分组暂无账号。</p>
      </div>
      <label>批量操作<ChoiceSelect v-model="action" label="批量操作" :options="actions" /></label>
      <label v-if="action === 'group'">分组名称<input v-model="group" maxlength="32" placeholder="输入名称；留空移出分组" /><small class="muted">同名即同一分组；不改变账号归属。</small></label>
      <div v-if="action === 'schedule'" class="form-grid"><label>每日时间<TimeField v-model="time" label="批量签到时间" /></label><label>时区<ChoiceSelect v-model="zone" label="批量签到时区" :options="zones" /></label></div>
      <p v-if="['schedule', 'default'].includes(action)" class="muted">仅更新下一次调度，不立即执行，也不改变已选游戏或自动签到开关。</p>
      <p v-if="action === 'pause'" class="error-text">将停止所选账号的当前任务与兑换。预约不会被删除，后续状态以执行记录为准。</p>
      <p v-if="action === 'run'" class="muted">按各账号当前已选项目执行一次，沿用现有排队与间隔，不增加并行请求。</p>
      <p v-if="error" class="error-banner" role="alert">{{ error }}</p>
      <div class="modal-actions"><button class="button primary" :disabled="busy || !selected.length || (action === 'run' && !runnable)">{{ busy ? '正在应用…' : action === 'run' ? '执行所选账号' : '应用到所选账号' }}<AppIcon name="check" /></button></div>
    </form>
  </ModalShell>
</template>

<style scoped>
.batch-form{display:grid;gap:16px}.batch-toolbar{display:flex;gap:12px;align-items:center;flex-wrap:wrap;font-size:12px;color:#737865}.batch-toolbar>span{margin-right:auto}.batch-accounts{max-height:240px;overflow:auto;display:grid;gap:8px;border-block:1px solid #e7e2d6;padding:10px 0}.batch-account{display:flex;gap:12px;align-items:center;padding:10px;border-radius:10px;background:#f6f4ec}.batch-account>span{min-width:0;display:grid;gap:4px}.batch-account strong{overflow-wrap:anywhere}.batch-account small{color:#828570;font-size:11px}.batch-form>label{display:grid;gap:8px}.batch-form .modal-actions{margin-top:0}
</style>

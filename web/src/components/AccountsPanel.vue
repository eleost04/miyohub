<script setup lang="ts">
import { computed, defineAsyncComponent, ref, watch } from 'vue'
import { api } from '../api'
import { formatDate } from '../time'
import type { Account, TaskProgress } from '../types'
import AppIcon from './AppIcon.vue'
import ModalShell from './ModalShell.vue'
import FloatingSave from './FloatingSave.vue'
import PageLoading from './PageLoading.vue'
import PageLoadError from './PageLoadError.vue'
import { accountTasks, hasTasks, taskNames } from '../taskSettings'
import { alreadyComplete, resultLabel } from '../taskResults'

const props = defineProps<{ accounts: Account[]; tasks: TaskProgress[]; userId: string; enabled: boolean; timezone: string; actionBusy: boolean }>()
const emit = defineEmits<{ changed: []; bind: [account?: Account]; configure: [account: Account]; run: [id: string]; batchRun: [ids: string[]]; stop: [id: string] }>()
const AccountBatchModal = defineAsyncComponent({ loader: () => import('./AccountBatchModal.vue'), loadingComponent: PageLoading, errorComponent: PageLoadError, delay: 150, timeout: 30000 })
const batchOpen = ref(false), groupFilter = ref<string | null>(null)
const groups = computed(() => [...new Set(props.accounts.map(a => a.group || '').filter(Boolean))].sort())
const visibleAccounts = computed(() => props.accounts.filter(a => groupFilter.value === null || (a.group || '') === groupFilter.value))
watch(groups, values => { if (groupFilter.value && !values.includes(groupFilter.value)) groupFilter.value = null })
const editing = ref(false), busy = ref(false), error = ref(''), notice = ref(''), deleteID = ref('')
const draft = ref({ id: '', name: '', disabled: false, cookie: '', stoken: '', mid: '', cloud_tokens: { genshin: '', zzz: '' }, clear_cloud_tokens: [] as string[] })
const source = ref<Account | null>(null)
const taskLabel: Record<string, string> = { bbs: '米游币任务', games: '游戏签到', cloud: '云游戏签到' }
const progress = (a: Account) => props.tasks.find(t => t.account_id === a.id)
const hasResults = (a: Account) => Object.keys(a.task_results || {}).length > 0
const statusLabel = (a: Account) => a.disabled ? '已停用' : a.status === 'valid' ? '凭据有效' : a.status === 'expired' ? '登录已过期' : a.status === 'check_failed' ? '需要检查' : '待检查'
function edit(a?: Account) {
  source.value = a || null
  draft.value = { id: a?.id || '', name: a?.name || '', disabled: a?.disabled || false, cookie: '', stoken: '', mid: '', cloud_tokens: { genshin: '', zzz: '' }, clear_cloud_tokens: [] }
  error.value = ''; editing.value = true
}
async function save() {
  busy.value = true; error.value = ''
  try { await api('/api/v1/accounts', { method: draft.value.id ? 'PUT' : 'POST', body: JSON.stringify(draft.value) }); editing.value = false; notice.value = '账号设置已保存'; emit('changed') }
  catch (e) { error.value = e instanceof Error ? e.message : '保存失败' } finally { busy.value = false }
}
async function check(a: Account) {
  busy.value = true; error.value = ''; notice.value = ''
  try { await api('/api/v1/accounts/check', { method: 'POST', body: JSON.stringify({ id: a.id }) }); notice.value = a.name + ' 的凭据检查通过' }
  catch (e) { error.value = e instanceof Error ? e.message : '检查失败' } finally { busy.value = false; emit('changed') }
}
async function remove(a: Account) {
  if (deleteID.value !== a.id) { deleteID.value = a.id; return }
  busy.value = true; error.value = ''
  try { await api('/api/v1/accounts?id=' + encodeURIComponent(a.id), { method: 'DELETE' }); deleteID.value = ''; notice.value = '账号及关联的兑换计划已删除'; emit('changed') }
  catch (e) { error.value = e instanceof Error ? e.message : '删除失败' } finally { busy.value = false }
}
function rebind(a: Account) { emit('bind', a) }
</script>
<template>
  <article class="panel accounts-panel">
    <div class="panel-title"><div><p class="eyebrow">账号管理</p><h2>米游社账号 <span class="count-label">{{ accounts.length }}</span></h2></div><button class="small-button primary-mini" @click="emit('bind')"><AppIcon name="plus" :size="14" />绑定账号</button></div>
    <p v-if="error && !editing" class="error-banner" role="alert">{{ error }}</p><p v-if="notice" class="notice" role="status">{{ notice }}</p>
    <div v-if="groups.length" class="account-group-filters" role="group" aria-label="账号分组"><button class="small-button" :aria-pressed="groupFilter === null" @click="groupFilter = null">全部</button><button class="small-button" :aria-pressed="groupFilter === ''" @click="groupFilter = ''">未分组</button><button v-for="group in groups" :key="group" class="small-button" :aria-pressed="groupFilter === group" @click="groupFilter = group">{{ group }}</button></div>
    <div class="account-list">
      <section v-for="a in visibleAccounts" :key="a.id" class="account-card" :class="{ 'account-working': progress(a), 'account-disabled': a.disabled }">
        <div class="account-heading"><span class="account-avatar">{{ a.name.slice(0, 1).toUpperCase() }}</span><div class="account-identity"><strong>{{ a.name }}</strong><small>米游社 UID {{ a.stuid || '待识别' }}</small></div><span class="pill" :class="{ soft: a.status === 'valid' && !a.disabled, warning: ['expired', 'check_failed'].includes(a.status) }">{{ statusLabel(a) }}</span></div>
        <div v-if="progress(a)" class="account-progress" role="status"><span class="loading-orbit small"></span><span>{{ progress(a)?.current }}</span><small>{{ progress(a)?.state === 'queued' ? '排队中' : '进行中' }}</small></div>
        <p v-else class="account-last-run">{{ a.last_task_at && !a.last_task_at.startsWith('0001') ? '最近执行 · ' + formatDate(a.last_task_at, timezone) : '准备就绪，开始第一次签到吧' }}</p>
        <div class="account-task-summary"><span class="account-auto-tag" :class="{ on: accountTasks(a).automatic && !a.disabled && hasTasks(a) }"><AppIcon name="clock" :size="12" />{{ accountTasks(a).automatic ? '参加每日安排' : '仅手动签到' }}</span><span v-for="name in taskNames(a)" :key="name">{{ name }}</span><span v-if="!hasTasks(a)">尚未选择签到任务</span></div>
        <div class="account-actions">
          <button v-if="progress(a)" class="small-button" :disabled="actionBusy" @click="emit('stop', a.id)"><AppIcon name="stop" :size="13" />停止</button>
          <button v-else class="small-button primary-mini" :disabled="a.disabled || !enabled || !hasTasks(a) || actionBusy || busy" @click="emit('run', a.id)"><AppIcon name="play" :size="13" />签到</button>
          <button class="small-button" :disabled="busy" @click="emit('configure', a)"><AppIcon name="settings" :size="13" />签到设置</button>
          <button class="small-button" :disabled="busy || !!progress(a)" @click="check(a)">检查凭据</button>
          <button class="small-button" :disabled="busy" @click="edit(a)">管理</button>
          <button v-if="a.user_id === userId" class="text-button" :disabled="busy || !!progress(a)" @click="rebind(a)">重新登录</button>
        </div>
        <p v-if="a.task_results?.bbs && alreadyComplete(a.task_results.bbs)" class="account-completion"><AppIcon name="check" :size="14" />米游币：今日无待领取奖励，本次仅检查状态</p>
        <details v-if="hasResults(a)" class="result-details"><summary>签到结果 <span class="result-counts"><span v-if="Object.values(a.task_results).some(r => r.success)" class="success-ink">{{ Object.values(a.task_results).reduce((sum, r) => sum + r.success, 0) }} 项执行成功</span><span v-else-if="Object.values(a.task_results).every(alreadyComplete)" class="success-ink">已完成状态检查</span><span v-else>本次未执行操作</span><span v-if="Object.values(a.task_results).some(r => r.failed)" class="error-ink">{{ Object.values(a.task_results).reduce((sum, r) => sum + r.failed, 0) }} 失败</span></span><AppIcon name="chevron" :size="13" /></summary><section v-for="(result, key) in a.task_results" :key="key" class="task-result"><div class="task-result-title"><strong>{{ taskLabel[key] || key }}</strong><small>{{ resultLabel(result) }}</small></div><p v-if="result.reason" class="muted">{{ result.reason }}</p><ul v-if="result.details?.length"><li v-for="(line, i) in result.details" :key="i">{{ line }}</li></ul></section></details>
        <div v-if="deleteID === a.id" class="delete-confirm"><p>将删除此账号及其全部兑换计划。</p><button class="small-button danger-button" :disabled="busy" @click="remove(a)">确认删除</button><button class="small-button" @click="deleteID = ''">保留账号</button></div>
      </section>
      <div v-if="!accounts.length" class="empty rich-empty"><AppIcon name="scan" :size="34" /><p>尚未绑定米游社账号</p><small>支持扫码登录、短信登录或手动填写凭据。</small><button class="small-button" @click="emit('bind')">添加米游社账号<AppIcon name="arrow" :size="14" /></button></div>
    </div>
    <p v-if="accounts.length && !visibleAccounts.length" class="muted">这个分组暂无账号。<button class="text-button" @click="groupFilter = null">查看全部</button></p>
    <div class="account-tools"><button class="text-button manual-add" @click="edit()">已有 Cookie？手动添加账号</button><button v-if="accounts.length" class="text-button" @click="batchOpen = true"><AppIcon name="filter" :size="14" />分组与批量</button></div>
  </article>

  <AccountBatchModal v-if="batchOpen" :accounts="accounts" :initial-group="groupFilter" :enabled="enabled" :action-busy="actionBusy" :timezone="timezone" @close="batchOpen = false" @changed="groupFilter = null; notice = '批量设置已保存'; emit('changed')" @run="ids => emit('batchRun', ids)" />

  <ModalShell v-if="editing" :title="draft.id ? '管理账号' : '手动添加账号'" :busy="busy" wide @close="editing = false">
    <form @submit.prevent="save">
      <FloatingSave label="保存账号" :busy="busy" />
      <div class="form-grid"><label>账号名称<input v-model="draft.name" maxlength="64" required /></label><label>Cookie<input v-model="draft.cookie" type="password" :required="!draft.id" autocomplete="new-password" :placeholder="draft.id ? '留空保留已保存凭据' : '粘贴完整 Cookie'" /></label><label>SToken<input v-model="draft.stoken" type="password" autocomplete="new-password" placeholder="从 Cookie 自动读取，或单独填写" /></label><label>MID<input v-model="draft.mid" type="password" autocomplete="new-password" placeholder="从 Cookie 自动读取，或单独填写" /></label></div>
      <div class="subtle-card"><h3>云游戏凭据</h3><p class="muted">在对应云游戏网页的网络请求中，复制 x-rpc-combo_token；两个游戏需要分别配置。</p><div class="form-grid"><label>云·原神<input v-model="draft.cloud_tokens.genshin" type="password" autocomplete="new-password" :placeholder="source?.cloud_configured?.includes('genshin') ? '已配置，留空保留' : '尚未配置'" /></label><label>云·绝区零<input v-model="draft.cloud_tokens.zzz" type="password" autocomplete="new-password" :placeholder="source?.cloud_configured?.includes('zzz') ? '已配置，留空保留' : '尚未配置'" /></label></div><div class="check-grid"><label v-if="source?.cloud_configured?.includes('genshin')"><input v-model="draft.clear_cloud_tokens" type="checkbox" value="genshin" />清除云·原神凭据</label><label v-if="source?.cloud_configured?.includes('zzz')"><input v-model="draft.clear_cloud_tokens" type="checkbox" value="zzz" />清除云·绝区零凭据</label></div></div>
      <label class="check-row field-label"><input v-model="draft.disabled" type="checkbox" />暂停此账号的签到与兑换</label>
      <p v-if="error" class="error-banner" role="alert">{{ error }}</p>
      <div class="modal-actions"><button data-save-inline class="button primary" :disabled="busy">{{ busy ? '正在保存…' : '保存账号' }}<AppIcon name="check" /></button><button v-if="source" type="button" class="text-button error-ink" :disabled="busy" @click="deleteID = source.id; editing = false">删除账号</button></div>
    </form>
  </ModalShell>
</template>

<style scoped>
.account-group-filters{display:flex;gap:6px;flex-wrap:wrap;margin-bottom:14px}.account-group-filters button{max-width:100%;overflow-wrap:anywhere}.account-group-filters [aria-pressed=true]{background:#e7ecdf;color:#485a3d;border-color:#c7d1bb}.account-tools{display:flex;gap:12px;justify-content:space-between;align-items:center;flex-wrap:wrap;margin-top:15px}.account-tools .manual-add{margin:0}
</style>

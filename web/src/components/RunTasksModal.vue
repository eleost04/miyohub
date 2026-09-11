<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Config, TaskProgress, TaskRunSelection } from '../types'
import ModalShell from './ModalShell.vue'
import AppIcon from './AppIcon.vue'
import { accountTasks, hasTasks } from '../taskSettings'

const props = defineProps<{ config: Config; tasks: TaskProgress[]; busy: boolean; error: string }>()
const emit = defineEmits<{ close: []; run: [selection: TaskRunSelection] }>()
const mode = ref('all')
const running = computed(() => new Set(props.tasks.map(task => task.account_id)))
const eligible = computed(() => props.config.accounts.filter(account => !account.disabled && hasTasks(account) && !running.value.has(account.id)))
const accounts = ref(eligible.value.map(account => account.id))
const names: Record<string, string> = { genshin: '原神', starrail: '星穹铁道', zzz: '绝区零', honkai3rd: '崩坏 3', tears: '未定事件簿', honkai2: '崩坏学园 2' }
const chosenAccounts = computed(() => eligible.value.filter(account => accounts.value.includes(account.id)))
const availableGames = computed(() => [...new Set(chosenAccounts.value.flatMap(account => { const t = accountTasks(account); return [...(t.features.game_checkin ? t.games.enabled : []), ...(t.features.cloud_game_checkin ? t.cloud_games.enabled : [])] }))])
const games = ref([...availableGames.value])
const hasBBS = computed(() => chosenAccounts.value.some(account => { const t = accountTasks(account); return t.features.bbs_tasks && t.bbs.forums.length > 0 && (t.bbs.checkin || t.bbs.read || t.bbs.like || t.bbs.share) }))
const selectedAccounts = computed(() => accounts.value.filter(id => eligible.value.some(account => account.id === id)))
const selectedGames = computed(() => games.value.filter(key => availableGames.value.includes(key)))
const canRun = computed(() => props.config.enabled && selectedAccounts.value.length > 0 && (mode.value === 'bbs' ? hasBBS.value : selectedGames.value.length > 0 || mode.value === 'all' && hasBBS.value && !availableGames.value.length))
function submit() {
  if (!canRun.value || props.busy) return
  const selection: TaskRunSelection = { account_ids: selectedAccounts.value }
  if (mode.value === 'bbs') selection.bbs_only = true
  else {
    if (mode.value === 'games') selection.games_only = true
    if (selectedGames.value.length) selection.games = selectedGames.value
  }
  emit('run', selection)
}
</script>

<template>
  <ModalShell title="选择本次任务" :busy="busy" @close="emit('close')">
    <p class="muted">每个账号按自己的签到设置执行。这里仅缩小本次范围，不会启用账号已关闭的任务。</p>
    <form class="run-selection" @submit.prevent="submit">
      <fieldset class="settings-fields" :disabled="busy">
        <div class="run-section-heading"><h3>选择账号 <span class="count-label">{{ selectedAccounts.length }}</span></h3><button type="button" class="text-button" @click="accounts = selectedAccounts.length === eligible.length ? [] : eligible.map(account => account.id)">{{ selectedAccounts.length === eligible.length ? '取消全选' : '全选可用账号' }}</button></div>
        <div class="run-account-list"><label v-for="account in config.accounts" :key="account.id" class="run-account-option" :class="{ unavailable: account.disabled || running.has(account.id) || !hasTasks(account) }"><input v-model="accounts" type="checkbox" :value="account.id" :disabled="account.disabled || running.has(account.id) || !hasTasks(account)" /><span>{{ account.name }}</span><small>{{ account.disabled ? '已停用' : running.has(account.id) ? '执行中' : !hasTasks(account) ? '未选择任务' : '可执行' }}</small></label></div>
        <h3>任务范围</h3>
        <div class="run-mode-grid"><label :class="{ selected: mode === 'all' }"><input v-model="mode" type="radio" name="task-mode" value="all" />全部已启用任务</label><label :class="{ selected: mode === 'games' }"><input v-model="mode" type="radio" name="task-mode" value="games" :disabled="!availableGames.length" />仅游戏与云游戏</label><label :class="{ selected: mode === 'bbs' }"><input v-model="mode" type="radio" name="task-mode" value="bbs" :disabled="!hasBBS" />仅米游币任务</label></div>
        <template v-if="mode !== 'bbs' && availableGames.length"><h3>参与的游戏</h3><div class="check-grid"><label v-for="key in availableGames" :key="key"><input v-model="games" type="checkbox" :value="key" />{{ names[key] || key }}</label></div><p class="muted">游戏选择同时适用于已启用的云游戏；云游戏仍需独立 Token。</p></template>
      </fieldset>
      <p v-if="!eligible.length" class="error-text">没有可执行的账号。请检查是否已绑定账号、启用任务，或等待正在运行的任务结束。</p>
      <p v-if="!config.enabled" class="error-text">管理员已暂停签到任务。</p>
      <p v-if="error" class="error-banner" role="alert">{{ error }}</p>
      <div class="modal-actions"><button class="button primary" :disabled="busy || !canRun">{{ busy ? '正在启动…' : '开始所选签到' }}<AppIcon name="play" :size="15" /></button><button type="button" class="small-button" :disabled="busy" @click="emit('close')">取消</button></div>
    </form>
  </ModalShell>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { api } from '../api'
import { accountTasks, games, forums } from '../taskSettings'
import type { Account, AccountTaskSettings, Config } from '../types'
import ModalShell from './ModalShell.vue'
import AppIcon from './AppIcon.vue'
import GameIcon from './GameIcon.vue'
import FloatingSave from './FloatingSave.vue'
import AutoSaveStatus from './AutoSaveStatus.vue'
import ChoiceSelect from './ChoiceSelect.vue'
import TimeField from './TimeField.vue'
import { useAutoSave } from '../autosave'

const props = defineProps<{ account: Account; schedule: Config['schedule'] }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const draft = ref<AccountTaskSettings>(JSON.parse(JSON.stringify(accountTasks(props.account))))
const baseline = ref(JSON.stringify(draft.value)), form = ref<HTMLFormElement | null>(null)
const tab = ref('games'), busy = ref(false), error = ref(''), discard = ref(false)
const tabs = [{ key: 'games', name: '游戏签到', icon: 'check' }, { key: 'cloud', name: '云游戏', icon: 'cloud' }, { key: 'bbs', name: '米游币', icon: 'gift' }]
const bbsActions = [{ key: 'checkin', name: '社区签到' }, { key: 'read', name: '看帖' }, { key: 'like', name: '点赞' }, { key: 'share', name: '分享' }] as const
const bbsMode = computed({ get: () => draft.value.bbs.run_all_selected ? 'selected' : 'missions', set: value => { draft.value.bbs.run_all_selected = value === 'selected' } })
const bbsModes = [{ value: 'missions', label: '按奖励进度执行', description: '已完成或未列出的互动任务跳过；社区签到缺项时仍按所选社区尝试' }, { value: 'selected', label: '按所选项目执行', description: '即使没有奖励任务也执行已开启的项目；不保证获得米游币' }]
const dirty = computed(() => JSON.stringify(draft.value) !== baseline.value)
const timeSource = computed({ get: () => draft.value.schedule ? 'custom' : 'site', set: value => { draft.value.schedule = value === 'custom' ? { time: props.schedule.time || '09:00', timezone: props.schedule.timezone || 'Asia/Shanghai' } : null } })
const timeSources = [{ value: 'custom', label: '自定义签到时间', description: '这个账号按你设置的时间签到，不再跟随站点时间' }, { value: 'site', label: '使用站点默认时间', description: '未设置个人时间时，由站点安排' }]
const zones = ['Asia/Shanghai', 'Asia/Hong_Kong', 'Asia/Tokyo', 'Asia/Singapore', 'UTC', 'Europe/London', 'America/New_York', 'America/Los_Angeles'].map(value => ({ value, label: value }))
const autosave = useAutoSave(() => JSON.stringify(draft.value), dirty, busy, () => !!form.value?.checkValidity(), persist)
function close() { if (busy.value) return; if (dirty.value) discard.value = true; else emit('close') }
function blacklist(key: string, event: Event) { draft.value.games.black_list[key] = (event.target as HTMLInputElement).value.split(/[，,\s]+/).filter(Boolean) }
async function persist() {
  if (busy.value || !dirty.value) return
  const sent = JSON.stringify(draft.value)
  busy.value = true; error.value = ''
  try {
    const saved = await api<Account>('/api/v1/accounts/tasks', { method: 'PUT', body: JSON.stringify({ id: props.account.id, task_settings: JSON.parse(sent) }) })
    if (JSON.stringify(draft.value) === sent) draft.value = saved.task_settings
    else draft.value.revision = saved.task_settings.revision
    baseline.value = JSON.stringify(saved.task_settings); emit('saved')
  }
  catch (e) { error.value = e instanceof Error ? e.message : '保存失败' } finally { busy.value = false }
}
async function save() { await persist(); if (!error.value && !dirty.value) emit('close') }
</script>

<template>
  <ModalShell :title="account.name + ' · 签到设置'" :busy="busy" wide @close="close">
    <form ref="form" class="account-task-form" @submit.prevent="save">
      <FloatingSave label="保存签到设置" :active="dirty || busy" :busy="busy" />
      <AutoSaveStatus :state="autosave.state.value" :error="error" />
      <fieldset class="settings-fields">
        <label class="preference-switch"><span><strong>参加每日自动签到</strong><small>{{ draft.schedule ? `每天 ${draft.schedule.time} · ${draft.schedule.timezone}` : schedule.enable ? `站点默认：每天 ${schedule.time}，随机延后 0–${schedule.jitter_minutes} 分钟` : '站点默认调度未开启；可设置自己的时间或手动签到' }}<br />仅影响这个账号，不影响兑换计划。</small></span><span class="push-switch"><input v-model="draft.automatic" type="checkbox" aria-label="参加每日自动签到" /><span aria-hidden="true"></span></span></label>
        <div class="account-schedule-card"><label>签到时间来源<ChoiceSelect v-model="timeSource" label="签到时间来源" :options="timeSources" /></label><div v-if="draft.schedule" class="form-grid"><label>每天签到时间<TimeField v-model="draft.schedule.time" label="每天签到时间" /></label><label>签到时区<ChoiceSelect v-model="draft.schedule.timezone" label="签到时区" :options="zones" /></label></div><p class="muted">个人时间优先，未设置时使用站点默认时间。每个账号每天自动执行一次；服务需在设置的时间保持运行，错过的时间不会在重启后补跑。</p></div>
        <div class="preference-tabs" aria-label="签到任务类别"><button v-for="item in tabs" :key="item.key" type="button" :aria-pressed="tab === item.key" :class="{ active: tab === item.key }" @click="tab = item.key"><AppIcon :name="item.icon" :size="16" />{{ item.name }}</button></div>
        <section v-show="tab === 'games'" class="preference-section">
          <label class="check-row section-switch"><input v-model="draft.features.game_checkin" type="checkbox" />启用游戏签到</label>
          <p class="muted">选择这个账号需要签到的游戏，没有角色的游戏会自动跳过。</p>
          <div class="game-choice-grid"><label v-for="game in games" :key="game.key" class="game-choice" :class="{ selected: draft.features.game_checkin && draft.games.enabled.includes(game.key) }"><span class="game-mark"><GameIcon :name="game.key" /></span><strong>{{ game.name }}</strong><input v-model="draft.games.enabled" type="checkbox" :value="game.key" :disabled="!draft.features.game_checkin" :aria-label="game.name + '签到'" /></label></div>
          <details class="preference-advanced"><summary>按角色 UID 排除<AppIcon name="chevron" :size="14" /></summary><p class="muted">只跳过这个账号下指定角色的签到，多个 UID 用逗号分隔。</p><div class="form-grid"><label v-for="game in games.filter(g => draft.games.enabled.includes(g.key))" :key="game.key">{{ game.name }} · 排除 UID<input :value="draft.games.black_list[game.key]?.join(', ')" inputmode="text" placeholder="例如 100000001, 100000002" @change="blacklist(game.key, $event)" /></label></div></details>
        </section>
        <section v-show="tab === 'cloud'" class="preference-section">
          <label class="check-row section-switch"><input v-model="draft.features.cloud_game_checkin" type="checkbox" />启用云游戏签到</label>
          <p class="muted">云游戏需要单独的 Combo Token，可在该账号的「管理 → 云游戏凭据」中填写。</p>
          <div class="game-choice-grid"><label v-for="game in games.filter(g => ['genshin', 'zzz'].includes(g.key))" :key="game.key" class="game-choice cloud-choice" :class="{ selected: draft.features.cloud_game_checkin && draft.cloud_games.enabled.includes(game.key) }"><span class="game-mark"><AppIcon name="cloud" :size="20" /></span><span><strong>云·{{ game.name }}</strong><small>{{ account.cloud_configured?.includes(game.key) ? '凭据已配置' : '尚未配置云游戏凭据' }}</small></span><input v-model="draft.cloud_games.enabled" type="checkbox" :value="game.key" :disabled="!draft.features.cloud_game_checkin" :aria-label="'云·' + game.name + '签到'" /></label></div>
        </section>
        <section v-show="tab === 'bbs'" class="preference-section">
          <label class="check-row section-switch"><input v-model="draft.features.bbs_tasks" type="checkbox" />启用米游币任务</label>
          <label class="bbs-mode-field">执行方式<ChoiceSelect v-model="bbsMode" label="米游币执行方式" :options="bbsModes" :disabled="!draft.features.bbs_tasks" /></label>
          <p class="muted">{{ draft.bbs.run_all_selected ? '每次运行均执行已开启项目：所选社区各签到一次，看帖最多 3 篇、点赞最多 5 篇、分享最多 1 篇，受帖子数限制。不保证获得米游币；手动再次运行会重新执行。' : '按当天奖励进度执行，已完成或未列出的互动项目跳过。社区签到缺项时仍尝试已选社区。' }}验证码使用你的个人打码设置。</p>
          <div class="check-grid preference-choices"><label v-for="action in bbsActions" :key="action.key"><input v-model="draft.bbs[action.key]" type="checkbox" :disabled="!draft.features.bbs_tasks" />{{ action.name }}</label></div>
          <h3>参与的社区</h3><div class="check-grid preference-choices"><label v-for="forum in forums" :key="forum.id"><input v-model="draft.bbs.forums" type="checkbox" :value="forum.id" :disabled="!draft.features.bbs_tasks" />{{ forum.name }}</label></div>
          <details class="preference-advanced"><summary>执行间隔与更多参数<AppIcon name="chevron" :size="14" /></summary><label class="check-row"><input v-model="draft.bbs.cancel_like" type="checkbox" />完成后取消点赞</label><div class="form-grid"><label>最多帖子数<input v-model.number="draft.bbs.post_limit" type="number" min="1" max="20" required /></label><label>最小任务间隔（秒）<input v-model.number="draft.bbs.delay_seconds[0]" type="number" min="0" max="60" required /></label><label>最大任务间隔（秒）<input v-model.number="draft.bbs.delay_seconds[1]" type="number" :min="draft.bbs.delay_seconds[0]" max="60" required /></label></div></details>
        </section>
      </fieldset>
      <p v-if="error" class="error-banner" role="alert">{{ error }}</p>
      <div v-if="discard" class="discard-inline" role="alert"><p>有未保存的签到设置，要放弃吗？</p><button type="button" class="small-button" @click="discard = false">继续编辑</button><button type="button" class="text-button error-ink" @click="emit('close')">放弃修改</button></div>
      <div class="modal-actions sticky-modal-actions"><button data-save-inline class="button primary" :disabled="busy">{{ busy ? '正在保存…' : dirty ? '保存签到设置' : '完成' }}<AppIcon name="check" :size="16" /></button><button v-if="dirty" type="button" class="small-button" :disabled="busy" @click="close">取消</button></div>
    </form>
  </ModalShell>
</template>
<style scoped>.account-schedule-card{display:grid;gap:14px;padding:16px;margin:16px 0;border:1px solid #e6e0d3;background:#faf8f1;border-radius:14px}.account-schedule-card label,.bbs-mode-field{display:grid;gap:8px;font-size:12px;color:#707763}.account-schedule-card .muted{font-size:11px;margin:0}</style>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { LogEntry } from '../types'
import { formatDate } from '../time'
import AppIcon from './AppIcon.vue'
import ChoiceSelect from './ChoiceSelect.vue'
const props = defineProps<{ logs: LogEntry[]; running?: boolean; timezone: string; compact?: boolean }>()
defineEmits<{ all: [] }>()
const search = ref(''), component = ref(''), limit = ref(80), issuesOnly = ref(false)
function needsAttention(log: LogEntry) { return /失败|未完成|未确认|尚未确认|未增加|过期|不足|中断|错误|无法/.test(log.message) }
const labels: Record<string, string> = { task: '任务', games: '游戏签到', cloud: '云游戏', bbs: '米游币', captcha: '验证码识别', exchange: '兑换', scheduler: '调度', auth: '登录', config: '配置', push: '推送' }
function displayMessage(log: LogEntry) {
  // Present the old acknowledgement text consistently without rewriting the
  // stored audit record or changing an unknown/failed result into a success.
  const legacy = '服务已接受通知（不代表终端已读）'
  return log.component === 'push' && log.message.endsWith(legacy) ? log.message.slice(0, -legacy.length) + '推送服务已接收通知' : log.message
}
const filtered = computed(() => [...props.logs].reverse().map(log => ({ ...log, message: displayMessage(log) })).filter(log => (!component.value || log.component === component.value) && (!search.value || log.message.toLowerCase().includes(search.value.toLowerCase())) && (!issuesOnly.value || needsAttention(log))))
const components = computed(() => [...new Set(props.logs.map(log => log.component))])
function exportLogs() {
  const body = [...filtered.value].reverse().map(log => formatDate(log.at, props.timezone) + ' [' + (labels[log.component] || log.component) + '] ' + log.message).join('\n')
  const url = URL.createObjectURL(new Blob([body], { type: 'text/plain;charset=utf-8' }))
  const link = document.createElement('a'); link.href = url; link.download = 'miyohub-logs.txt'; link.click(); window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}
</script>
<template>
  <article class="panel logs-panel" :class="{ 'logs-compact': compact }"><div class="panel-title"><div><p class="eyebrow">执行记录</p><h2>{{ compact ? '最近记录' : '运行日志' }} <span v-if="running" class="live-label">实时更新</span></h2></div><button v-if="compact" class="text-button" @click="$emit('all')">查看全部日志<AppIcon name="arrow" :size="14" /></button><button v-else class="small-button" :disabled="!filtered.length" @click="exportLogs"><AppIcon name="download" :size="14" />导出</button></div>
    <template v-if="!compact"><div class="log-quick-filters"><button class="small-button" :class="{ 'primary-mini': component === 'bbs' }" @click="component = component === 'bbs' ? '' : 'bbs'; limit = 80">米游币任务</button><button class="small-button" :class="{ 'primary-mini': issuesOnly }" :aria-pressed="issuesOnly" @click="issuesOnly = !issuesOnly; limit = 80"><AppIcon name="filter" :size="14" />只看异常</button><span>最近 {{ logs.length }} 条 · 自动刷新</span></div><div class="activity-toolbar"><label class="search-field"><AppIcon name="search" :size="16" /><input v-model="search" type="search" aria-label="搜索日志" placeholder="搜索账号、任务或结果…" @input="limit = 80" /></label><ChoiceSelect v-model="component" label="日志类型" :options="[{ value: '', label: '全部类型' }, ...components.map(key => ({ value: key, label: labels[key] || key }))]" @change="limit = 80" /></div></template>
    <div v-if="filtered.length" class="activity-list"><div v-for="(entry, index) in filtered.slice(0, compact ? 6 : limit)" :key="entry.at + index" class="activity-row" :class="{ 'log-attention': needsAttention(entry) }"><time>{{ formatDate(entry.at, timezone) }}</time><span class="log-kind">{{ labels[entry.component] || entry.component }}</span><p>{{ entry.message }}</p></div></div>
    <div v-else class="empty rich-empty"><AppIcon name="clock" :size="30" /><p>{{ logs.length ? '没有找到匹配的记录' : '暂无运行记录' }}</p><small>{{ logs.length ? '请调整关键词或日志筛选条件。' : '执行任务或修改配置后，可在这里查看记录。' }}</small></div>
    <div v-if="filtered.length && !compact" class="activity-footer"><span>共 {{ filtered.length }} 条记录 · {{ timezone }}</span><button v-if="filtered.length > limit" class="small-button" @click="limit += 80">加载更多</button></div>
  </article>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, ref } from 'vue'
import type { LogEntry } from '../types'
import { formatDate } from '../time'
import AppIcon from './AppIcon.vue'
import ChoiceSelect from './ChoiceSelect.vue'
import PageLoading from './PageLoading.vue'
import PageLoadError from './PageLoadError.vue'
import { displayLogMessage as displayMessage, downloadLogs, isLogIssue as needsAttention, keyedLogEntries, logContext, logLabels as labels } from '../logs'
const LogDetailsModal = defineAsyncComponent({ loader: () => import('./LogDetailsModal.vue'), loadingComponent: PageLoading, errorComponent: PageLoadError, delay: 150, timeout: 30000 })
const props = defineProps<{ logs: LogEntry[]; running?: boolean; timezone: string; compact?: boolean }>()
defineEmits<{ all: [] }>()
const search = ref(''), component = ref(''), limit = ref(80), issuesOnly = ref(false)
const selected = ref<{ entry: LogEntry; correlated: boolean; entries: LogEntry[] } | null>(null)
function openLog(entry: LogEntry) { selected.value = { entry, ...logContext(props.logs, entry) } }
const detailEntries = computed(() => {
  if (!selected.value) return []
  if (!selected.value.correlated) return selected.value.entries
  const current = logContext(props.logs, selected.value.entry).entries
  return current.length ? current : selected.value.entries
})
const filtered = computed(() => [...props.logs].reverse().filter(log => (!component.value || log.component === component.value) && (!search.value || displayMessage(log).toLowerCase().includes(search.value.toLowerCase())) && (!issuesOnly.value || needsAttention(log))))
const visibleRows = computed(() => {
  const included = new Set(filtered.value.slice(0, props.compact ? 6 : limit.value))
  return keyedLogEntries(props.logs).reverse().filter(row => included.has(row.entry))
})
const components = computed(() => [...new Set(props.logs.map(log => log.component))])
function exportLogs() {
  downloadLogs([...filtered.value].reverse(), props.timezone)
}
</script>
<template>
  <article class="panel logs-panel" :class="{ 'logs-compact': compact }"><div class="panel-title"><div><p class="eyebrow">执行记录</p><h2>{{ compact ? '最近记录' : '运行日志' }} <span v-if="running" class="live-label">实时更新</span></h2></div><button v-if="compact" class="text-button" @click="$emit('all')">查看全部日志<AppIcon name="arrow" :size="14" /></button><button v-else class="small-button" :disabled="!filtered.length" @click="exportLogs"><AppIcon name="download" :size="14" />导出</button></div>
    <template v-if="!compact"><div class="log-quick-filters"><button class="small-button" :class="{ 'primary-mini': component === 'bbs' }" @click="component = component === 'bbs' ? '' : 'bbs'; limit = 80">米游币任务</button><button class="small-button" :class="{ 'primary-mini': issuesOnly }" :aria-pressed="issuesOnly" @click="issuesOnly = !issuesOnly; limit = 80"><AppIcon name="filter" :size="14" />只看异常</button><span>最近 {{ logs.length }} 条 · 自动刷新</span></div><div class="activity-toolbar"><label class="search-field"><AppIcon name="search" :size="16" /><input v-model="search" type="search" aria-label="搜索日志" placeholder="搜索账号、任务或结果…" @input="limit = 80" /></label><ChoiceSelect v-model="component" label="日志类型" :options="[{ value: '', label: '全部类型' }, ...components.map(key => ({ value: key, label: labels[key] || key }))]" @change="limit = 80" /></div></template>
    <div v-if="filtered.length" class="activity-list"><div v-for="{ entry, key } in visibleRows" :key="key" class="activity-row" :class="{ 'log-attention': needsAttention(entry) }"><time>{{ formatDate(entry.at, timezone) }}</time><span class="log-kind">{{ labels[entry.component] || entry.component }}</span><button type="button" class="activity-message" :aria-label="'查看' + (labels[entry.component] || entry.component) + '日志明细'" @click="openLog(entry)"><span class="log-message">{{ displayMessage(entry) }}</span><span class="log-detail-hint">明细<AppIcon name="chevron" :size="12" /></span></button></div></div>
    <div v-else class="empty rich-empty"><AppIcon name="clock" :size="30" /><p>{{ logs.length ? '没有找到匹配的记录' : '暂无运行记录' }}</p><small>{{ logs.length ? '请调整关键词或日志筛选条件。' : '执行任务或修改配置后，可在这里查看记录。' }}</small></div>
    <div v-if="filtered.length && !compact" class="activity-footer"><span>共 {{ filtered.length }} 条记录 · {{ timezone }}</span><button v-if="filtered.length > limit" class="small-button" @click="limit += 80">加载更多</button></div>
  </article>
  <LogDetailsModal v-if="selected" :entry="selected.entry" :entries="detailEntries" :correlated="selected.correlated" :timezone="timezone" @close="selected = null" />
</template>

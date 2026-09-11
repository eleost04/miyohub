<script setup lang="ts">
import type { LogEntry } from '../types'
import { displayLogMessage, downloadLogs, isLogIssue, keyedLogEntries, logKey, logLabels } from '../logs'
import { formatDate } from '../time'
import AppIcon from './AppIcon.vue'
import ModalShell from './ModalShell.vue'

defineProps<{ entry: LogEntry; entries: LogEntry[]; correlated: boolean; timezone: string }>()
defineEmits<{ close: [] }>()
</script>

<template>
  <ModalShell title="日志详情" :eyebrow="logLabels[entry.component] || entry.component" wide @close="$emit('close')">
    <section class="log-selection"><time>{{ formatDate(entry.at, timezone) }}</time><p>{{ displayLogMessage(entry) }}</p></section>
    <p class="log-context-note">{{ correlated ? '同一账号、同一次执行的已保留记录，按时间排列。' : '历史记录没有执行编号，下面是该条记录前后的日志，不能确认全部属于同一次执行。' }}</p>
    <div class="log-detail-toolbar"><span>{{ entries.length }} 条已保留记录</span><button type="button" class="small-button" @click="downloadLogs(entries, timezone, correlated ? 'miyohub-run-logs.txt' : 'miyohub-log-context.txt')"><AppIcon name="download" :size="14" />{{ correlated ? '导出本次记录' : '导出上下文' }}</button></div>
    <ol class="log-detail-list">
      <li v-for="{ entry: line, key } in keyedLogEntries(entries)" :key="key" class="log-detail-entry" :class="{ 'is-issue': isLogIssue(line), 'is-selected': logKey(line) === logKey(entry) }">
        <div><time>{{ formatDate(line.at, timezone) }}</time><span>{{ logLabels[line.component] || line.component }}</span><small v-if="logKey(line) === logKey(entry)">所选记录</small></div>
        <p>{{ displayLogMessage(line) }}</p>
      </li>
    </ol>
    <p class="log-retention-note">只显示站点当前保留的日志；已清理的早期记录无法在这里恢复。</p>
  </ModalShell>
</template>

<style scoped>
.log-selection { padding: 14px 16px; border-radius: 13px; background: #f4f3e9; }
.log-selection time, .log-detail-entry time { color: #958f7f; font-size: 10px; font-variant-numeric: tabular-nums; }
.log-selection p, .log-detail-entry p { margin: 7px 0 0; color: #656853; font-size: 12px; line-height: 1.85; overflow-wrap: anywhere; white-space: pre-wrap; }
.log-context-note, .log-retention-note { color: #94907f; font-size: 11px; line-height: 1.8; margin: 14px 0; }
.log-detail-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin: 15px 0; color: #918c7a; font-size: 11px; }
.log-detail-list { padding: 0; margin: 0; list-style: none; display: grid; gap: 8px; }
.log-detail-entry { border: 1px solid #eeeadd; border-radius: 11px; padding: 12px 14px; }
.log-detail-entry > div { display: flex; flex-wrap: wrap; align-items: baseline; gap: 7px 12px; font-size: 10px; color: #839173; }
.log-detail-entry small { margin-left: auto; color: #758568; font-size: 10px; }
.log-detail-entry.is-selected { border-color: #c4ceb2; background: #f3f6ed; }
.log-detail-entry.is-issue { background: #fbf1e8; border-color: #ecdcc8; }
@media (max-width: 640px) { .log-selection, .log-detail-entry { padding: 12px; } .log-detail-toolbar .small-button { min-height: 40px; } }
</style>

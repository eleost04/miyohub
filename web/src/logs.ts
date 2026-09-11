import type { LogEntry } from './types'
import { formatDate } from './time'

export const logLabels: Record<string, string> = { task: '任务', games: '游戏签到', cloud: '云游戏', bbs: '米游币', captcha: '验证码识别', exchange: '兑换', scheduler: '调度', auth: '登录', config: '配置', push: '推送' }

export function displayLogMessage(log: LogEntry) {
  const legacy = '服务已接受通知（不代表终端已读）'
  return log.component === 'push' && log.message.endsWith(legacy) ? log.message.slice(0, -legacy.length) + '推送服务已接收通知' : log.message
}

export function isLogIssue(log: LogEntry) {
  // Successful totals contain the word "失败" too. Inspect the count before
  // looking for failure keywords; leave arbitrary error messages untouched.
  const totals = log.message.match(/(?:汇总|本次操作)[：:]\s*成功\s*\d+\s*[,，·]\s*失败\s*(\d+)\s*[,，·]\s*跳过\s*\d+\s*$/)
  if (totals) return Number(totals[1]) > 0
  if (/米游币任务状态查询已恢复（已自动重试 \d+ 次）$/.test(log.message)) return false
  return /失败|未完成|未确认|尚未确认|未增加|过期|不足|中断|错误|异常|无法|失效/.test(log.message)
}

export function logKey(log: LogEntry) {
  return JSON.stringify([log.at, log.component, log.message, log.account_id || '', log.run_id || ''])
}

export function keyedLogEntries(logs: LogEntry[]) {
  const occurrences = new Map<string, number>()
  return logs.map(entry => {
    const key = logKey(entry), occurrence = occurrences.get(key) || 0
    occurrences.set(key, occurrence + 1)
    return { entry, key: key + ':' + occurrence }
  })
}

export function logContext(logs: LogEntry[], selected: LogEntry) {
  if (selected.run_id && selected.account_id) {
    return { correlated: true, entries: logs.filter(log => log.run_id === selected.run_id && log.account_id === selected.account_id) }
  }
  // Legacy records have no stable account/run identity. Show a labelled local
  // context window instead of guessing from a display name or timestamp.
  const index = logs.findIndex(log => logKey(log) === logKey(selected))
  return { correlated: false, entries: index < 0 ? [selected] : logs.slice(Math.max(0, index - 20), index + 6) }
}

export function downloadLogs(logs: LogEntry[], timezone: string, filename = 'miyohub-logs.txt') {
  const body = logs.map(log => formatDate(log.at, timezone) + ' [' + (logLabels[log.component] || log.component) + '] ' + displayLogMessage(log)).join('\n')
  const url = URL.createObjectURL(new Blob([body], { type: 'text/plain;charset=utf-8' }))
  const link = document.createElement('a')
  link.href = url; link.download = filename; link.click()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}

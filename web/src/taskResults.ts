import type { TaskSummary } from './types'

export function alreadyComplete(result: TaskSummary) {
  return result.failed === 0 && (result.status === 'already_complete' || result.details?.includes('今日米游币任务已完成'))
}

export function resultLabel(result: TaskSummary) {
  if (alreadyComplete(result)) return '今日无待领取奖励 · 未重复执行'
  return `${result.success} 成功 · ${result.failed} 失败 · ${result.skipped} 未执行`
}

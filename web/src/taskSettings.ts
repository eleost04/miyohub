import type { Account, AccountTaskSettings } from './types'

export const games = [{ key: 'genshin', name: '原神', mark: '原' }, { key: 'starrail', name: '星穹铁道', mark: '轨' }, { key: 'zzz', name: '绝区零', mark: '零' }, { key: 'honkai3rd', name: '崩坏 3', mark: '崩' }, { key: 'tears', name: '未定事件簿', mark: '未' }, { key: 'honkai2', name: '崩坏学园 2', mark: '学' }]
export const forums = [{ id: 2, name: '原神' }, { id: 6, name: '星穹铁道' }, { id: 8, name: '绝区零' }, { id: 1, name: '崩坏 3' }, { id: 3, name: '崩坏学园 2' }, { id: 4, name: '未定事件簿' }, { id: 5, name: '大别野' }]
export function accountTasks(account: Account): AccountTaskSettings {
  return account.task_settings || { revision: 1, automatic: true, features: { game_checkin: true, cloud_game_checkin: false, bbs_tasks: false }, games: { enabled: ['genshin', 'starrail', 'zzz'], black_list: {} }, cloud_games: { enabled: ['genshin', 'zzz'] }, bbs: { forums: [5, 2], checkin: true, read: false, like: false, share: false, cancel_like: true, post_limit: 5, delay_seconds: [1, 3] } }
}
export function hasTasks(account: Account) {
  const t = accountTasks(account)
  return t.features.game_checkin && t.games.enabled.length > 0 || t.features.cloud_game_checkin && t.cloud_games.enabled.length > 0 || t.features.bbs_tasks && t.bbs.forums.length > 0 && (t.bbs.checkin || t.bbs.read || t.bbs.like || t.bbs.share)
}
export function taskNames(account: Account) {
  const t = accountTasks(account), names: string[] = []
  if (t.features.game_checkin) names.push(...games.filter(g => t.games.enabled.includes(g.key)).map(g => g.name))
  if (t.features.cloud_game_checkin) names.push(...t.cloud_games.enabled.map(g => g === 'genshin' ? '云·原神' : '云·绝区零'))
  if (t.features.bbs_tasks && t.bbs.forums.length && (t.bbs.checkin || t.bbs.read || t.bbs.like || t.bbs.share)) names.push('米游币')
  return names
}

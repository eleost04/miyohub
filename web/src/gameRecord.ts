export type RecordRole = { uid: string; region: string; nickname: string; region_name: string }
export type RecordMetric = { key: string; label: string; current: number | null; max: number | null; recovery_seconds?: number }
export type RecordSnapshot = {
  game: string; roles: RecordRole[] | null; role?: RecordRole
  note?: { metrics: RecordMetric[]; flags: { key: string; label: string; value: boolean | null }[] }
  calendar?: { events: CalendarEvent[]; skipped: number; server_time?: string }
  observed_at?: string; refresh_at?: string; cached: boolean; stale: boolean; status: string; message?: string
}
export const recordGames = [{ value: 'genshin', label: '原神' }, { value: 'starrail', label: '崩坏：星穹铁道' }, { value: 'zzz', label: '绝区零' }]
export const roleKey = (role: RecordRole) => role.uid + ':' + role.region
export type CalendarEvent = { id: string; title: string; kind: string; source: 'official' | 'manual'; start_at: string | null; end_at: string | null; finished: boolean | null }
export const calendarKinds = [{ value: 'version', label: '版本更新' }, { value: 'pool', label: '卡池' }, { value: 'activity', label: '限时活动' }, { value: 'challenge', label: '挑战' }]
export const calendarKindName = (kind: string) => calendarKinds.find(k => k.value === kind)?.label || '日程'

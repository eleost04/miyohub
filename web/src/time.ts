export function formatDate(value: string | number, zone = 'Asia/Shanghai') {
  if (!value || String(value).startsWith('0001-')) return '—'
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'medium', timeZone: zone }).format(new Date(typeof value === 'number' ? value * 1000 : value))
}
export function zonedInput(epoch: number, zone: string): string {
  if (!epoch) return ''
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone: zone, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' }).formatToParts(new Date(epoch * 1000))
  const p = Object.fromEntries(parts.map(p => [p.type, p.value]))
  return `${p.year}-${p.month}-${p.day}T${p.hour}:${p.minute}:${p.second}`
}
export function zonedEpoch(input: string, zone: string): number {
  if (!input) return 0
  const normalized = input.length === 16 ? input + ':00' : input
  const wall = Date.parse(normalized + 'Z') / 1000
  if (!Number.isFinite(wall)) throw new Error('日期格式无效')
  let epoch = wall
  for (let n = 0; n < 4; n++) {
    const represented = Date.parse(zonedInput(epoch, zone) + 'Z') / 1000
    if (represented === wall) return epoch
    epoch += wall - represented
  }
  throw new Error('该时区不存在此时间，请避开夏令时切换时段')
}

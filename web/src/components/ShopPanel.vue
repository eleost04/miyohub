<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { api } from '../api'
import { formatDate, zonedEpoch, zonedInput } from '../time'
import type { Config, ExchangePlan, ExchangeStatus, ShopGood, ShopResult, ShopRole, ShopAddress, User } from '../types'
import AppIcon from './AppIcon.vue'
import ModalShell from './ModalShell.vue'
import ChoiceSelect from './ChoiceSelect.vue'
import FloatingSave from './FloatingSave.vue'
import GoodImage from './GoodImage.vue'
import DateTimeField from './DateTimeField.vue'
import { registerBackLayer } from '../history'

const props = defineProps<{ config: Config; user: User }>()
const emit = defineEmits<{ changed: [] }>()
const zone = computed(() => props.config.schedule.timezone || 'Asia/Shanghai')
const goods = ref<ShopGood[]>([]), games = ref<ShopResult['games']>([]), game = ref(''), search = ref(''), availability = ref('')
const category = ref<1 | 2>(1)
const categories = [{ type: 1 as const, name: '实物商品', icon: 'gift' }, { type: 2 as const, name: '虚拟商品', icon: 'ticket' }]
const categoryGoods = computed(() => goods.value.filter(g => g.type === category.value))
const plans = ref<ExchangePlan[]>(props.config.shop_exchange.plans), schedule = ref<ExchangeStatus | null>(null), planFilter = ref('all')
const error = ref(''), notice = ref(''), loading = ref(false), busy = ref(false), detailsLoading = ref(false), now = ref(Date.now())
const selected = ref<ShopGood | null>(null), draft = ref<ExchangePlan | null>(null), planTime = ref('')
const addresses = ref<ShopAddress[]>([]), roles = ref<ShopRole[]>([]), balance = ref<number | null>(null)
const deliveryError = ref(''), confirmPlan = ref<ExchangePlan | null>(null), confirmAddress = ref(''), deleteID = ref('')
const life = new AbortController()
let catalogRequest: AbortController | undefined, detailRequest: AbortController | undefined, pollTimer: number | undefined, clockTimer: number | undefined, serverDelta = 0
const request = <T,>(path: string, init: RequestInit = {}) => api<T>(path, init, [life.signal])
const roleKey = computed({ get: () => draft.value ? draft.value.uid + '|' + draft.value.region : '|', set: value => { if (draft.value) [draft.value.uid, draft.value.region] = value.split('|') } })
const permitted = computed(() => props.user.role === 'admin' || !!props.user.permissions?.exchange)
const accountAllowed = (id: string) => { const a = props.config.accounts.find(a => a.id === id); return permitted.value && !!a && !a.disabled && (a.exchange_allowed ?? a.user_id === props.user.id) }
const exchangeAccounts = computed(() => props.config.accounts.filter(a => accountAllowed(a.id)))
const canSave = computed(() => props.config.shop_exchange.enable && !!draft.value?.account_id && accountAllowed(draft.value.account_id) && !detailsLoading.value && !deliveryError.value && balance.value !== null && balance.value >= (selected.value?.price || 0) && (!selected.value?.requires_address || !!draft.value.address_id) && (!selected.value?.requires_role || (!!draft.value.uid && !!draft.value.region)) && (!draft.value.auto || !!planTime.value))
const stateLabel: Record<string, string> = { pending: '待执行', running: '执行中', success: '兑换成功', failed: '未成功', unknown: '待确认', cancelled: '已停止', missed: '已错过' }
const phaseLabel: Record<string, string> = { preparing: '正在准备', waiting: '准备就绪', exchanging: '正在兑换' }
const goodLabel: Record<string, string> = { online: '可兑换', always: '随时兑换', ended: '已结束', scheduled: '即将开放', sold_out_with_next: '等待补货' }
const accountName = (id: string) => props.config.accounts.find(a => a.id === id)?.name || '账号已删除'
const online = (good: ShopGood) => ['online', 'always'].includes(good.display_status)
const active = (plan: ExchangePlan) => ['pending', 'running'].includes(plan.state)
const filteredGoods = computed(() => categoryGoods.value.filter(g => (!search.value || (g.goods_name + ' ' + g.goods_id).toLowerCase().includes(search.value.toLowerCase())) && (!availability.value || (availability.value === 'online' ? online(g) : ['scheduled', 'sold_out_with_next'].includes(g.display_status)))))
const filteredPlans = computed(() => [...plans.value].filter(p => planFilter.value === 'all' || (planFilter.value === 'active' ? active(p) : !active(p))).sort((a, b) => Number(active(b)) - Number(active(a)) || (a.exchange_at || Infinity) - (b.exchange_at || Infinity)))
const pendingCount = computed(() => plans.value.filter(p => p.enable && active(p)).length)
const clockLabel = computed(() => {
  const clock = schedule.value?.clock
  if (clock?.error) return clock.error
  if (!clock?.synced_at || clock.synced_at.startsWith('0001')) return '临近兑换时向米哈游校时'
  return clock.source === 'mihoyo_now_time' ? '米哈游时间 · 秒级校准' : '米哈游响应时间 · 秒级校准'
})
const clockDetail = computed(() => {
  const clock = schedule.value?.clock
  if (!clock?.source) return ''
  return `来源：${clock.source === 'mihoyo_now_time' ? '商品接口 now_time' : 'HTTP Date'}；网络往返 ${clock.rtt_ms} ms；估计误差 ±${clock.uncertainty_ms ?? 500} ms；校时于 ${formatDate(clock.synced_at, zone.value)}`
})
const fail = (e: unknown) => { if (!life.signal.aborted && !(e instanceof DOMException && e.name === 'AbortError')) error.value = e instanceof Error ? e.message : '请求失败' }
function countdown(epoch: number) {
  const seconds = Math.max(0, Math.ceil((epoch * 1000 - now.value - serverDelta) / 1000))
  if (!seconds) return '即将执行'
  const days = Math.floor(seconds / 86400), hours = Math.floor(seconds / 3600) % 24, minutes = Math.floor(seconds / 60) % 60
  return (days ? days + '天 ' : '') + String(hours).padStart(2, '0') + ':' + String(minutes).padStart(2, '0') + ':' + String(seconds % 60).padStart(2, '0')
}
watch(() => props.config.shop_exchange.plans, value => { plans.value = value })
async function loadGoods() {
  catalogRequest?.abort(); const controller = new AbortController(); catalogRequest = controller
  loading.value = true; error.value = ''
  try {
    const result = await request<ShopResult>('/api/v1/shop/goods?game=' + encodeURIComponent(game.value), { signal: controller.signal })
    if (!controller.signal.aborted) { goods.value = result.goods; if (result.games?.length) games.value = result.games }
  } catch (e) { fail(e) } finally { if (!controller.signal.aborted) loading.value = false }
}
async function poll() {
  try {
    const began = Date.now()
    const statusRequest = request<ExchangeStatus>('/api/v1/shop/status').then(value => { serverDelta = new Date(value.server_time).getTime() - (began + Date.now()) / 2; return value })
    const [nextPlans, nextStatus] = await Promise.all([request<ExchangePlan[]>('/api/v1/shop/plans'), statusRequest])
    const finished = nextPlans.some(p => plans.value.some(old => old.id === p.id && old.state === 'running') && p.state !== 'running')
    plans.value = nextPlans; schedule.value = nextStatus
    now.value = Date.now()
    if (finished) { notice.value = '兑换状态已更新，可在计划中查看结果。'; emit('changed') }
  } catch (e) { fail(e) }
  finally { if (!life.signal.aborted) pollTimer = window.setTimeout(poll, document.hidden ? 15000 : plans.value.some(p => p.state === 'running') ? 1500 : 5000) }
}
async function openGood(id: string, edit?: ExchangePlan, copy = false) {
  detailRequest?.abort(); const controller = new AbortController(); detailRequest = controller
  error.value = ''; busy.value = true
  try {
    const good = await request<ShopGood>('/api/v1/shop/good-detail?goods_id=' + encodeURIComponent(id), { signal: controller.signal })
    if (controller.signal.aborted) return
    selected.value = good; draft.value = null
    if (edit && good.display_status !== 'ended') startDraft(edit, copy)
  } catch (e) { fail(e) } finally { if (!controller.signal.aborted) busy.value = false }
}
function startDraft(edit?: ExchangePlan, copy = false) {
  if (!permitted.value || !props.config.shop_exchange.enable || edit && !accountAllowed(edit.account_id)) { error.value = '该账号尚未获得商品兑换权限，或站点兑换服务已暂停。'; return }
  const good = selected.value!, future = good.exchange_timestamp * 1000 > Date.now() + serverDelta
  const base: ExchangePlan = { id: '', revision: 0, state: 'pending', attempt: 0, price: good.price, goods_type: good.type, enable: true, auto: future, account_id: exchangeAccounts.value[0]?.id || '', goods_id: good.goods_id, goods_name: good.goods_name, device_fp: '', uid: '', region: '', game_biz: good.game_biz, address_id: '', exchange_at: future ? good.exchange_timestamp : 0, last_run: '', last_result: '' }
  draft.value = edit && !copy ? { ...edit, price: good.price, goods_type: good.type, goods_name: good.goods_name } : edit ? { ...base, account_id: edit.account_id, address_id: edit.address_id, uid: edit.uid, region: edit.region } : base
  planTime.value = zonedInput(draft.value.exchange_at, zone.value)
}
watch(() => [draft.value?.account_id, draft.value?.goods_id], ([id], _old, onCleanup) => {
  addresses.value = []; roles.value = []; balance.value = null; deliveryError.value = ''; detailsLoading.value = false
  if (!id || !selected.value || !accountAllowed(id)) return
  const controller = new AbortController(); onCleanup(() => controller.abort())
  detailsLoading.value = true
  const good = selected.value, q = '?account_id=' + encodeURIComponent(id)
  Promise.all([
    request<{ point?: number; points?: number }>('/api/v1/shop/points' + q, { signal: controller.signal }),
    good.requires_address ? request<ShopAddress[]>('/api/v1/shop/addresses' + q, { signal: controller.signal }) : Promise.resolve([]),
    good.requires_role ? request<ShopRole[]>('/api/v1/shop/roles' + q + '&game_biz=' + encodeURIComponent(good.game_biz), { signal: controller.signal }) : Promise.resolve([]),
  ]).then(([points, nextAddresses, nextRoles]) => {
    if (controller.signal.aborted || !draft.value) return
    balance.value = Number(points.points ?? points.point)
    if (!Number.isFinite(balance.value)) throw new Error('未能读取米游币余额')
    addresses.value = nextAddresses; roles.value = nextRoles
    if (!nextAddresses.some(a => a.id === draft.value!.address_id)) draft.value.address_id = nextAddresses.length === 1 ? nextAddresses[0]!.id : ''
    if (!nextRoles.some(r => r.uid === draft.value!.uid && r.region === draft.value!.region)) { draft.value.uid = nextRoles.length === 1 ? nextRoles[0]!.uid : ''; draft.value.region = nextRoles.length === 1 ? nextRoles[0]!.region : '' }
    if (good.requires_address && !nextAddresses.length) deliveryError.value = '该账号暂无收货地址，请先在米游社添加。'
    if (good.requires_role && !nextRoles.length) deliveryError.value = '该账号没有绑定对应的游戏角色。'
  }).catch(e => { if (!controller.signal.aborted) deliveryError.value = e instanceof Error ? e.message : '账号资料加载失败' }).finally(() => { if (!controller.signal.aborted) detailsLoading.value = false })
})
function close() { if (!busy.value) { selected.value = null; draft.value = null } }
watch(() => !!draft.value, (editing, _previous, onCleanup) => {
  let active = true, release: (() => void) | undefined
  if (editing) void nextTick(() => { if (active) release = registerBackLayer(() => { if (!busy.value) draft.value = null }) })
  onCleanup(() => { active = false; release?.() })
})
async function save(run = false) {
  if (!draft.value || !canSave.value) return
  busy.value = true; error.value = ''
  try {
    const payload = { ...draft.value, exchange_at: draft.value.auto ? zonedEpoch(planTime.value, zone.value) : 0 }
    if (run) { payload.auto = false; payload.exchange_at = 0 }
    if (payload.auto && payload.exchange_at * 1000 <= Date.now() + serverDelta) throw new Error('请选择未来的自动兑换时间')
    const result = await request<ExchangePlan>('/api/v1/shop/plans', { method: payload.id ? 'PUT' : 'POST', body: JSON.stringify(payload) })
    plans.value = [...plans.value.filter(p => p.id !== result.id), result]
    draft.value = null; selected.value = null; notice.value = result.auto ? '预约已保存，将在兑换前自动准备。' : '计划已保存，随时可以开始兑换。'; emit('changed')
    if (run) await confirm(result)
  } catch (e) { fail(e) } finally { busy.value = false }
}
async function confirm(plan: ExchangePlan) {
  if (!accountAllowed(plan.account_id)) { error.value = '该账号尚未获得商品兑换权限，请联系管理员开通。'; return }
  error.value = ''; busy.value = true
  try {
    confirmAddress.value = ''
    if (plan.address_id) {
      const items = await request<ShopAddress[]>('/api/v1/shop/addresses?account_id=' + encodeURIComponent(plan.account_id))
      const address = items.find(a => a.id === plan.address_id)
      if (!address) throw new Error('收货地址已失效，请编辑计划重新选择。')
      confirmAddress.value = address.name + ' · ' + address.phone + ' · ' + address.address
    }
    confirmPlan.value = plan
  } catch (e) { fail(e) } finally { busy.value = false }
}
async function runConfirmed() {
  if (!confirmPlan.value || busy.value || !accountAllowed(confirmPlan.value.account_id)) return
  busy.value = true; error.value = ''
  try {
    const p = await request<ExchangePlan>('/api/v1/shop/plans/run', { method: 'POST', body: JSON.stringify({ id: confirmPlan.value.id }) })
    plans.value = plans.value.map(item => item.id === p.id ? p : item); confirmPlan.value = null; notice.value = '兑换已启动，结果会自动更新。'
    window.clearTimeout(pollTimer); pollTimer = window.setTimeout(poll, 500)
  } catch (e) { fail(e) } finally { busy.value = false }
}
async function remove(p: ExchangePlan) {
  if (deleteID.value !== p.id) { deleteID.value = p.id; return }
  busy.value = true
  try { await request('/api/v1/shop/plans?id=' + encodeURIComponent(p.id), { method: 'DELETE' }); plans.value = plans.value.filter(item => item.id !== p.id); deleteID.value = ''; emit('changed') } catch (e) { fail(e) } finally { busy.value = false }
}
async function cancel(p: ExchangePlan) {
  try { await request('/api/v1/shop/plans/cancel', { method: 'POST', body: JSON.stringify({ id: p.id }) }); notice.value = '已请求停止，正在确认最终结果。' } catch (e) { fail(e) }
}
onMounted(() => { void loadGoods(); void poll(); clockTimer = window.setInterval(() => { now.value = Date.now() }, 1000) })
onUnmounted(() => { life.abort(); catalogRequest?.abort(); detailRequest?.abort(); window.clearTimeout(pollTimer); window.clearInterval(clockTimer) })
</script>

<template>
  <section class="shop-intro"><div><p class="eyebrow">米游币商城</p><h2>浏览商品与管理预约</h2><p class="muted">支持立即兑换或定时预约，收货信息按商品要求填写。</p></div><div class="next-exchange"><span><AppIcon name="clock" :size="15" />下一次兑换</span><strong>{{ !config.shop_exchange.enable ? '兑换已暂停' : schedule?.enabled && schedule.next_run ? countdown(schedule.next_run) : '暂无定时计划' }}</strong><small :title="clockDetail">{{ clockLabel }}</small></div></section>
  <p v-if="error && !selected && !confirmPlan" class="error-banner" role="alert">{{ error }}</p><p v-if="notice" class="notice" role="status">{{ notice }}</p>
  <p v-if="!config.shop_exchange.enable" class="error-banner">管理员已暂停商品兑换，可以继续浏览商品。</p>
  <p v-if="!permitted" class="scope-note"><AppIcon name="lock" :size="19" /><span><strong>商品兑换需要管理员授权</strong><br />你可以浏览商品、查看历史记录。创建、修改、预约和执行兑换计划需联系管理员开通，不影响日常签到。</span></p>
  <article class="panel shop-panel">
    <div class="panel-title"><div><p class="eyebrow">商品列表</p><h2>店铺商品 <span class="count-label">{{ filteredGoods.length }}</span></h2></div><button class="small-button" :disabled="loading" @click="loadGoods"><AppIcon name="refresh" :size="14" />{{ loading ? '更新中…' : '刷新商品' }}</button></div>
    <div class="goods-categories" aria-label="商品类型"><button v-for="item in categories" :key="item.type" type="button" :aria-pressed="category === item.type" :class="{ active: category === item.type }" @click="category = item.type"><AppIcon :name="item.icon" :size="18" /><span>{{ item.name }}</span><small>{{ goods.filter(g => g.type === item.type).length }}</small></button></div>
    <div class="shop-toolbar"><label class="search-field"><AppIcon name="search" :size="16" /><input v-model="search" type="search" aria-label="搜索商品" placeholder="搜索当前分类的商品…" /></label><ChoiceSelect v-model="game" label="商品分区" :options="[{ value: '', label: '所有游戏与米游社' }, ...games.filter(g => g.key !== 'all').map(g => ({ value: g.key, label: g.name }))]" @change="loadGoods" /><ChoiceSelect v-model="availability" label="商品状态" :options="[{ value: '', label: '全部状态' }, { value: 'online', label: '现在可兑换' }, { value: 'upcoming', label: '即将开放 / 补货' }]" /></div>
    <div v-if="loading && !goods.length" class="goods-grid" aria-label="正在加载商品"><div v-for="n in 4" :key="n" class="good-skeleton skeleton"></div></div>
    <div v-else-if="filteredGoods.length" class="goods-grid">
      <article v-for="good in filteredGoods" :key="good.goods_id" class="good-card"><GoodImage :src="good.icon" :name="good.goods_name" /><div class="good-body"><span class="pill" :class="{ soft: online(good) }" :data-status="good.display_status">{{ goodLabel[good.display_status] || '待开放' }}</span><h3>{{ good.goods_name }}</h3><p class="good-price">{{ good.price.toLocaleString() }}<small>米游币</small></p><p class="good-meta">库存 {{ good.stock }} · {{ good.limit }}</p><p class="good-time">{{ good.time_needs_detail ? '开放时间待详情确认' : good.exchange_timestamp ? formatDate(good.exchange_timestamp, zone) : good.exchange_time }}</p><button class="small-button" :disabled="busy" @click="openGood(good.goods_id)">查看详情<AppIcon name="arrow" :size="13" /></button></div></article>
    </div>
    <div v-else class="empty rich-empty"><AppIcon :name="category === 1 ? 'gift' : 'ticket'" :size="34" /><p>{{ categoryGoods.length ? '没有匹配的商品' : category === 1 ? '当前分区暂无实物商品' : '当前分区暂无虚拟商品' }}</p><small>可切换商品类型、游戏分区或调整筛选条件。</small></div>
  </article>

  <article class="panel plans-panel">
    <div class="panel-title"><div><p class="eyebrow">预约与记录</p><h2>兑换计划 <span class="count-label">{{ pendingCount }} 待执行</span></h2></div><ChoiceSelect v-model="planFilter" label="筛选兑换计划" :options="[{ value: 'all', label: '全部计划' }, { value: 'active', label: '待执行 / 进行中' }, { value: 'history', label: '历史结果' }]" /></div>
    <div class="plan-list"><article v-for="p in filteredPlans" :key="p.id" class="exchange-plan" :data-state="p.state">
      <div class="plan-heading"><span class="plan-icon"><AppIcon :name="p.state === 'success' ? 'check' : p.auto ? 'clock' : 'gift'" :size="20" /></span><div class="plan-description"><strong>{{ p.goods_name }}</strong><small>{{ accountName(p.account_id) }} · {{ p.price.toLocaleString() }} 米游币</small></div><span class="pill" :class="{ soft: p.state === 'success', warning: ['failed', 'unknown', 'missed'].includes(p.state) }">{{ !p.enable ? '已停用' : p.state === 'running' ? phaseLabel[p.phase || ''] || '执行中' : stateLabel[p.state] }}</span></div>
      <div class="plan-status-line"><span>{{ p.auto ? formatDate(p.exchange_at, zone) : '手动执行' }}</span><span v-if="p.auto && p.enable && active(p) && accountAllowed(p.account_id) && config.shop_exchange.enable" class="plan-countdown">{{ countdown(p.exchange_at) }}</span></div>
      <div v-if="p.state === 'running'" class="exchange-steps"><span :class="{ done: p.phase !== 'preparing', current: p.phase === 'preparing' }">准备资料</span><i></i><span :class="{ done: p.phase === 'exchanging', current: p.phase === 'waiting' }">等待开兑</span><i></i><span :class="{ current: p.phase === 'exchanging' }">发送兑换</span></div>
      <p class="plan-result">{{ p.last_result || '暂无执行结果' }}<span v-if="p.attempt"> · 已请求 {{ p.attempt }} 次</span></p>
      <p v-if="p.state === 'unknown'" class="error-text">请先在米游社核对兑换记录，确认结果后再决定是否新建计划。</p>
      <p v-if="p.state === 'pending' && !accountAllowed(p.account_id)" class="error-text">所属用户未获兑换授权或账号已停用，计划不会自动执行。</p>
      <div class="good-actions"><template v-if="p.state === 'pending'"><button class="small-button primary-mini" :disabled="busy || !p.enable || !config.shop_exchange.enable || !accountAllowed(p.account_id)" @click="confirm(p)"><AppIcon name="play" :size="13" />立即兑换</button><button class="small-button" :disabled="busy || !accountAllowed(p.account_id)" @click="openGood(p.goods_id, p)">编辑计划</button></template><button v-if="p.state === 'running'" class="small-button" @click="cancel(p)"><AppIcon name="stop" :size="13" />停止执行</button><template v-else><button v-if="p.state !== 'pending' && p.state !== 'unknown'" class="small-button" :disabled="busy || !accountAllowed(p.account_id)" @click="openGood(p.goods_id, p, true)">重新建计划</button><button class="text-button" :class="{ 'error-ink': deleteID === p.id }" :disabled="busy" @click="remove(p)">{{ deleteID === p.id ? '确认删除计划' : '删除' }}</button><button v-if="deleteID === p.id" class="text-button" @click="deleteID = ''">取消</button></template></div>
    </article></div>
    <div v-if="!filteredPlans.length" class="empty rich-empty"><AppIcon name="clock" :size="32" /><p>{{ plans.length ? '没有符合筛选条件的计划' : '暂无兑换计划' }}</p><small>选择商品后，可创建手动兑换计划或定时预约。</small></div>
  </article>

  <ModalShell v-if="selected" :title="selected.goods_name" eyebrow="商品详情" :busy="busy" wide @close="close">
    <div class="good-preview"><GoodImage :src="selected.icon" :name="selected.goods_name" loading="eager" /><div><span class="pill" :class="{ soft: online(selected) }">{{ goodLabel[selected.display_status] || '待开放' }}</span><p class="good-price">{{ selected.price.toLocaleString() }}<small>米游币</small></p><p class="muted">库存 {{ selected.stock }} · {{ selected.limit }}</p></div></div>
    <p class="muted">{{ selected.exchange_timestamp ? formatDate(selected.exchange_timestamp, zone) : selected.exchange_time }}</p>
    <form v-if="draft" class="plan-form" @submit.prevent="save()">
      <FloatingSave :label="draft.auto ? '保存预约' : '保存计划'" :busy="busy" :disabled="!canSave" />
      <label>兑换账号<ChoiceSelect v-model="draft.account_id" label="兑换账号" :disabled="busy" searchable :options="[{ value: '', label: '选择账号' }, ...config.accounts.map(a => ({ value: a.id, label: a.name, description: a.disabled ? '账号已停用' : accountAllowed(a.id) ? '已授权兑换' : '所属用户尚未获兑换授权', disabled: !accountAllowed(a.id) }))]" /></label>
      <div class="balance-line"><span>可用米游币</span><strong>{{ detailsLoading ? '读取中…' : balance === null ? '—' : balance.toLocaleString() }}</strong></div>
      <p v-if="balance !== null && balance < selected.price" class="error-text">还差 {{ selected.price - balance }} 米游币，余额不足，暂不能保存或执行兑换。</p><p v-if="deliveryError" class="error-text" role="alert">{{ deliveryError }}</p>
      <label v-if="selected.requires_address">收货地址<ChoiceSelect v-model="draft.address_id" label="收货地址" :disabled="busy || detailsLoading" searchable :options="[{ value: '', label: '请选择地址' }, ...addresses.map(a => ({ value: a.id, label: a.name + ' · ' + a.phone, description: a.address }))]" /></label>
      <label v-if="selected.requires_role">接收奖励的角色<ChoiceSelect v-model="roleKey" label="接收奖励的角色" :disabled="busy || detailsLoading" searchable :options="[{ value: '|', label: '请选择角色' }, ...roles.map(r => ({ value: r.uid + '|' + r.region, label: r.nickname || r.uid, description: (r.region_name || r.region) + ' · UID ' + r.uid }))]" /></label>
      <div class="subtle-card"><label class="check-row"><input v-model="draft.auto" type="checkbox" :disabled="busy" />预约时间，到点自动兑换</label><label v-if="draft.auto">兑换时间（{{ zone }}）<DateTimeField v-model="planTime" label="兑换时间" :timezone="zone" :disabled="busy" seconds /></label><p v-if="draft.auto" class="muted">提前校时和准备资料，预留秒级校时误差后开始。{{ config.shop_exchange.retry_seconds > 0 ? `在计划时间起 ${config.shop_exchange.retry_seconds} 秒内持续尝试；准备和排队不延长窗口。` : '当前设置只请求一次，不自动重试。' }}成功、售罄或需人工处理时提前停止。</p><p v-else class="muted">保存计划后，由你决定何时开始兑换。</p><label class="check-row"><input v-model="draft.enable" type="checkbox" :disabled="busy" />启用此计划</label></div>
      <div class="modal-actions"><button data-save-inline class="button primary" :disabled="busy || !canSave">{{ busy ? '保存中…' : draft.auto ? '保存预约' : '保存计划' }}</button><button v-if="!draft.id && !draft.auto && online(selected)" type="button" class="small-button" :disabled="busy || !canSave || !draft.enable || !config.shop_exchange.enable" @click="save(true)">直接兑换<AppIcon name="arrow" :size="14" /></button></div>
    </form>
    <div v-else><button class="button primary wide" :disabled="!exchangeAccounts.length || !config.shop_exchange.enable || selected.display_status === 'ended'" @click="startDraft()">{{ !permitted ? '需管理员开通兑换权限' : online(selected) ? '选择账号与兑换方式' : '预约这件商品' }}<AppIcon name="arrow" /></button><p v-if="!config.accounts.length" class="muted">先在任务总览中绑定账号，获得兑换授权后即可创建计划。</p></div>
    <p v-if="error" class="error-banner" role="alert">{{ error }}</p>
  </ModalShell>

  <ModalShell v-if="confirmPlan" title="确认兑换" :busy="busy" @close="confirmPlan = null">
    <div class="confirmation-gift"><AppIcon name="gift" :size="36" /><h3>{{ confirmPlan.goods_name }}</h3><strong>{{ confirmPlan.price.toLocaleString() }}<small>米游币</small></strong></div>
    <div class="confirmation-details"><p><span>兑换账号</span><strong>{{ accountName(confirmPlan.account_id) }}</strong></p><p v-if="confirmPlan.uid"><span>接收角色</span><strong>{{ confirmPlan.uid }} · {{ confirmPlan.region }}</strong></p><p v-if="confirmAddress"><span>收货信息</span><strong>{{ confirmAddress }}</strong></p></div><p class="muted">确认后提交兑换请求；兑换成功后由米游社扣除对应米游币。</p>
    <p v-if="error" class="error-banner" role="alert">{{ error }}</p><div class="modal-actions"><button class="button primary" :disabled="busy || !config.shop_exchange.enable || !accountAllowed(confirmPlan.account_id)" @click="runConfirmed">{{ busy ? '正在提交…' : '确认兑换' }}<AppIcon name="check" /></button><button class="small-button" :disabled="busy" @click="confirmPlan = null">取消</button></div>
  </ModalShell>
</template>
<style scoped>
.goods-categories{display:flex;gap:8px;padding:5px;margin:0 0 18px;border:1px solid #e9e3d6;border-radius:15px;background:#f4f2e9;width:fit-content;max-width:100%}.goods-categories button{display:flex;align-items:center;justify-content:center;gap:9px;padding:10px 18px;border:0;border-radius:10px;background:transparent;color:#9a957f;font-size:13px;cursor:pointer;min-width:0}.goods-categories button.active{background:#fffefa;color:#60734e;box-shadow:0 2px 6px #54452208}.goods-categories small{font-size:10px;opacity:.75;font-variant-numeric:tabular-nums}@media(max-width:640px){.goods-categories{width:100%;display:grid;grid-template-columns:1fr 1fr}.goods-categories button{padding:10px 8px;gap:6px}}
</style>

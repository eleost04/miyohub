<script setup lang="ts">
import { computed, defineAsyncComponent, ref } from 'vue'
import type { Account, Config, User } from '../types'
import { accountTasks, taskNames } from '../taskSettings'
import AppIcon from './AppIcon.vue'
import ModalShell from './ModalShell.vue'
import PageLoading from './PageLoading.vue'
import PageLoadError from './PageLoadError.vue'
const lazy = { loadingComponent: PageLoading, errorComponent: PageLoadError, delay: 150, timeout: 30000 }
const BindAccountModal = defineAsyncComponent({ ...lazy, loader: () => import('./BindAccountModal.vue') })
const AccountTasksModal = defineAsyncComponent({ ...lazy, loader: () => import('./AccountTasksModal.vue') })
const CaptchaPanel = defineAsyncComponent({ ...lazy, loader: () => import('./CaptchaPanel.vue') })
const PushPanel = defineAsyncComponent({ ...lazy, loader: () => import('./PushPanel.vue') })
const props = defineProps<{ user: User; config: Config }>()
const emit = defineEmits<{ changed: []; done: [status: 'dismissed' | 'complete'] }>()
const steps = [{ name: '绑定账号', icon: 'scan' }, { name: '签到设置', icon: 'check' }, { name: '打码服务', icon: 'shield' }, { name: '消息推送', icon: 'bell' }]
const step = ref(0), binding = ref<'' | 'qr' | 'sms'>(''), editing = ref<Account | null>(null), busy = ref(false), discard = ref(false)
const captcha = ref<InstanceType<typeof CaptchaPanel> | null>(null), push = ref<InstanceType<typeof PushPanel> | null>(null)
const owned = computed(() => props.config.accounts.filter(a => a.user_id === props.user.id))
const zone = computed(() => props.config.schedule.timezone || 'Asia/Shanghai')
function panel() { return step.value === 2 ? captcha.value : step.value === 3 ? push.value : null }
async function flush() { const current = panel(); return !current?.dirty || await current.flush() }
async function advance(value: number) {
  if (busy.value) return
  busy.value = true
  try { if (await flush()) { if (value >= steps.length) emit('done', 'complete'); else step.value = value } }
  finally { busy.value = false }
}
async function close() {
  if (busy.value) return
  busy.value = true
  try { if (await flush()) emit('done', 'dismissed'); else discard.value = true }
  finally { busy.value = false }
}
function bound() { step.value = 1; emit('changed') }
</script>
<template>
  <ModalShell title="快速配置" :busy="busy" :hidden="!!binding || !!editing" wide @close="close">
    <p class="muted guide-note">按需完成以下设置，也可以直接关闭。之后可从「个人账号」重新打开；关闭引导不会关闭已配置的服务。</p>
    <nav class="guide-steps" aria-label="配置步骤"><button v-for="(item, index) in steps" :key="item.name" type="button" :class="{ current: step === index, visited: step > index }" :aria-current="step === index ? 'step' : undefined" :disabled="busy" @click="advance(index)"><span><AppIcon :name="item.icon" :size="17" /></span><small>{{ item.name }}</small></button></nav>
    <section v-if="step === 0" class="guide-section">
      <h3>绑定你的米游社账号</h3><p class="muted">当前登录的是站点账号。绑定米游社账号后，才能为它设置签到与兑换任务。</p>
      <div class="guide-choices"><button type="button" @click="binding = 'qr'"><AppIcon name="scan" :size="25" /><strong>扫码绑定</strong><small>使用米游社 APP 扫码确认</small><AppIcon name="arrow" :size="16" /></button><button type="button" @click="binding = 'sms'"><AppIcon name="phone" :size="25" /><strong>短信验证码绑定</strong><small>人机验证可在页面手动完成</small><AppIcon name="arrow" :size="16" /></button></div>
      <p class="scope-note">{{ owned.length ? `你已绑定 ${owned.length} 个米游社账号，可继续配置或再绑定其他账号。` : '此步骤可跳过，之后在「任务总览」添加账号。' }}</p>
    </section>
    <section v-else-if="step === 1" class="guide-section">
      <h3>选择每个账号的签到项目与时间</h3><p class="muted">游戏签到、云游戏与米游币任务分别开关；个人时间优先，未设置时使用站点默认时间。</p>
      <div v-if="owned.length" class="guide-accounts"><button v-for="account in owned" :key="account.id" type="button" :aria-label="'配置签到 ' + account.name" @click="editing = account"><span class="account-avatar">{{ account.name.slice(0, 1) }}</span><span><strong>{{ account.name }}</strong><small>{{ taskNames(account).join(' · ') || '尚未选择签到项目' }}</small><small>{{ !accountTasks(account).automatic ? '仅手动执行' : accountTasks(account).schedule ? `每天 ${accountTasks(account).schedule!.time} · ${accountTasks(account).schedule!.timezone}` : '跟随站点默认时间' }}</small></span><AppIcon name="settings" :size="19" /></button></div>
      <div v-else class="empty"><p>还没有绑定米游社账号。</p><button class="small-button" @click="advance(0)">返回绑定账号</button></div>
      <p class="muted">这里只保存配置，不会立即执行签到或兑换。</p>
    </section>
    <section v-else-if="step === 2" class="guide-service"><p class="guide-optional">可选 · 普通网络超时不需要打码。只在米游社要求人机验证时使用。</p><CaptchaPanel ref="captcha" :user="user" /></section>
    <section v-else class="guide-service"><p class="guide-optional">可选 · 配置接收渠道后，可开启自己的签到与兑换结果通知。不会发送其他用户的结果。</p><PushPanel ref="push" :timezone="zone" /></section>
    <div v-if="discard" class="discard-inline" role="alert"><p>当前有未保存的配置。可以返回完成保存，或放弃这些修改并关闭引导。</p><button class="small-button" @click="discard = false">返回配置</button><button class="text-button error-ink" @click="emit('done', 'dismissed')">放弃未保存项并关闭</button></div>
    <div class="modal-actions sticky-modal-actions guide-actions"><button v-if="step > 0" class="small-button" :disabled="busy" @click="advance(step - 1)">上一步</button><button class="text-button guide-skip" :disabled="busy" @click="close">暂时关闭</button><button class="button primary" :disabled="busy" @click="advance(step + 1)">{{ step === 3 ? '完成引导' : '下一步' }}<AppIcon :name="step === 3 ? 'check' : 'arrow'" :size="16" /></button></div>
  </ModalShell>
  <BindAccountModal v-if="binding" :account="null" :initial-tab="binding" @close="binding = ''" @changed="bound" />
  <AccountTasksModal v-if="editing" :account="editing" :schedule="config.schedule" @close="editing = null" @saved="emit('changed')" />
</template>
<style scoped>
.guide-note{margin-top:0}.guide-steps{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:8px;margin:23px 0}.guide-steps button{display:grid;justify-items:center;gap:8px;padding:10px 2px;border:0;border-radius:12px;background:transparent;color:#a4a08e;cursor:pointer}.guide-steps button>span{display:grid;place-items:center;width:32px;height:32px;border-radius:50%;background:#f0eee6}.guide-steps button.current{background:#eff2e8;color:#586b47}.guide-steps button.current>span{background:#6f8060;color:white}.guide-steps small{font-size:11px}.guide-section h3{font-size:17px;font-weight:500}.guide-choices{display:grid;grid-template-columns:1fr 1fr;gap:14px;margin:22px 0}.guide-choices button{display:grid;gap:12px;justify-items:start;text-align:left;min-width:0;padding:22px 18px;border:1px solid #e4dece;border-radius:16px;background:#faf9f3;color:#666f57;cursor:pointer}.guide-choices strong{font-size:14px;font-weight:500}.guide-choices small{font-size:11px;line-height:1.6;color:#95927f}.guide-accounts{display:grid;gap:10px;margin:20px 0}.guide-accounts button{display:flex;align-items:center;gap:13px;padding:15px;border:1px solid #e4dece;border-radius:14px;background:#fbfaf6;color:#626c54;text-align:left;cursor:pointer}.guide-accounts button>span:nth-child(2){display:grid;flex:1;gap:7px;min-width:0}.guide-accounts strong{font-size:13px;font-weight:500}.guide-accounts small{font-size:11px;color:#97937f;overflow-wrap:anywhere}.guide-optional{font-size:12px;color:#92917e;line-height:1.8}.guide-service:deep(.panel){border:0;padding:0;box-shadow:none}.guide-service:deep(.push-intro){margin-bottom:16px}.guide-actions{background:#fffefa;z-index:2}.guide-skip{margin-right:auto}.guide-actions .button{min-width:110px}@media(max-width:640px){.guide-choices{gap:10px}.guide-choices button{padding:18px 13px}.guide-service:deep(.push-options){grid-template-columns:1fr}.guide-actions{gap:12px}.guide-actions .button{min-width:100px}.guide-skip{font-size:11px}}
</style>

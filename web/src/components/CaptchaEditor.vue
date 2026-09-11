<script setup lang="ts">
import { ref } from 'vue'
import type { CaptchaChannel, Config } from '../types'
import AppIcon from './AppIcon.vue'
import ModalShell from './ModalShell.vue'
import ChoiceSelect from './ChoiceSelect.vue'
import FloatingSave from './FloatingSave.vue'

const props = defineProps<{ modelValue: Config['captcha']; scope: 'site' | 'personal'; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: Config['captcha']] }>()
const editing = ref<CaptchaChannel | null>(null), originalID = ref('')
const providerOptions = [{ value: 'custom' as const, label: '自建 / 自定义服务', description: '兼容 test_nine 的 pass_nine / pass_uni 接口' }, { value: 'damagou' as const, label: '打码狗', description: '使用你提供的 UserKey，调用可能产生平台费用' }]
function update(channels: CaptchaChannel[]) { emit('update:modelValue', { ...props.modelValue, channels }) }
function edit(channel?: CaptchaChannel) {
  originalID.value = channel?.id || ''
  editing.value = channel ? JSON.parse(JSON.stringify(channel)) : { id: crypto.randomUUID(), provider: 'custom', enable: false, userkey: '', type: '', timeout: 60, endpoint: '', token: '', use_v3_model: true, configured: [] }
  if (editing.value && editing.value.use_v3_model === undefined) editing.value.use_v3_model = true
}
function providerChanged() {
  if (!editing.value) return
  Object.assign(editing.value, { id: crypto.randomUUID(), userkey: '', token: '', endpoint: '', type: '', configured: [], clear_token: false, use_v3_model: true })
}
function apply() {
  if (!editing.value) return
  const ch = JSON.parse(JSON.stringify(editing.value)) as CaptchaChannel
  const list = [...props.modelValue.channels], index = list.findIndex(c => c.id === originalID.value)
  if (index >= 0) list[index] = ch; else list.push(ch)
  update(list); editing.value = null
}
function toggle(channel: CaptchaChannel, event: Event) { update(props.modelValue.channels.map(c => c.id === channel.id ? { ...c, enable: (event.target as HTMLInputElement).checked } : c)) }
function move(index: number, direction: number) { const list = [...props.modelValue.channels], target = index + direction; if (target < 0 || target >= list.length) return; const ch = list.splice(index, 1)[0]; if (ch) list.splice(target, 0, ch); update(list) }
</script>

<template>
  <div class="captcha-editor">
    <div class="captcha-channel-list">
      <article v-for="(channel, index) in modelValue.channels" :key="channel.id" class="captcha-channel">
        <div class="captcha-channel-title"><span class="captcha-order">{{ index + 1 }}</span><strong>{{ channel.provider === 'custom' ? '自建 / 自定义服务' : '打码狗' }}</strong><label class="check-row"><input :checked="channel.enable" type="checkbox" :disabled="disabled" :aria-label="'启用验证码渠道 ' + (index + 1)" @change="toggle(channel, $event)" />启用</label></div>
        <p class="channel-endpoint">{{ channel.provider === 'custom' ? channel.endpoint || '尚未填写服务地址' : channel.configured?.includes('userkey') || channel.userkey ? 'UserKey 已配置' : '尚未填写 UserKey' }}</p>
        <div class="channel-summary"><span>{{ channel.timeout }} 秒超时</span><span v-if="channel.provider === 'custom'">{{ channel.use_v3_model === false ? '旧版模型' : 'V3 模型' }}</span><span v-if="channel.token || channel.configured?.includes('token')">{{ channel.clear_token ? '待清除鉴权' : '已配置鉴权' }}</span></div>
        <div class="captcha-actions"><button type="button" class="small-button" :disabled="disabled" :aria-label="'配置验证码渠道 ' + (index + 1)" @click="edit(channel)"><AppIcon name="settings" :size="14" />配置</button><button type="button" class="text-button" :disabled="disabled || index === 0" :aria-label="'上移验证码渠道 ' + (index + 1)" @click="move(index, -1)">上移</button><button type="button" class="text-button" :disabled="disabled || index === modelValue.channels.length - 1" :aria-label="'下移验证码渠道 ' + (index + 1)" @click="move(index, 1)">下移</button><button type="button" class="text-button error-ink" :disabled="disabled" :aria-label="'移除验证码渠道 ' + (index + 1)" @click="update(modelValue.channels.filter(c => c.id !== channel.id))"><AppIcon name="trash" :size="14" />移除</button></div>
      </article>
      <div v-if="!modelValue.channels.length" class="empty rich-empty"><AppIcon name="shield" :size="28" /><p>还没有配置打码渠道</p><small>添加服务地址或打码狗密钥，验证码出现时才会调用。</small></div>
    </div>
    <div class="captcha-add"><button type="button" class="small-button primary-mini" :disabled="disabled || modelValue.channels.length >= 10" @click="edit()"><AppIcon name="plus" :size="14" />添加验证码渠道</button><small class="muted">{{ modelValue.channels.length }} / 10 · 按顺序尝试，成功后停止</small></div>
    <p class="captcha-footnote">{{ scope === 'personal' ? '个人渠道仅接受公网 URL。Docker 服务名、回环或内网地址请通过管理员授权的站点服务使用。' : '站点渠道可访问内网。在 Docker 中请使用同网络服务名，如 miyohub-captcha:9645。' }}密钥仅保存在服务器，保存后不回传浏览器。</p>
  </div>
  <ModalShell v-if="editing" :title="originalID ? '配置验证码渠道' : '添加验证码渠道'" wide @close="editing = null">
    <form class="channel-editor-form" @submit.prevent.stop="apply">
      <FloatingSave label="应用渠道配置" text="应用" />
      <div class="form-grid"><label>渠道类型<ChoiceSelect v-model="editing.provider" label="验证码渠道类型" :options="providerOptions" @change="providerChanged" /></label><label>超时（秒）<input v-model.number="editing.timeout" type="number" min="1" max="120" required /></label></div>
      <template v-if="editing.provider === 'custom'">
        <label>接口地址<input v-model.trim="editing.endpoint" type="url" :required="editing.enable" maxlength="4096" :placeholder="scope === 'site' ? 'http://miyohub-captcha:9645/pass_nine' : 'https://your-service.example/pass_nine'" spellcheck="false" /></label>
        <p class="muted">填完整接口地址，不含查询参数。自动传入 gt、challenge、use_v3_model；只传验证码，不发送账号凭据。</p>
        <div class="form-grid"><label>Bearer Token（可选）<small v-if="editing.configured?.includes('token') && !editing.clear_token" class="push-configured">已配置</small><input v-model="editing.token" type="password" :disabled="editing.clear_token" autocomplete="new-password" maxlength="4096" placeholder="留空保留已保存值" /></label><label>九宫格模型<ChoiceSelect :model-value="editing.use_v3_model !== false" label="九宫格模型" :options="[{ value: true, label: 'V3 模型（推荐）' }, { value: false, label: '旧版模型' }]" @update:model-value="editing.use_v3_model = $event" /></label></div>
        <label v-if="editing.configured?.includes('token')" class="check-row"><input v-model="editing.clear_token" type="checkbox" />保存时清除已配置 Token</label>
        <p class="captcha-contract">响应格式：<code>data.result = success</code> 且 <code>data.validate</code> 非空。更换服务地址不会沿用原 Token。</p>
      </template>
      <template v-else><label>打码狗 UserKey <small v-if="editing.configured?.includes('userkey')" class="push-configured">已配置</small><input v-model="editing.userkey" type="password" autocomplete="new-password" :required="editing.enable && !editing.configured?.includes('userkey')" maxlength="4096" placeholder="留空保留已保存值" /></label><label>识别类型（可选）<input v-model="editing.type" maxlength="128" placeholder="使用平台默认类型" /></label><p class="muted">使用你的打码狗账户余额；保存配置不会发起付费识别。</p></template>
      <label class="check-row"><input v-model="editing.enable" type="checkbox" />启用这个渠道</label>
      <div class="modal-actions sticky-modal-actions"><button data-save-inline class="button primary">应用渠道配置<AppIcon name="check" :size="16" /></button><button type="button" class="small-button" @click="editing = null">取消</button></div><p class="muted">应用到页面后，按页面的自动保存开关保存；关闭自动保存时，请手动保存页面配置。</p>
    </form>
  </ModalShell>
</template>

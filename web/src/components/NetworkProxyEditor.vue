<script setup lang="ts">
import { computed, ref } from 'vue'
import type { ProxySettings } from '../types'
import ModalShell from './ModalShell.vue'
import AppIcon from './AppIcon.vue'
import FloatingSave from './FloatingSave.vue'

const props = defineProps<{ modelValue: ProxySettings; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: ProxySettings] }>()
const editing = ref<ProxySettings | null>(null), error = ref('')
const summary = computed(() => {
  if (!props.modelValue.enable) return '未开启 · 米游社接口直连'
  try { const url = new URL(props.modelValue.url); return url.protocol.replace(':', '').toUpperCase() + ' · ' + url.host }
  catch { return '代理地址待配置' }
})
function open() { editing.value = JSON.parse(JSON.stringify(props.modelValue)); error.value = '' }
function apply() {
  const next = editing.value
  if (!next) return
  next.url = next.url.trim(); next.username = next.username.trim()
  try {
    if (next.url) {
      const url = new URL(next.url)
      if (!['http:', 'https:', 'socks5:', 'socks5h:'].includes(url.protocol) || !url.hostname || url.username || url.password || url.search || url.hash || !['', '/'].includes(url.pathname)) throw new Error()
    } else if (next.enable) throw new Error()
  } catch { error.value = '请填写 http(s) 或 socks5(h)://主机:端口，用户名和密码放在独立字段中。'; return }
  emit('update:modelValue', { ...next }); editing.value = null
}
</script>

<template>
  <div class="proxy-summary"><div><strong>米游社出站代理</strong><p class="muted">{{ summary }}</p></div><button type="button" class="small-button" :disabled="disabled" @click="open"><AppIcon name="settings" :size="15" />配置代理</button></div>
  <ModalShell v-if="editing" title="米游社出站代理" @close="editing = null">
    <form class="channel-editor-form" @submit.prevent.stop="apply">
      <FloatingSave label="应用代理配置" text="应用" />
      <p class="muted">用于米游社登录、游戏/云游戏签到、米游币与商品兑换。个人打码链接、站点打码服务、推送和浏览器图片不经过此代理。</p>
      <label class="check-row"><input v-model="editing.enable" type="checkbox" />启用米游社代理</label>
      <label>代理地址<input v-model.trim="editing.url" type="url" :required="editing.enable" maxlength="2048" placeholder="socks5://proxy.example:1080" autocomplete="off" spellcheck="false" /></label>
      <p class="muted">支持 HTTP、HTTPS、SOCKS5 和 SOCKS5H。SOCKS5 由代理解析目标域名。Docker 内的 127.0.0.1 指向容器自身，请使用容器能访问的代理地址。</p>
      <div class="form-grid"><label>代理用户名（可选）<input v-model="editing.username" maxlength="255" autocomplete="off" /></label><label>代理密码（可选）<small v-if="editing.has_password && !editing.clear_password" class="push-configured">已配置</small><input v-model="editing.password" type="password" :disabled="editing.clear_password" autocomplete="new-password" maxlength="255" placeholder="留空保留已保存密码" /></label></div>
      <label v-if="editing.has_password" class="check-row"><input v-model="editing.clear_password" type="checkbox" />清除已保存的代理密码</label>
      <p class="muted">密码加密保存在服务器，不回传浏览器。更换代理地址或用户名时需重新填写密码。开启后连接失败不会自动切回直连，保存从后续请求生效。</p>
      <p v-if="error" class="error-banner" role="alert">{{ error }}</p>
      <div class="modal-actions"><button data-save-inline class="button primary">应用代理配置<AppIcon name="check" :size="16" /></button><button type="button" class="small-button" @click="editing = null">取消</button></div>
      <p class="muted">应用后按页面自动保存开关保存；关闭自动保存时，请手动保存站点设置。</p>
    </form>
  </ModalShell>
</template>

<style scoped>
.proxy-summary{display:flex;align-items:center;justify-content:space-between;gap:14px;padding:16px 0 4px;border-top:1px solid var(--line);margin-top:18px}.proxy-summary p{margin:6px 0;overflow-wrap:anywhere}.proxy-summary>div{min-width:0}.proxy-summary button{flex-shrink:0}
</style>

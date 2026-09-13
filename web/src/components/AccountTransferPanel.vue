<script setup lang="ts">
import { onUnmounted, ref } from 'vue'
import { api } from '../api'
import AppIcon from './AppIcon.vue'
import ModalShell from './ModalShell.vue'

const emit = defineEmits<{ changed: [] }>()
type Preview = { id: string; expires_at: string; preview: { names: string[]; add: number; skipped: number } }
const open = ref(false), mode = ref<'export' | 'import'>('export'), busy = ref(false), error = ref(''), notice = ref('')
const password = ref(''), passphrase = ref(''), confirmation = ref(''), file = ref<unknown>(null), filename = ref(''), confirmed = ref(false)
const preview = ref<Preview | null>(null)
let generation = 0
const downloads = new Set<string>()
function cancelPreview() {
  if (preview.value) void api('/api/v1/profile/archive/preview', { method: 'DELETE', body: JSON.stringify({ id: preview.value.id }) }).catch(() => {})
  preview.value = null; confirmed.value = false
}
function reset() { generation++; cancelPreview(); password.value = ''; passphrase.value = ''; confirmation.value = ''; file.value = null; filename.value = ''; error.value = '' }
function close() { if (busy.value) return; reset(); open.value = false }
function changeMode(value: 'export' | 'import') { if (busy.value) return; reset(); mode.value = value }
async function selectFile(event: Event) {
  cancelPreview(); error.value = ''; file.value = null; filename.value = ''
  const selected = (event.target as HTMLInputElement).files?.[0], current = ++generation
  if (!selected) return
  if (selected.size > 768 * 1024) { error.value = '迁移文件不能超过 768 KiB'; return }
  try {
    const value = JSON.parse(await selected.text())
    if (current !== generation) return
    if (!value || value.format !== 'miyohub-accounts' || value.version !== 1) throw new Error()
    file.value = value; filename.value = selected.name
  } catch { if (current === generation) error.value = '请选择受支持的加密迁移文件，不接受明文状态文件' }
}
async function exportFile() {
  if (busy.value) return
  error.value = ''
  if (passphrase.value !== confirmation.value) { error.value = '两次迁移密码不一致'; return }
  busy.value = true
  const current = generation
  try {
    const data = await api<unknown>('/api/v1/profile/archive/export', { method: 'POST', body: JSON.stringify({ password: password.value, passphrase: passphrase.value }) })
    if (current !== generation) return
    const url = URL.createObjectURL(new Blob([JSON.stringify(data)], { type: 'application/json' }))
    downloads.add(url)
    const link = document.createElement('a'); link.href = url; link.download = 'miyohub-accounts-' + new Date().toISOString().slice(0, 10) + '.miyohub'; link.click()
    window.setTimeout(() => { URL.revokeObjectURL(url); downloads.delete(url) }, 1000)
    notice.value = '加密文件已生成。请将迁移密码与文件分开保管；本站不保存迁移密码。'
    password.value = ''; passphrase.value = ''; confirmation.value = ''; open.value = false
  } catch (e) { error.value = e instanceof Error ? e.message : '导出失败' }
  finally { busy.value = false }
}
async function inspect() {
  if (!file.value || busy.value) return
  busy.value = true; error.value = ''
  const current = generation
  try {
    const result = await api<Preview>('/api/v1/profile/archive/preview', { method: 'POST', body: JSON.stringify({ archive: file.value, passphrase: passphrase.value }) })
    if (current !== generation) {
      void api('/api/v1/profile/archive/preview', { method: 'DELETE', body: JSON.stringify({ id: result.id }) }).catch(() => {})
      return
    }
    preview.value = result
    passphrase.value = ''; file.value = null
  } catch (e) { error.value = e instanceof Error ? e.message : '无法读取迁移文件' }
  finally { busy.value = false }
}
async function apply() {
  if (!preview.value || !confirmed.value || busy.value) return
  busy.value = true; error.value = ''
  try {
    const result = await api<Preview['preview']>('/api/v1/profile/archive/import', { method: 'POST', body: JSON.stringify({ id: preview.value.id, confirm: true }) })
    preview.value = null; confirmed.value = false; open.value = false
    notice.value = `已导入 ${result.add} 个账号，跳过 ${result.skipped} 个已绑定或重名账号。新账号均已停用，请核对配置后再启用。`
    emit('changed')
  } catch (e) { error.value = e instanceof Error ? e.message : '导入失败，请重新预览' }
  finally { busy.value = false }
}
onUnmounted(() => { reset(); for (const url of downloads) URL.revokeObjectURL(url) })
</script>

<template>
  <article class="panel transfer-panel"><div class="panel-title"><div><p class="eyebrow">个人配置</p><h2>账号迁移</h2></div><AppIcon name="shield" /></div><p class="muted">在自己的不同部署实例间，用密码加密的文件迁移账号、分组与签到设置。无需第三方同步服务。</p><button class="small-button" @click="reset(); open = true">迁移账号配置<AppIcon name="arrow" :size="14" /></button><p v-if="notice" class="notice" role="status">{{ notice }}</p></article>
  <ModalShell v-if="open" title="加密迁移账号配置" :busy="busy" @close="close">
    <div class="transfer-tabs" role="tablist" aria-label="迁移方式"><button type="button" role="tab" :aria-selected="mode === 'export'" :disabled="busy" @click="changeMode('export')">导出</button><button type="button" role="tab" :aria-selected="mode === 'import'" :disabled="busy" @click="changeMode('import')">导入</button></div>
    <p class="muted">文件包含账号登录凭据，使用 AES-256-GCM 和 PBKDF2-SHA256 加密。丢失迁移密码无法恢复；请只向可信的 HTTPS 实例导入。</p>
    <form v-if="mode === 'export'" class="transfer-form" @submit.prevent="exportFile">
      <label>本站登录密码<input v-model="password" type="password" required maxlength="128" autocomplete="current-password" /></label>
      <label>迁移密码<input v-model="passphrase" type="password" required minlength="12" maxlength="128" autocomplete="new-password" /></label>
      <label>确认迁移密码<input v-model="confirmation" type="password" required minlength="12" maxlength="128" autocomplete="new-password" /></label>
      <p class="muted">最多 100 个账号；不包含站点用户、权限、打码/推送密钥、日志或兑换预约。导出不会停止原实例的任务。</p>
      <button class="button primary" :disabled="busy">{{ busy ? '正在加密…' : '生成加密文件' }}</button>
    </form>
    <form v-else-if="!preview" class="transfer-form" @submit.prevent="inspect">
      <label>选择加密文件<input type="file" accept=".miyohub,application/json" :disabled="busy" @change="selectFile" /></label><small class="muted">{{ filename || '最大 768 KiB；不接受明文配置' }}</small>
      <label>迁移密码<input v-model="passphrase" type="password" required minlength="12" maxlength="128" autocomplete="off" /></label>
      <button class="button primary" :disabled="busy || !file">{{ busy ? '正在解密…' : '解析并预览' }}</button>
    </form>
    <form v-else class="transfer-form" @submit.prevent="apply">
      <p>可新增 {{ preview.preview.add }} 个账号，跳过 {{ preview.preview.skipped }} 个已绑定或重名账号。</p>
      <ul class="transfer-names"><li v-for="name in preview.preview.names" :key="name">{{ name }}</li></ul>
      <p class="muted">预览 5 分钟后失效。不会覆盖原有账号、赋予管理员权限或立即执行任务。切换部署前请自行停用原实例的对应任务，避免重复执行。</p>
      <label class="check-row"><input v-model="confirmed" type="checkbox" />我确认新账号默认停用，核对后再手动启用</label>
      <button class="button primary" :disabled="busy || !confirmed || !preview.preview.add">{{ busy ? '正在导入…' : '确认导入' }}</button>
      <button type="button" class="text-button" :disabled="busy" @click="reset">重新选择文件</button>
    </form>
    <p v-if="error" class="error-banner" role="alert">{{ error }}</p>
  </ModalShell>
</template>

<style scoped>
.transfer-panel{margin-top:20px}.transfer-form{display:grid;gap:16px}.transfer-form>label:not(.check-row){display:grid;gap:8px}.transfer-form input[type=file]{max-width:100%;min-width:0;font-size:12px}.transfer-tabs{display:flex;gap:8px;margin-bottom:18px}.transfer-tabs button{border:1px solid #dfdece;background:#f8f6ee;color:#737865;border-radius:10px;padding:9px 22px;font:inherit;font-size:13px;cursor:pointer}.transfer-tabs [aria-selected=true]{background:#6e8060;color:white;border-color:#6e8060}.transfer-names{margin:0;max-height:160px;overflow:auto;padding-left:20px;overflow-wrap:anywhere}.transfer-form .check-row{align-items:start;line-height:1.7}.transfer-panel>.notice{margin-top:15px}
</style>

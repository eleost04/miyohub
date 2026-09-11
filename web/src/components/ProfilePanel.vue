<script setup lang="ts">
import { ref } from 'vue'
import { api, resetApiSession } from '../api'
import type { User } from '../types'
const props = defineProps<{ user?: User }>()
const emit = defineEmits<{ guide: [] }>()
const idNotice = ref('')
async function copyID() { try { await navigator.clipboard.writeText(props.user?.id || ''); idNotice.value = '用户 ID 已复制' } catch { idNotice.value = '可选择下方用户 ID 手动复制' } }
const oldPassword = ref(''), newPassword = ref(''), confirmation = ref(''), busy = ref(false), message = ref(''), error = ref('')
async function save() {
 busy.value = true; error.value = ''; message.value = ''
 try {
  if (newPassword.value !== confirmation.value) throw new Error('两次新密码不一致')
  await api('/api/v1/auth/password', { method: 'POST', body: JSON.stringify({ old_password: oldPassword.value, new_password: newPassword.value }) })
  resetApiSession(); oldPassword.value = ''; newPassword.value = ''; confirmation.value = ''; message.value = '密码已更新，其他设备的登录已失效。'
 } catch (e) { error.value = e instanceof Error ? e.message : '修改失败' } finally { busy.value = false }
}
</script>
<template><section class="profile-summary panel"><span class="profile-avatar">{{ user?.username.slice(0, 1) }}</span><div><p class="eyebrow">站点账号</p><h2>{{ user?.username || '我的账号' }}</h2><p v-if="user?.id" class="profile-user-id"><span>站点用户 ID</span><code>{{ user.id }}</code><button class="text-button" type="button" @click="copyID">复制</button></p><small v-if="idNotice" class="muted" role="status">{{ idNotice }}</small><p class="muted">{{ user?.role === 'admin' ? '站点管理员' : '普通用户' }} · 通知渠道在「消息推送」中配置</p><button class="small-button" type="button" @click="emit('guide')">重新打开配置引导</button></div></section><article class="panel profile-security"><div class="panel-title"><div><p class="eyebrow">账号安全</p><h2>修改登录密码</h2></div><span class="pill soft">安全设置</span></div><form class="admin-toolbar" @submit.prevent="save"><label>原密码<input v-model="oldPassword" type="password" required maxlength="128" autocomplete="current-password" /></label><label>新密码<input v-model="newPassword" type="password" required minlength="8" maxlength="128" autocomplete="new-password" /></label><label>确认新密码<input v-model="confirmation" type="password" required minlength="8" maxlength="128" autocomplete="new-password" /></label><button class="small-button" :disabled="busy">{{ busy ? '保存中…' : '修改密码' }}</button></form><p class="muted">修改密码后，其他设备的登录会自动失效。</p><p v-if="message" class="notice" role="status">{{ message }}</p><p v-if="error" class="error-banner" role="alert">{{ error }}</p></article></template>
<style scoped>label{display:grid;gap:8px;font-size:13px}.admin-toolbar{align-items:end}.profile-user-id{display:flex;flex-wrap:wrap;gap:6px 10px;align-items:center;font-size:11px;color:#7f8274}.profile-user-id code{overflow-wrap:anywhere}.profile-summary>div{min-width:0}</style>

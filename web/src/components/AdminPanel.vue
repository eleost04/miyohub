<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { api } from '../api'
import { formatDate, zonedEpoch } from '../time'
import type { User, InviteCode } from '../types'
import AppIcon from './AppIcon.vue'
import ModalShell from './ModalShell.vue'
import ChoiceSelect from './ChoiceSelect.vue'
import FloatingSave from './FloatingSave.vue'
import DateTimeField from './DateTimeField.vue'
const props = defineProps<{ user: User; timezone: string }>()
const users = ref<User[]>([]), invites = ref<InviteCode[]>([]), mode = ref('review')
const busy = ref(false), error = ref(''), notice = ref('')
const dialog = ref<'' | 'manage' | 'create' | 'invite' | 'reset' | 'delete' | 'deleteInvite'>('')
const selectedUser = ref<User | null>(null), draft = ref<User | null>(null), selectedInvite = ref<InviteCode | null>(null)
const baseline = ref(''), discard = ref(false), resetPassword = ref('')
const newUser = ref({ username: '', password: '', role: 'user' })
const newInvite = () => ({ expires_at: '', max_uses: 1, note: '', permissions: { exchange: true, site_captcha: true }, use_site_captcha: true })
const invite = ref(newInvite()), expiryPreset = ref('7')
const expiryOptions = [{ value: '7', label: '7 天后失效', description: '推荐；过期后不能再用于注册' }, { value: '1', label: '24 小时后失效' }, { value: '30', label: '30 天后失效' }, { value: 'custom', label: '自定义失效时间' }, { value: 'never', label: '不设失效时间', description: '仅建议在受控范围内使用' }]
watch(() => invite.value.permissions.site_captcha, allowed => { if (!allowed) invite.value.use_site_captcha = false })
const manageDirty = computed(() => dialog.value === 'manage' && JSON.stringify(draft.value) !== baseline.value)
const statusOptions = [{ value: 'active', label: '已启用 / 审核通过' }, { value: 'pending', label: '等待审核' }, { value: 'disabled', label: '停用 / 拒绝访问' }]
const roleOptions = [{ value: 'user', label: '普通用户', description: '自行管理账号任务、个人打码和推送' }, { value: 'admin', label: '管理员', description: '管理全站与全部用户，自动具备服务权限' }]
const registrationOptions = [{ value: 'review', label: '注册后审核', description: '管理员通过后才可登录' }, { value: 'open', label: '开放注册', description: '注册后即可登录，受限服务仍需授权' }, { value: 'closed', label: '仅邀请码注册', description: '必须提供有效邀请码' }]
const dialogTitle = computed(() => ({ manage: (selectedUser.value?.username || '') + ' · 用户设置', create: '创建新用户', invite: '创建邀请码', reset: '重置用户密码', delete: '删除用户？', deleteInvite: '删除邀请码？', '': '' })[dialog.value])
const life = new AbortController()
const request = <T,>(path: string, init: RequestInit = {}) => api<T>(path, { ...init, signal: life.signal })
async function load() {
 const [u, codes] = await Promise.all([request<{ users: User[]; registration_mode: string }>('/api/v1/admin/users'), request<InviteCode[]>('/api/v1/admin/invite-codes')])
 users.value = u.users; mode.value = u.registration_mode; invites.value = codes
}
async function act(fn: () => Promise<unknown>, message = '已保存') {
 if (busy.value) return false
 busy.value = true; error.value = ''; notice.value = ''
 try { await fn(); await load(); notice.value = message; return true } catch (e) { if (!life.signal.aborted) error.value = e instanceof Error ? e.message : '操作失败'; return false } finally { busy.value = false }
}
function manage(u: User) { selectedUser.value = u; draft.value = JSON.parse(JSON.stringify({ ...u, permissions: u.permissions || { exchange: false, site_captcha: false } })); baseline.value = JSON.stringify(draft.value); dialog.value = 'manage'; discard.value = false; error.value = '' }
function close(force = false) { if (busy.value) return; if (!force && manageDirty.value) { discard.value = true; return }; dialog.value = ''; draft.value = null; resetPassword.value = ''; newUser.value.password = ''; discard.value = false; error.value = '' }
function permission(key: 'exchange' | 'site_captcha', event: Event) { if (draft.value) draft.value.permissions[key] = (event.target as HTMLInputElement).checked }
async function saveUser() {
 if (!draft.value) return
 const u = draft.value
 if (await act(() => request('/api/v1/admin/users', { method: 'PUT', body: JSON.stringify({ user_id: u.id, status: u.status, role: u.role, permissions: u.permissions }) }), '用户状态与服务权限已保存')) close(true)
}
async function createUser() { if (await act(async () => { await request('/api/v1/admin/users', { method: 'POST', body: JSON.stringify(newUser.value) }); newUser.value = { username: '', password: '', role: 'user' } }, '用户已创建，兑换和站点打码可在用户设置中单独授权')) close(true) }
async function reset() { if (await act(() => request('/api/v1/admin/users/password', { method: 'PUT', body: JSON.stringify({ user_id: selectedUser.value?.id, password: resetPassword.value }) }), '密码已重置，旧登录已失效')) close(true) }
async function removeUser() { if (await act(() => request('/api/v1/admin/users/delete', { method: 'DELETE', body: JSON.stringify({ user_id: selectedUser.value?.id }) }), '用户及其绑定账号、计划和个人服务配置已删除')) close(true) }
async function createInvite() { if (await act(async () => {
  const epoch = expiryPreset.value === 'custom' ? zonedEpoch(invite.value.expires_at, props.timezone) : expiryPreset.value === 'never' ? 0 : Math.floor(Date.now()/1000) + Number(expiryPreset.value)*86400
  if (expiryPreset.value === 'custom' && (!epoch || epoch*1000 <= Date.now())) throw new Error('请选择未来的失效时间')
  await request('/api/v1/admin/invite-codes', { method: 'POST', body: JSON.stringify({ ...invite.value, expires_at: epoch ? new Date(epoch * 1000).toISOString() : '' }) })
  invite.value = newInvite(); expiryPreset.value = '7'
}, '邀请码已创建，注册者将继承此邀请码预设的服务权限')) close(true) }
function revoke(c: InviteCode) { void act(() => request('/api/v1/admin/invite-codes', { method: 'PUT', body: JSON.stringify({ code: c.code, disabled: !c.disabled }) })) }
async function removeInvite() { if (await act(() => request('/api/v1/admin/invite-codes', { method: 'DELETE', body: JSON.stringify({ code: selectedInvite.value?.code }) }), '邀请码已删除')) close(true) }
function link(c: InviteCode) { return new URL('/?invite=' + encodeURIComponent(c.code), window.location.origin).href }
async function copy(c: InviteCode) { try { await navigator.clipboard.writeText(link(c)); notice.value = '邀请链接已复制' } catch { notice.value = '请长按或右键下方链接复制。' } }
function inviteStatus(c: InviteCode) { if (c.disabled) return '已注销'; if (c.expires_at && !c.expires_at.startsWith('0001-') && new Date(c.expires_at).getTime() <= Date.now()) return '已过期'; if (c.max_uses && c.used_count >= c.max_uses) return '次数用尽'; return '可使用' }
onMounted(() => { void act(load, '') })
onUnmounted(() => life.abort())
</script>

<template>
 <p v-if="error && !dialog" class="error-banner" role="alert">{{ error }}</p><p v-if="notice" class="notice" role="status">{{ notice }}</p>
 <article class="panel admin-panel">
   <div class="panel-title"><div><p class="eyebrow">用户管理</p><h2>用户与服务授权 <span class="count-label">{{ users.length }}</span></h2></div><button class="small-button primary-mini" :disabled="busy" @click="dialog = 'create'; error = ''"><AppIcon name="plus" :size="14" />创建用户</button></div>
   <div class="admin-registration"><label>注册方式<ChoiceSelect v-model="mode" label="注册方式" :options="registrationOptions" :disabled="busy" /></label><button class="small-button" :disabled="busy" @click="act(() => request('/api/v1/admin/settings', { method: 'PUT', body: JSON.stringify({ registration_mode: mode }) }), '注册方式已保存')">保存注册方式</button></div>
   <p class="muted">普通用户默认可管理自己的签到、打码和推送。商品兑换与站点打码服务需分别授权。</p>
   <div class="admin-user-list"><section v-for="u in users" :key="u.id" class="admin-user-row">
     <span class="account-avatar">{{ u.username.slice(0, 1) }}</span>
     <div class="admin-user-info"><strong>{{ u.username }}{{ u.id === user.id ? '（我）' : '' }}</strong><small>{{ u.role === 'admin' ? '管理员' : '普通用户' }} · {{ u.status === 'pending' ? '等待审核' : u.status === 'active' ? '已启用' : '已停用' }}</small><div class="permission-badges"><span :class="{ granted: u.role === 'admin' || u.permissions?.exchange }">{{ u.role === 'admin' || u.permissions?.exchange ? '可兑换' : '未授权兑换' }}</span><span :class="{ granted: u.role === 'admin' || u.permissions?.site_captcha }">{{ u.role === 'admin' || u.permissions?.site_captcha ? '可用站点打码' : '未授权站点打码' }}</span></div></div>
     <button class="small-button" :disabled="busy" :aria-label="'管理用户 ' + u.username" @click="manage(u)"><AppIcon name="settings" :size="14" />{{ u.id === user.id ? '查看' : '管理' }}</button>
   </section></div>
 </article>
 <article class="panel admin-panel">
   <div class="panel-title"><div><p class="eyebrow">注册邀请</p><h2>邀请码管理</h2></div><button class="small-button" :disabled="busy" @click="dialog = 'invite'; error = ''"><AppIcon name="plus" :size="14" />创建邀请码</button></div>
   <p class="muted">凭有效邀请码注册后免审核，并继承创建时预设的服务权限。邀请码由服务器随机生成，请只分享给受邀人。</p>
   <div v-for="c in invites" :key="c.code" class="account-row"><div class="admin-invite-info"><strong>{{ c.code }} · {{ inviteStatus(c) }}</strong><small>{{ c.used_count }}/{{ c.max_uses || '∞' }} 次 · {{ !c.expires_at || c.expires_at.startsWith('0001-') ? '无到期时间' : formatDate(c.expires_at, timezone) }}</small><div class="permission-badges"><span :class="{ granted: c.permissions?.exchange }">{{ c.permissions?.exchange ? '授予兑换权限' : '不授予兑换权限' }}</span><span :class="{ granted: c.permissions?.site_captcha }">{{ c.use_site_captcha ? '默认使用站点打码' : c.permissions?.site_captcha ? '可选站点打码' : '不授予站点打码' }}</span></div><small v-if="c.note">{{ c.note }}</small><a :href="link(c)" target="_blank" rel="noopener noreferrer">{{ link(c) }}</a></div><button class="small-button" @click="copy(c)">复制链接</button><button class="small-button" :disabled="busy" @click="revoke(c)">{{ c.disabled ? '重新启用' : '停用' }}</button><button class="text-button error-ink" :disabled="busy" @click="selectedInvite = c; dialog = 'deleteInvite'; error = ''">删除</button></div><p v-if="!invites.length" class="empty">暂无邀请码，可按需创建。</p>
 </article>
 <ModalShell v-if="dialog" :title="dialogTitle" :busy="busy" :wide="dialog === 'manage'" @close="close()">
   <form v-if="dialog === 'manage' && draft" class="admin-dialog-form" @submit.prevent="saveUser">
     <p class="muted user-id">站点用户 ID：{{ draft.id }}</p>
     <FloatingSave v-if="draft.id !== user.id" label="保存用户设置" :busy="busy" />
     <div class="form-grid"><label>用户状态<ChoiceSelect v-model="draft.status" label="用户状态" :options="statusOptions" :disabled="busy || draft.id === user.id" /></label><label>用户角色<ChoiceSelect v-model="draft.role" label="用户角色" :options="roleOptions" :disabled="busy || draft.id === user.id" /></label></div>
     <div class="permission-fields"><h3>独立服务权限</h3>
       <label class="preference-switch"><span><strong>商品兑换</strong><small>允许创建、修改、即时执行与预约兑换计划。收回后停止后续兑换请求。</small></span><span class="push-switch"><input :checked="draft.role === 'admin' || draft.permissions.exchange" type="checkbox" aria-label="授予商品兑换权限" :disabled="busy || draft.role === 'admin' || draft.id === user.id" @change="permission('exchange', $event)" /><span aria-hidden="true"></span></span></label>
       <label class="preference-switch"><span><strong>使用站点打码服务</strong><small>允许使用管理员提供的服务。用户配置自己的公网打码渠道无需这个权限。</small></span><span class="push-switch"><input :checked="draft.role === 'admin' || draft.permissions.site_captcha" type="checkbox" aria-label="授予站点打码权限" :disabled="busy || draft.role === 'admin' || draft.id === user.id" @change="permission('site_captcha', $event)" /><span aria-hidden="true"></span></span></label>
     </div>
     <p class="muted">{{ draft.role === 'admin' ? '管理员自动拥有全部服务权限；请谨慎提升角色。' : '两项权限互相独立，不影响用户自己的签到项目和推送配置。' }}</p>
     <p v-if="draft.id === user.id" class="scope-note">当前管理员不能修改自己的角色或状态，避免误操作导致无法管理站点。</p>
     <div v-if="discard" class="discard-inline" role="alert"><p>有未保存的用户设置，要放弃吗？</p><button type="button" class="small-button" @click="discard = false">继续编辑</button><button type="button" class="text-button error-ink" @click="close(true)">放弃修改</button></div>
     <div class="modal-actions sticky-modal-actions"><button v-if="draft.id !== user.id" data-save-inline class="button primary" :disabled="busy">保存用户设置<AppIcon name="check" :size="16" /></button><button type="button" class="small-button" :disabled="busy" @click="close()">{{ draft.id === user.id ? '关闭' : '取消' }}</button></div>
     <div v-if="draft.id !== user.id" class="admin-user-actions"><button type="button" class="text-button" :disabled="busy || manageDirty" @click="dialog = 'reset'; resetPassword = ''; error = ''">重置密码</button><button type="button" class="text-button error-ink" :disabled="busy || manageDirty" @click="dialog = 'delete'; error = ''">删除用户</button></div>
   </form>
   <form v-else-if="dialog === 'create'" class="admin-dialog-form" @submit.prevent="createUser"><label>用户名<input v-model="newUser.username" maxlength="64" required autocomplete="off" /></label><label>初始密码<input v-model="newUser.password" type="password" minlength="8" maxlength="128" autocomplete="new-password" required /></label><label>角色<ChoiceSelect v-model="newUser.role" label="新用户角色" :options="roleOptions" :disabled="busy" /></label><p class="muted">普通用户的兑换、站点打码权限默认关闭，可创建后分别开通。</p><button class="button primary" :disabled="busy">创建用户</button></form>
   <form v-else-if="dialog === 'invite'" class="admin-dialog-form" @submit.prevent="createInvite">
     <p class="muted">生成不可预测的随机邀请码，不需要手动填写。以下权限只应用于之后使用本邀请码注册的普通用户。</p>
     <label>邀请码有效期<ChoiceSelect v-model="expiryPreset" label="邀请码有效期" :options="expiryOptions" :disabled="busy" /></label>
     <label v-if="expiryPreset === 'custom'">失效时间（{{ timezone }}）<DateTimeField v-model="invite.expires_at" label="邀请码失效时间" :timezone="timezone" :disabled="busy" seconds /></label>
     <div class="form-grid"><label>使用次数（0 为不限）<input v-model.number="invite.max_uses" type="number" min="0" max="100000" required :disabled="busy" /></label><label>备注<input v-model="invite.note" maxlength="160" :disabled="busy" /></label></div>
     <div class="permission-fields"><h3>注册后默认权限</h3>
       <label class="preference-switch"><span><strong>允许商品兑换</strong><small>用户可为自己的账号创建和执行兑换计划。</small></span><span class="push-switch"><input v-model="invite.permissions.exchange" type="checkbox" aria-label="邀请码授予兑换权限" :disabled="busy" /><span aria-hidden="true"></span></span></label>
       <label class="preference-switch"><span><strong>允许使用站点打码</strong><small>授权使用管理员提供的验证码服务。</small></span><span class="push-switch"><input v-model="invite.permissions.site_captcha" type="checkbox" aria-label="邀请码授予站点打码权限" :disabled="busy" /><span aria-hidden="true"></span></span></label>
       <label class="check-row"><input v-model="invite.use_site_captcha" type="checkbox" :disabled="busy || !invite.permissions.site_captcha" />注册后默认选择站点打码</label>
     </div>
     <p v-if="expiryPreset === 'never' || invite.max_uses === 0" class="scope-note">当前邀请码未限制{{ expiryPreset === 'never' ? '有效期' : '' }}{{ expiryPreset === 'never' && invite.max_uses === 0 ? '和' : '' }}{{ invite.max_uses === 0 ? '使用次数' : '' }}。分享后如不再需要，请及时停用。</p>
     <div class="modal-actions sticky-modal-actions"><button class="button primary" :disabled="busy">生成随机邀请码<AppIcon name="check" :size="16" /></button><button type="button" class="small-button" :disabled="busy" @click="close(true)">取消</button></div>
   </form>
   <form v-else-if="dialog === 'reset'" class="admin-dialog-form" @submit.prevent="reset"><p class="muted">为 {{ selectedUser?.username }} 设置新密码，旧设备的登录会立即失效。</p><label>新密码<input v-model="resetPassword" type="password" minlength="8" maxlength="128" autocomplete="new-password" required /></label><button class="button primary" :disabled="busy">重置并撤销旧登录</button></form>
   <template v-else-if="dialog === 'delete'"><p class="muted">将删除 {{ selectedUser?.username }} 及其全部绑定账号、兑换计划、日志与个人服务配置。此操作不能在界面中撤销。</p><div class="modal-actions"><button class="small-button" :disabled="busy" @click="close(true)">保留用户</button><button class="button danger-button" :disabled="busy" @click="removeUser">确认删除用户及全部账号</button></div></template>
   <template v-else-if="dialog === 'deleteInvite'"><p class="muted">删除邀请码 {{ selectedInvite?.code }}？已通过此邀请码注册的用户不受影响。</p><div class="modal-actions"><button class="small-button" :disabled="busy" @click="close(true)">保留</button><button class="button danger-button" :disabled="busy" @click="removeInvite">确认删除邀请码</button></div></template>
   <p v-if="error" class="error-banner" role="alert">{{ error }}</p>
 </ModalShell>
</template>
<style scoped>
.account-row{flex-wrap:wrap;gap:10px}.admin-panel>.panel-title{flex-wrap:wrap}
.admin-invite-info{min-width:0}.admin-invite-info strong,.admin-invite-info a,.user-id{overflow-wrap:anywhere}
</style>

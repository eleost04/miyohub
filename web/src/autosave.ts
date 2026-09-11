import { onUnmounted, ref, watch, type Ref } from 'vue'
import type { CaptchaChannel } from './types'

function preference() { try { return localStorage.getItem('miyohub:auto-save') !== 'false' } catch { return true } }
export const autoSaveEnabled = ref(preference())
watch(autoSaveEnabled, enabled => { try { localStorage.setItem('miyohub:auto-save', String(enabled)) } catch { /* Storage is optional. */ } })

export function useAutoSave(snapshot: () => string, dirty: Ref<boolean>, busy: Ref<boolean>, valid: () => boolean, save: () => Promise<void>) {
  let timer: number | undefined, attempt = '', pending: Promise<void> | undefined, active = true
  const state = ref('saved')
  async function run(force = false) {
    window.clearTimeout(timer)
    if (pending) await pending
    if (!active || !dirty.value || busy.value || !valid()) return
    const value = snapshot()
    if (!force && attempt === value) return
    attempt = value; state.value = 'saving'
    pending = save()
    try { await pending } finally { pending = undefined; state.value = dirty.value ? 'unsaved' : 'saved'; schedule() }
  }
  function schedule() {
    window.clearTimeout(timer)
    if (!active) return
    if (busy.value) { state.value = 'saving'; return }
    if (!dirty.value) { state.value = 'saved'; attempt = ''; return }
    if (!autoSaveEnabled.value) { state.value = 'manual'; return }
    if (!valid()) { state.value = 'invalid'; return }
    if (attempt === snapshot()) { state.value = 'unsaved'; return } // Do not loop on a failed request.
    state.value = 'waiting'; timer = window.setTimeout(() => void run(), 1000)
  }
  watch([snapshot, dirty, busy, autoSaveEnabled], schedule, { flush: 'post' })
  onUnmounted(() => { active = false; window.clearTimeout(timer) })
  return { state, flush: async () => { if (autoSaveEnabled.value) await run(true); return !dirty.value } }
}

// Apply only server metadata to a newer local draft. Never replace input typed
// while the request was in flight; clear only secrets that were just saved.
export function reconcileCaptcha(current: CaptchaChannel[], sent: CaptchaChannel[], saved: CaptchaChannel[]) {
  for (const channel of current) {
    const before = sent.find(item => item.id === channel.id), after = saved.find(item => item.id === channel.id)
    if (!before || !after) continue
    channel.configured = after.configured
    for (const key of ['token', 'userkey'] as const) if (channel[key] === before[key]) channel[key] = after[key]
    if (channel.clear_token === before.clear_token) channel.clear_token = after.clear_token
  }
}

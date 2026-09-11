<script setup lang="ts">
import { onMounted, onUnmounted, provide, ref } from 'vue'
import AppIcon from './AppIcon.vue'
import { lockPageScroll, overlayOwner } from '../overlays'
import { registerBackLayer } from '../history'
const props = defineProps<{ title: string; eyebrow?: string; busy?: boolean; wide?: boolean; hidden?: boolean }>()
const emit = defineEmits<{ close: [] }>()
const dialog = ref<HTMLElement | null>(null)
const owner = Symbol('dialog')
provide(overlayOwner, owner)
let previous: HTMLElement | null = null, unlock: (() => void) | undefined, releaseBack: (() => void) | undefined
function close() { if (!props.busy) emit('close') }
function keyboard(event: KeyboardEvent) {
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close() }
  if (event.key !== 'Tab' || !dialog.value) return
  const elements = [...dialog.value.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), select:not(:disabled), a[href], textarea, [tabindex="0"]')].filter(el => el.getClientRects().length)
  const first = elements[0], last = elements[elements.length - 1]
  if (!first) { event.preventDefault(); return }
  if (event.shiftKey && (document.activeElement === first || document.activeElement === dialog.value)) { event.preventDefault(); last?.focus() }
  if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
}
onMounted(() => { previous = document.activeElement as HTMLElement; unlock = lockPageScroll(owner); releaseBack = registerBackLayer(close); dialog.value?.focus({ preventScroll: true }) })
onUnmounted(() => { releaseBack?.(); unlock?.(); if (previous?.isConnected) previous.focus({ preventScroll: true }) })
</script>
<template>
  <Teleport to="body"><div v-show="!hidden" class="modal-backdrop" :aria-hidden="hidden || undefined" @click.self="close" @keydown="keyboard"><section ref="dialog" class="login-modal" :class="{ 'wide-modal': wide }" role="dialog" aria-modal="true" :aria-label="title" tabindex="-1">
    <div class="panel-title modal-title"><div><p v-if="eyebrow" class="eyebrow">{{ eyebrow }}</p><h2>{{ title }}</h2></div><button type="button" class="icon-button" aria-label="关闭弹窗" :disabled="busy" @click="close"><AppIcon name="close" /></button></div>
    <slot />
  </section></div></Teleport>
</template>

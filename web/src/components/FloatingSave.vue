<script setup lang="ts">
import { computed, inject, onMounted, onUnmounted, ref } from 'vue'
import AppIcon from './AppIcon.vue'
import { overlayOwner, topOverlay } from '../overlays'

const props = withDefaults(defineProps<{ label: string; text?: string; active?: boolean; busy?: boolean; disabled?: boolean; type?: 'submit' | 'button' }>(), { type: 'submit', text: '保存', active: true })
const button = ref<HTMLButtonElement | null>(null), inlineVisible = ref(true), bottom = ref('')
const owner = inject(overlayOwner, undefined)
const visible = computed(() => props.active && !inlineVisible.value && topOverlay.value === owner)
let observer: IntersectionObserver | undefined, inline: HTMLElement | null = null, margin = ''
function updateViewport() {
  const viewport = window.visualViewport
  const inset = Math.max(0, window.innerHeight - (viewport?.height || window.innerHeight) - (viewport?.offsetTop || 0))
  const gap = owner ? 16 : 96
  bottom.value = `calc(${gap + inset}px + env(safe-area-inset-bottom))`
  const nextMargin = `-${viewport?.offsetTop || 0}px 0px -${gap + inset}px 0px`
  if (!inline || nextMargin === margin) return
  margin = nextMargin; observer?.disconnect()
  observer = new IntersectionObserver(([entry]) => { inlineVisible.value = !!entry?.isIntersecting && entry.intersectionRatio >= .9 }, { rootMargin: margin, threshold: [0, .9, 1] })
  observer.observe(inline)
}
onMounted(() => {
  // Keep the real submit inside its form so required/min/pattern validation is
  // shared with the inline action. Only one save entry should be on screen.
  inline = button.value?.closest('form')?.querySelector<HTMLElement>('[data-save-inline]') || null
  if (!inline) inlineVisible.value = false
  updateViewport()
  window.addEventListener('resize', updateViewport)
  window.visualViewport?.addEventListener('resize', updateViewport)
  window.visualViewport?.addEventListener('scroll', updateViewport)
})
onUnmounted(() => {
  observer?.disconnect()
  window.removeEventListener('resize', updateViewport)
  window.visualViewport?.removeEventListener('resize', updateViewport)
  window.visualViewport?.removeEventListener('scroll', updateViewport)
})
</script>
<template><button v-show="visible" ref="button" :type="type" class="floating-save" :style="{ bottom }" :disabled="busy || disabled" :aria-label="'快速' + label" :aria-busy="busy"><span v-if="busy" class="loading-orbit small"></span><AppIcon v-else name="check" :size="17" /><span>{{ busy ? '保存中' : text }}</span></button></template>

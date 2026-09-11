<script setup lang="ts" generic="T extends string | number | boolean">
import { computed, nextTick, onUnmounted, ref, useId, watch } from 'vue'
import AppIcon from './AppIcon.vue'
import { lockPageScroll } from '../overlays'
import { registerBackLayer } from '../history'

const props = defineProps<{ modelValue: T; options: { value: T; label: string; description?: string; disabled?: boolean }[]; label: string; placeholder?: string; disabled?: boolean; searchable?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: T]; change: [value: T] }>()
const trigger = ref<HTMLButtonElement | null>(null), menu = ref<HTMLElement | null>(null)
const open = ref(false), query = ref(''), mobile = ref(false), position = ref<Record<string, string>>({}), viewportPosition = ref<Record<string, string>>({})
const listID = useId()
const selected = computed(() => props.options.find(option => option.value === props.modelValue))
const filtered = computed(() => props.options.filter(option => (option.label + ' ' + (option.description || '')).toLocaleLowerCase().includes(query.value.trim().toLocaleLowerCase())))
let unlock: (() => void) | undefined, releaseBack: (() => void) | undefined
let positionFrame: number | undefined
function close(focus = true) {
  open.value = false; releaseBack?.(); releaseBack = undefined; unlock?.(); unlock = undefined
  window.removeEventListener('resize', reposition)
  window.removeEventListener('scroll', reposition, true)
  window.visualViewport?.removeEventListener('resize', reposition)
  window.visualViewport?.removeEventListener('scroll', reposition)
  window.cancelAnimationFrame(positionFrame || 0); positionFrame = undefined
  if (focus) trigger.value?.focus({ preventScroll: true })
}
function updatePosition() {
  if (!open.value || !trigger.value) return
  const rect = trigger.value.getBoundingClientRect(), viewport = window.visualViewport
  const left = viewport?.offsetLeft || 0, top = viewport?.offsetTop || 0
  const width = viewport?.width || window.innerWidth, height = viewport?.height || window.innerHeight
  mobile.value = window.innerWidth <= 640
  viewportPosition.value = mobile.value ? { top: top + 'px', left: left + 'px', width: width + 'px', height: height + 'px', right: 'auto', bottom: 'auto' } : {}
  if (mobile.value) { position.value = { maxHeight: Math.min(560, height * .8) + 'px' }; return }
  const menuWidth = Math.min(Math.max(rect.width, 260), width - 32)
  const bottom = top + height
  const belowTop = Math.max(top + 16, Math.min(rect.bottom + 8, bottom - 16))
  const aboveBottom = Math.max(top + 16, Math.min(rect.top - 8, bottom - 16))
  const belowSpace = bottom - 16 - belowTop, aboveSpace = aboveBottom - top - 16
  const below = belowSpace >= Math.min(280, height - 32) || belowSpace >= aboveSpace
  // Keep the menu usable even if its trigger is outside the shrunken viewport.
  const placement: Record<string, string> = Math.max(belowSpace, aboveSpace) < 160
    ? { top: top + 16 + 'px', maxHeight: Math.min(400, height - 32) + 'px' }
    : { ...(below ? { top: belowTop + 'px' } : { bottom: window.innerHeight - aboveBottom + 'px' }), maxHeight: Math.min(400, below ? belowSpace : aboveSpace) + 'px' }
  position.value = { width: menuWidth + 'px', left: Math.max(left + 16, Math.min(rect.left, left + width - menuWidth - 16)) + 'px', ...placement }
}
function reposition() {
  if (positionFrame !== undefined) return
  positionFrame = window.requestAnimationFrame(() => { positionFrame = undefined; updatePosition() })
}
async function show() {
  if (props.disabled || trigger.value?.matches(':disabled')) return
  if (open.value) { close(); return }
  query.value = ''; open.value = true; unlock = lockPageScroll(); updatePosition()
  // Keyboard/toolbars and orientation changes resize the viewport, but must
  // not dismiss the picker or discard its search query.
  window.addEventListener('resize', reposition)
  window.addEventListener('scroll', reposition, true)
  window.visualViewport?.addEventListener('resize', reposition)
  window.visualViewport?.addEventListener('scroll', reposition)
  releaseBack = registerBackLayer(() => close())
  await nextTick()
  if (!open.value) return
  // Do not summon the mobile keyboard just to pick an existing account/address.
  const target = props.searchable && !mobile.value ? menu.value?.querySelector<HTMLInputElement>('input') : menu.value?.querySelector<HTMLElement>('[aria-selected="true"]:not(:disabled)') || menu.value?.querySelector<HTMLElement>('[role="option"]:not(:disabled)')
  ;(target || menu.value)?.focus({ preventScroll: true })
}
function choose(option: { value: T; disabled?: boolean }) { if (option.disabled) return; emit('update:modelValue', option.value); emit('change', option.value); close() }
function keyboard(event: KeyboardEvent) {
  if (event.key === 'Escape' || event.key === 'Tab') { event.preventDefault(); event.stopPropagation(); close(); return }
  if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return
  event.preventDefault(); event.stopPropagation()
  const elements = [...(menu.value?.querySelectorAll<HTMLElement>('[role="option"]:not(:disabled)') || [])]
  const current = elements.indexOf(document.activeElement as HTMLElement)
  const next = event.key === 'Home' ? 0 : event.key === 'End' ? elements.length - 1 : event.key === 'ArrowDown' ? (current + 1) % elements.length : (current <= 0 ? elements.length : current) - 1
  elements[next]?.focus()
}
watch(() => props.disabled, disabled => { if (disabled && open.value) close() })
onUnmounted(() => close(false))
</script>

<template>
  <button ref="trigger" type="button" class="choice-trigger" role="combobox" :aria-label="label" aria-haspopup="listbox" :aria-expanded="open" :aria-controls="open ? listID : undefined" :disabled="disabled" @click="show" @keydown.down.prevent="show" @keydown.up.prevent="show"><span>{{ selected?.label || placeholder || '请选择' }}</span><AppIcon name="chevron" :size="14" /></button>
  <Teleport to="body"><div v-if="open" class="choice-backdrop" :class="{ 'choice-mobile': mobile }" :style="viewportPosition" @click.self="close()" @keydown="keyboard">
    <section ref="menu" class="choice-menu" :style="position" tabindex="-1" :aria-label="label + '选项'">
      <div class="choice-menu-title"><strong>{{ label }}</strong><button type="button" class="icon-button" aria-label="关闭选择" @click="close()"><AppIcon name="close" :size="16" /></button></div>
      <input v-if="searchable" v-model="query" class="choice-search" type="search" :aria-label="'搜索' + label" placeholder="输入关键词筛选…" autocomplete="off" />
      <div :id="listID" role="listbox" :aria-label="label" class="choice-options">
        <button v-for="option in filtered" :key="typeof option.value + ':' + String(option.value)" type="button" role="option" :data-value="String(option.value)" :aria-label="option.label" :aria-description="option.description" :aria-selected="option.value === modelValue" :disabled="option.disabled" @click="choose(option)"><span><strong>{{ option.label }}</strong><small v-if="option.description">{{ option.description }}</small></span><AppIcon v-if="option.value === modelValue" name="check" :size="17" /></button>
        <p v-if="!filtered.length" class="empty">没有匹配选项</p>
      </div>
    </section>
  </div></Teleport>
</template>

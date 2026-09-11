<script setup lang="ts">
import { computed, ref } from 'vue'
import { zonedInput } from '../time'
import ModalShell from './ModalShell.vue'
import ChoiceSelect from './ChoiceSelect.vue'
import TimeField from './TimeField.vue'
import AppIcon from './AppIcon.vue'
const props = withDefaults(defineProps<{ modelValue: string; label: string; timezone?: string; disabled?: boolean; seconds?: boolean }>(), { timezone: 'Asia/Shanghai', disabled: false, seconds: false })
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const open = ref(false), day = ref(''), clock = ref('09:00'), year = ref(String(new Date().getFullYear())), month = ref('01')
const pad = (value: number) => String(value).padStart(2, '0')
const years = computed(() => { const current = new Date().getFullYear(); const first = Math.min(current - 1, Number(year.value)); return Array.from({ length: Math.max(current + 10, Number(year.value)) - first + 1 }, (_, i) => ({ value: String(first + i), label: first + i + ' 年' })) })
const months = Array.from({ length: 12 }, (_, i) => ({ value: pad(i + 1), label: i + 1 + ' 月' }))
const days = computed(() => {
  const y = Number(year.value), m = Number(month.value)
  const offset = (new Date(y, m - 1, 1).getDay() + 6) % 7
  const count = new Date(y, m, 0).getDate()
  return Array.from({ length: Math.ceil((offset + count) / 7) * 7 }, (_, i) => i >= offset && i < offset + count ? i - offset + 1 : 0)
})
const date = (value: number) => year.value + '-' + month.value + '-' + pad(value)
function edit() {
  const value = props.modelValue || zonedInput(Math.floor(Date.now() / 1000) + 3600, props.timezone)
  day.value = value.slice(0, 10); year.value = day.value.slice(0, 4); month.value = day.value.slice(5, 7)
  clock.value = value.slice(11, props.seconds ? 19 : 16) || '09:00'
  if (props.seconds && clock.value.length === 5) clock.value += ':00'
  open.value = true
}
function apply() { emit('update:modelValue', day.value + 'T' + clock.value); open.value = false }
</script>
<template>
  <button type="button" class="datetime-trigger" :aria-label="label" aria-haspopup="dialog" :aria-expanded="open" :disabled="disabled" @click="edit"><span>{{ modelValue ? modelValue.replace('T', ' ') : '选择日期与时间' }}</span><AppIcon name="clock" :size="17" /></button>
  <ModalShell v-if="open" :title="label" @close="open = false">
    <div class="calendar-month"><ChoiceSelect v-model="year" label="年份" :options="years" /><ChoiceSelect v-model="month" label="月份" :options="months" /></div>
    <div class="calendar-week" aria-hidden="true"><span v-for="name in ['一','二','三','四','五','六','日']" :key="name">{{ name }}</span></div>
    <div class="calendar-days" role="group" aria-label="选择日期"><template v-for="(value, index) in days" :key="index"><button v-if="value" type="button" :aria-label="date(value)" :aria-pressed="day === date(value)" :class="{ selected: day === date(value) }" @click="day = date(value)">{{ value }}</button><span v-else /></template></div>
    <p class="calendar-selection">已选 {{ day }} · {{ timezone }}</p>
    <TimeField v-model="clock" label="时间" :seconds="seconds" />
    <div class="modal-actions"><button type="button" class="button primary" :disabled="!day" @click="apply">应用时间<AppIcon name="check" :size="16" /></button><button type="button" class="small-button" @click="open = false">取消</button></div>
  </ModalShell>
</template>
<style scoped>
.datetime-trigger{display:flex;align-items:center;justify-content:space-between;gap:10px;width:100%;min-width:0;padding:11px 12px;border:1px solid #dfd7c9;border-radius:10px;background:#fffefa;color:#555b4b;font:inherit;text-align:left;font-size:12px;cursor:pointer}.datetime-trigger:disabled{opacity:.55;cursor:default}.datetime-trigger>span{overflow-wrap:anywhere}.calendar-month{display:grid;grid-template-columns:1fr 1fr;gap:12px}.calendar-week,.calendar-days{display:grid;grid-template-columns:repeat(7,minmax(0,1fr));gap:4px}.calendar-week{margin-top:18px;color:#94927d;text-align:center;font-size:11px;padding:9px 0}.calendar-days button{aspect-ratio:1;max-height:44px;border:0;border-radius:10px;background:transparent;color:#606853;font-size:13px;cursor:pointer}.calendar-days button:hover{background:#eeeede}.calendar-days button.selected{background:#6f8060;color:#fff}.calendar-selection{font-size:11px;color:#8a8876;margin:17px 0}
</style>

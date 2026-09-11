<script setup lang="ts">
import { computed } from 'vue'
import ChoiceSelect from './ChoiceSelect.vue'
const props = withDefaults(defineProps<{ modelValue: string; label: string; disabled?: boolean; seconds?: boolean }>(), { disabled: false, seconds: false })
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const hours = Array.from({ length: 24 }, (_, i) => ({ value: String(i).padStart(2, '0'), label: String(i).padStart(2, '0') + ' 时' }))
const minutes = Array.from({ length: 60 }, (_, i) => ({ value: String(i).padStart(2, '0'), label: String(i).padStart(2, '0') + ' 分' }))
const time = (h: string, m: string, s: string) => h + ':' + m + (props.seconds ? ':' + s : '')
const hour = computed({ get: () => props.modelValue.split(':')[0] || '09', set: value => emit('update:modelValue', time(value, minute.value, second.value)) })
const minute = computed({ get: () => props.modelValue.split(':')[1] || '00', set: value => emit('update:modelValue', time(hour.value, value, second.value)) })
const second = computed({ get: () => props.modelValue.split(':')[2] || '00', set: value => emit('update:modelValue', time(hour.value, minute.value, value)) })
const secondOptions = minutes.map(item => ({ ...item, label: item.value + ' 秒' }))
</script>
<template><div class="time-field" :class="{ seconds }" role="group" :aria-label="label"><ChoiceSelect v-model="hour" :label="label + ' · 小时'" :options="hours" :disabled="disabled" /><span aria-hidden="true">:</span><ChoiceSelect v-model="minute" :label="label + ' · 分钟'" :options="minutes" :disabled="disabled" /><template v-if="seconds"><span aria-hidden="true">:</span><ChoiceSelect v-model="second" :label="label + ' · 秒'" :options="secondOptions" :disabled="disabled" /></template></div></template>
<style scoped>.time-field{display:grid;grid-template-columns:minmax(0,1fr) auto minmax(0,1fr);align-items:center;gap:8px;min-width:0}.time-field.seconds{grid-template-columns:minmax(0,1fr) auto minmax(0,1fr) auto minmax(0,1fr);gap:5px}.time-field>span{color:#999782}</style>

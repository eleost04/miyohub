<script setup lang="ts">
import { autoSaveEnabled } from '../autosave'
defineProps<{ state: string; error?: string }>()
const labels: Record<string, string> = { saved: '已保存', waiting: '等待保存…', saving: '保存中…', invalid: '请补全配置后保存', manual: '有未保存修改', unsaved: '未保存，请重试' }
</script>
<template><div class="autosave-status"><label><input v-model="autoSaveEnabled" type="checkbox" aria-label="自动保存配置" />自动保存</label><span role="status" :class="{ 'error-ink': error }">{{ error ? '保存失败，请检查提示或重试' : labels[state] }}</span></div></template>

<script setup lang="ts">
import type { PushProvider } from '../types'
withDefaults(defineProps<{ name: PushProvider; size?: number }>(), { size: 22 })
// Original compact channel symbols; provider names carry the precise identity.
const paths: Record<PushProvider, string> = {
  pushplus: 'M5 18V5h6a4 4 0 0 1 0 8H5m12-1v8m-4-4h8',
  telegram: 'm3 10 17-6-5 16-5-7-7-3Zm7 3 6-5m-6 5v5l3-2',
  wxpusher: 'M12 4a8 8 0 0 0-8 8c0 2 1 4 2.5 5.5L5 21l4-1.5M12 4a8 8 0 0 1 8 8m-7 4h8m-3-3 3 3-3 3M8 10h.2m4 0h.2m4 0h.2',
  dingrobot: 'm5 6 13-2-5 6 5-1-8 12 1-8-5-2 6-2-7-3Z',
  feishubot: 'm4 5 8 6 7-6-3 9-4 6-4-7-4-8Zm8 6v9m0-9 6 3',
  qqbot: 'M7 8h10a3 3 0 0 1 3 3v6a3 3 0 0 1-3 3H7a3 3 0 0 1-3-3v-6a3 3 0 0 1 3-3Zm5 0V4m-2 0h4m-6 9h1m6 0h1m-7 4h6',
  qq: 'M5 8h14v11H5V8Zm7 0V4m-5 9h.2m9 0h.2m-7 4h6M2 11v5m20-5v5',
  email: 'M5 5h14a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2Zm-2 2 9 7 9-7',
  wechat_claw: 'M15 6c-1.2-1.3-3-2-5-2-4.2 0-7.5 2.6-7.5 6 0 2 1.1 3.8 3.1 4.8L4.5 18l4.3-2m10.7 1.5 1 3.5-4-2H16c-3.5 0-6.5-2-6.5-5s3-5 6.5-5 6.5 2 6.5 5c0 1.5-1 2.8-3 3.5ZM7 8.5h.2m4 0h.2m2 5h.2m4 0h.2',
  webhook: 'm7 6-5 6 5 6m10-12 5 6-5 6m-3-15-4 18',
}
</script>
<template><svg :width="size" :height="size" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false" class="app-icon"><path :d="paths[name] || paths.webhook" /></svg></template>

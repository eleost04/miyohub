<script setup lang="ts">
withDefaults(defineProps<{ name: string; size?: number }>(), { size: 20 })
const paths: Record<string, string> = {
  genshin: 'm12 3 2.5 6.5L21 12l-6.5 2.5L12 21l-2.5-6.5L3 12l6.5-2.5L12 3Zm0 6v6m-3-3h6',
  starrail: 'M6 5h12v12H6V5Zm0 4h12m-9 5h.2m5.6 0h.2M8 17l-3 4m11-4 3 4M9 3h6',
  zzz: 'M5 5h14l-7 7h7l-7 7H5l7-7H5l7-7',
  honkai3rd: 'm7 3 13 13-4 4L3 7l4-4Zm-2 2 13 13m-7-1 6-6m-9 9 3-3m-5 1 2 2',
  tears: 'M12 3v17m-4 0h8M5 7h14M7 7l-4 7h8L7 7Zm10 0-4 7h8l-4-7M3 14a4 3 0 0 0 8 0m2 0a4 3 0 0 0 8 0',
  honkai2: 'M12 6 4 3v15l8 3 8-3V3l-8 3Zm0 0v15M7 8l2 1m6 0 2-1m-10 4 2 1m6 0 2-1',
}
</script>
<template><svg :width="size" :height="size" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.55" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false" class="app-icon"><path :d="paths[name] || paths.genshin" /></svg></template>

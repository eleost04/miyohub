<script setup lang="ts">
withDefaults(defineProps<{ size?: number }>(), { size: 34 })
</script>
<template><svg :width="size" :height="size" viewBox="0 0 64 64" fill="none" aria-hidden="true" focusable="false" class="brand-symbol"><rect width="64" height="64" rx="20" fill="#303a30"/><path d="M32 11c3.5 12.5 8.5 17.5 21 21-12.5 3.5-17.5 8.5-21 21-3.5-12.5-8.5-17.5-21-21 12.5-3.5 17.5-8.5 21-21Z" fill="#f8f5ef"/><path d="m26 32 4 4 8-9" stroke="#708263" stroke-width="3.2" stroke-linecap="round" stroke-linejoin="round"/></svg></template>
<style scoped>.brand-symbol{display:inline-block;flex:0 0 auto;vertical-align:middle}</style>

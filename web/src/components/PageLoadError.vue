<script setup lang="ts">
defineProps<{ error?: Error }>()
function reload() { window.location.reload() }
</script>

<template>
  <section class="panel empty rich-empty" role="alert"><p>页面资源加载失败</p><small>请检查网络后重试。如果站点刚完成更新，重新加载即可获取新版本。</small><button class="small-button" @click="reload">重新加载页面</button></section>
</template>

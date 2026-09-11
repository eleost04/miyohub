<script setup lang="ts">
import { ref, watch } from 'vue'
import AppIcon from './AppIcon.vue'

const props = withDefaults(defineProps<{ src: string; name: string; loading?: 'eager' | 'lazy' }>(), { loading: 'lazy' })
const failed = ref(false)
watch(() => props.src, () => { failed.value = false })
</script>

<template>
  <div class="good-image">
    <img v-if="src && !failed" :src="src" :alt="name" :loading="loading" decoding="async" width="160" height="160" referrerpolicy="no-referrer" @error="failed = true" />
    <div v-else class="good-image-fallback" role="img" :aria-label="failed ? '商品图片暂不可用' : '暂无商品图片'"><AppIcon name="gift" :size="32" /><small v-if="failed">图片暂不可用</small></div>
  </div>
</template>

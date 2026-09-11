<script setup lang="ts">
import ModalShell from './ModalShell.vue'
defineProps<{ mobile: boolean; title: string; contentId: string; busy: boolean }>()
defineEmits<{ close: []; save: [] }>()
</script>
<template>
  <ModalShell v-if="mobile" :title="title" :busy="busy" @close="$emit('close')"><form @submit.prevent.stop="$emit('save')"><fieldset :disabled="busy" class="push-fields"><slot /></fieldset></form></ModalShell>
  <div v-else :id="contentId" class="push-channel-body"><slot /></div>
</template>

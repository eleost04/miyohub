<script setup lang="ts">
import { computed, ref } from 'vue'
import AppIcon from './AppIcon.vue'
import ChoiceSelect from './ChoiceSelect.vue'
import BrandMark from './BrandMark.vue'
import releaseData from '../releases.json'
import { appVersion } from '../version'

type ReleaseEntry = { commit: string; category: string; title: string; description: string }
type Release = { version: string; date: string; summary: string; entries: ReleaseEntry[] }
const releases: Release[] = releaseData

defineProps<{ serverVersion?: string }>()
const category = ref('all'), query = ref('')
const categories = [{ value: 'all', label: '全部更新' }, ...[...new Set(releases.flatMap(r => r.entries.map(e => e.category)))].map(value => ({ value, label: value }))]
const filtered = computed(() => releases.map(release => ({
  ...release,
  entries: release.entries.filter(entry => {
    const matchesCategory = category.value === 'all' || entry.category === category.value
    const text = [entry.title, entry.description, entry.commit].join(' ').toLowerCase()
    return matchesCategory && text.includes(query.value.trim().toLowerCase())
  }),
})).filter(release => release.entries.length || (category.value === 'all' && !query.value.trim())))
const channel = (version: string) => version.includes('-beta.') ? 'Beta 测试版' : version.includes('-rc.') ? '候选版' : '正式版'
</script>

<template>
  <section class="panel release-overview">
    <div class="release-brand"><BrandMark :size="44" /><div><p class="eyebrow">当前前端版本</p><h2>MiyoHub {{ appVersion }}</h2></div><span class="pill soft">{{ channel(appVersion) }}</span></div>
    <p class="muted">按版本查看功能改动、修复说明及关联提交。</p>
    <p v-if="serverVersion && serverVersion !== appVersion" class="error-banner" role="alert">后端为 {{ serverVersion }}，与当前前端版本不同。请刷新页面；若仍不一致，请管理员重新部署完整构建。</p>
    <div class="release-filters"><ChoiceSelect v-model="category" label="更新类型" :options="categories" /><label>搜索更新<input v-model="query" type="search" placeholder="功能、说明或提交编号" maxlength="100" /></label></div>
  </section>
  <section v-for="release in filtered" :key="release.version" class="panel release-panel">
    <div class="panel-title"><div><p class="eyebrow">{{ release.date }}</p><h2>v{{ release.version }}</h2></div><span class="pill">{{ channel(release.version) }}</span></div>
    <p class="muted">{{ release.summary }}</p>
    <p v-if="!release.entries.length" class="muted">此版本为功能基线，后续改动将逐项记录。</p>
    <ol class="release-entries"><li v-for="entry in release.entries" :key="entry.commit"><div class="release-entry-heading"><span class="pill soft">{{ entry.category }}</span><h3>{{ entry.title }}</h3></div><p>{{ entry.description }}</p><a :href="'https://github.com/eleost04/miyohub/commit/' + entry.commit" target="_blank" rel="noopener noreferrer" :aria-label="'查看提交 ' + entry.commit"><code>{{ entry.commit }}</code><AppIcon name="arrow" :size="13" /></a></li></ol>
  </section>
  <section v-if="!filtered.length" class="panel empty"><p>没有符合条件的更新记录。</p><button class="small-button" @click="category = 'all'; query = ''">清除筛选</button></section>
</template>

<style scoped>
.release-overview,.release-panel{margin-bottom:20px}.release-brand{display:flex;align-items:center;gap:14px}.release-brand>div{flex:1}.release-brand h2{font-size:21px;margin:3px 0}.release-overview>.muted{margin:16px 0}.release-filters{display:grid;grid-template-columns:180px 1fr;gap:16px;align-items:end}.release-filters label{display:grid;gap:8px;font-size:12px;color:#777c6c}.release-filters input{width:100%}.release-entries{list-style:none;padding:0;margin:20px 0 0;display:grid}.release-entries li{padding:20px 0;border-top:1px solid #e9e8df}.release-entry-heading{display:flex;align-items:center;gap:10px}.release-entry-heading .pill{flex-shrink:0}.release-entry-heading h3{font-size:15px;margin:0;line-height:1.6}.release-entries p{font-size:13px;line-height:1.9;color:#777c6c;margin:9px 0}.release-entries a{display:inline-flex;align-items:center;gap:7px;padding:5px 0;color:#65775b;text-underline-offset:3px}.release-entries code{font-size:12px}@media(max-width:640px){.release-filters{grid-template-columns:1fr}.release-brand{flex-wrap:wrap}.release-brand h2{font-size:19px}.release-entry-heading{align-items:flex-start}.release-entries li{padding:18px 0}.release-entries a{min-height:36px}}
</style>

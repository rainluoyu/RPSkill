<script setup lang="ts">
import { t } from "../lib/i18n"
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { useWorkbenchStore } from '../stores/workbench'
import { segment } from '../lib/types'
import type { Timeline, Perspective, Polishing } from '../lib/types'
import NarrativeFields from './NarrativeFields.vue'
import TimelineMedia from './TimelineMedia.vue'
const emit = defineEmits<{ 'dirty-change': [dirty: boolean] }>()
const s = useWorkbenchStore()
const mode = ref<'list' | 'editor'>('list'), editing = ref(''), search = ref(''), baseline = ref('')
const tab = ref<'settings' | 'media'>('settings')
const editor = ref<HTMLElement>()
const form = reactive({ name: '', perspective: 'third' as Perspective, polishing: 'basic' as Polishing })
const dirty = computed(() => mode.value === 'editor' && JSON.stringify(form) !== baseline.value)
const stories = computed(() => s.timelines.filter(t => t.name.toLocaleLowerCase().includes(search.value.trim().toLocaleLowerCase())))
const polishingNames = { off: "关闭润色", basic: "基础润色", full: "完全润色" }
watch(dirty, value => emit('dirty-change', value), { immediate: true })
async function open(story?: Timeline) {
  editing.value = story?.id || ''
  Object.assign(form, { name: story?.name || '', perspective: story?.narrative?.perspective || 'third', polishing: story?.polishing?.level || 'basic' })
  baseline.value = JSON.stringify(form); mode.value = 'editor'; tab.value = 'settings'
  await nextTick()
  editor.value?.querySelector<HTMLInputElement>('input')?.focus()
}
function back() {
  if (s.pending || (dirty.value && !confirm(t("放弃故事未保存的修改？")))) return
  mode.value = 'list'
}
async function save() {
  const name = form.name.trim()
  if (!name) { s.error = t("故事名称不能为空"); return }
  const id = editing.value || name
  const data = { name, narrative: { perspective: form.perspective }, polishing: { level: form.polishing } }
  if (await s.mutate(editing.value ? `/chats/${segment(id)}` : '/chats', editing.value ? 'PATCH' : 'POST', data)) {
    const created = !editing.value
    mode.value = 'list'
    if (created) await s.switchTimeline(name)
  }
}
async function remove(story: Timeline) {
  if (!confirm(t("永久删除故事“{0}”及其历史、附件？", [story.name]))) return
  if (await s.mutate(`/chats/${segment(story.id)}`, 'DELETE', {})) mode.value = 'list'
}
</script>
<template>
  <section>
    <template v-if="mode === 'list'">
      <div class="story-toolbar"><input v-model="search" type="search" :aria-label="t('搜索故事')" :placeholder="t('搜索故事')" /><button class="primary-button" :disabled="!!s.pending" @click="open()">{{ t("新建故事") }}</button></div>
      <ul class="story-list">
        <li v-for="story in stories" :key="story.id" :class="{ current: story.id === s.selectedTimeline }">
          <button class="story-summary" :aria-label="t('故事设置 {0}', [story.name])" :disabled="!!s.pending" @click="open(story)">
            <span class="story-title"><strong>{{ story.name }}</strong><span v-if="story.id === s.selectedTimeline" class="story-badge">{{ t("当前") }}</span></span>
            <span class="muted">{{ story.narrative?.perspective === 'first' ? t("第一人称") : t("第三人称") }} · {{ t(polishingNames[story.polishing?.level || 'basic']) }}</span>
          </button>
          <div class="story-tools"><button v-if="story.id !== s.selectedTimeline" :aria-label="t('切换故事 {0}', [story.name])" :disabled="!!s.pending || s.syncing" @click="s.switchTimeline(story.id)">{{ t("切换") }}</button><button class="danger" :aria-label="t('删除故事 {0}', [story.name])" :disabled="!!s.pending || s.timelines.length <= 1" @click="remove(story)">{{ t("删除") }}</button></div>
        </li>
      </ul>
      <p v-if="!stories.length" class="muted">{{ t("没有匹配的故事。") }}</p>
    </template>
    <section v-else ref="editor">
      <div class="story-toolbar"><button :disabled="!!s.pending" @click="back">{{ t("← 返回列表") }}</button><strong>{{ editing ? editing : t("新建故事") }}</strong></div>
      <nav v-if="editing" class="story-tabs" :aria-label="t('故事详情')">
        <button :aria-pressed="tab === 'settings'" :class="{ active: tab === 'settings' }" @click="tab = 'settings'">{{ t("故事设置") }}{{ dirty ? ' ·' : '' }}</button>
        <button :aria-pressed="tab === 'media'" :class="{ active: tab === 'media' }" @click="tab = 'media'">{{ t("媒体") }}</button>
      </nav>
      <form v-show="tab === 'settings'" @submit.prevent="save">
        <fieldset class="form-grid story-fields" :disabled="!!s.pending">
          <label>{{ t("故事名称") }}<input v-model="form.name" :aria-label="t('故事名称')" required /></label>
          <div class="two-columns"><NarrativeFields v-model:perspective="form.perspective" v-model:polishing="form.polishing" /></div>
          <div class="actions"><button class="primary-button" :disabled="!dirty">{{ editing ? t("保存故事") : t("创建故事") }}</button><button type="button" @click="back">{{ t("取消") }}</button></div>
        </fieldset>
      </form>
      <TimelineMedia v-if="editing && tab === 'media'" :timeline="editing" />
    </section>
  </section>
</template>
<style scoped>
.story-tabs { display: flex; gap: 8px; border-bottom: 1px solid var(--line); padding-bottom: 12px; margin-bottom: 20px; }
.story-tabs button { background: transparent; border-color: transparent; }
.story-tabs button.active { color: var(--accent); background: var(--surface); border-color: var(--line); }
.story-toolbar { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; margin-bottom: 16px; }
.story-toolbar > input { flex: 1 1 180px; }
.story-list { list-style: none; margin: 0; padding: 0; }
.story-list li { display: flex; align-items: center; gap: 12px; padding: 12px; border: 1px solid var(--line); border-radius: 8px; background: var(--surface); margin-bottom: 10px; }
.story-list li.current { border-color: #887455; }
.story-summary { display: grid; gap: 8px; flex: 1; min-width: 0; text-align: left; background: transparent; border-color: transparent; overflow-wrap: anywhere; }
.story-title { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
.story-badge { font-size: 11px; color: var(--accent); }
.story-tools { display: flex; gap: 8px; flex-shrink: 0; }
.story-fields { border: 0; padding: 0; margin: 0; min-width: 0; }
@media (max-width: 600px) { .story-list li { flex-wrap: wrap; } .story-summary { flex-basis: 100%; } .story-tools { margin-left: auto; } }
</style>

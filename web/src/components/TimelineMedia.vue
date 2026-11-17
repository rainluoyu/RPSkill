<script setup lang="ts">
import { t } from "../lib/i18n"
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { api } from '../lib/api'
import type { MediaRecord } from '../lib/types'
import { segment } from '../lib/types'
import { useWorkbenchStore } from '../stores/workbench'
import MediaView from './MediaView.vue'
const props = defineProps<{ timeline: string }>()
const s = useWorkbenchStore(), items = ref<MediaRecord[]>([]), loading = ref(false), search = ref('')
const filtered = computed(() => items.value.filter(media => media.name.toLocaleLowerCase().includes(search.value.trim().toLocaleLowerCase())))
let epoch = 0
async function refresh() {
  const current = ++epoch, id = props.timeline
  if (!id) return
  loading.value = true
  try { const records = await api<MediaRecord[]>(`/media?timeline=${segment(id)}`); if (current === epoch) items.value = records ?? [] }
  catch (e) { if (current === epoch) s.report(e) } finally { if (current === epoch) loading.value = false }
}
watch(() => props.timeline, () => { epoch++; items.value = []; search.value = ''; loading.value = false; void refresh() }, { immediate: true })
onBeforeUnmount(() => { epoch++ })
async function remove(media: MediaRecord) {
  if (!confirm(t("永久删除附件 {0}？已被历史引用的附件不可删除。", [media.name]))) return
  if (await s.mutate(`/media/${segment(media.id)}`, 'DELETE', {})) { s.removeAttachment(media.id, props.timeline); await refresh() }
}
</script>
<template>
  <section :aria-label="t('当前故事媒体管理')">
    <div class="files-toolbar"><input v-model="search" type="search" :aria-label="t('搜索媒体文件')" :placeholder="t('搜索文件')" /><span class="muted">{{ t("{0} 个文件", [filtered.length]) }}</span><button type="button" :disabled="loading" @click="refresh">{{ t("刷新") }}</button></div>
    <p v-if="loading" role="status">{{ t("正在加载…") }}</p>
    <p v-else-if="!filtered.length" class="muted">{{ search.trim() ? t("没有匹配的文件。") : t("当前故事暂无媒体。") }}</p>
    <div class="file-grid" :aria-busy="loading">
      <MediaView v-for="media in filtered" :key="media.id" :media="media" tile>
        <template #actions="{ close }">
          <button type="button" :disabled="!!s.pending || s.timelines.find(t => t.id === timeline)?.archived" @click="s.attachExisting(media, timeline); close()">{{ t("附加到该故事提示") }}</button>
          <button type="button" class="danger" :disabled="!!s.pending" :aria-label="t('删除附件 {0}', [media.name])" @click="remove(media)">{{ t("删除附件") }}</button>
        </template>
      </MediaView>
    </div>
  </section>
</template>
<style scoped>
.files-toolbar { display: flex; align-items: center; gap: 12px; margin-bottom: 18px; flex-wrap: wrap; }
.files-toolbar input { flex: 1 1 160px; min-width: 0; }
.files-toolbar span { font-size: 12px; }
.file-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(132px, 1fr)); gap: 12px; }
@media (max-width: 480px) { .file-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; } }
</style>

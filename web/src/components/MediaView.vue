<script setup lang="ts">
import { t } from "../lib/i18n"
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import type { MediaRef } from '../lib/types'
import { mediaURL, previewKind } from '../lib/media'
import { download } from '../lib/api'
import { useWorkbenchStore } from '../stores/workbench'
const props = withDefaults(defineProps<{ media: MediaRef; tile?: boolean }>(), { tile: false })
const s = useWorkbenchStore(), failed = ref(false), saving = ref(false), opened = ref(false)
const dialog = ref<HTMLDialogElement>(), trigger = ref<HTMLButtonElement>()
const url = computed(() => mediaURL(props.media)), kind = computed(() => previewKind(props.media.mime))
const label = computed(() => kind.value === 'image' ? t("图片") : kind.value === 'video' ? t("视频") : kind.value === 'audio' ? t("音频") : t("附件"))
const symbol = computed(() => kind.value === 'image' ? '▧' : kind.value === 'video' ? '▶' : kind.value === 'audio' ? '♫' : '▤')
async function show() {
  opened.value = true
  await nextTick()
  dialog.value?.showModal()
}
function close() {
  dialog.value?.close()
  opened.value = false
  trigger.value?.focus()
}
function backdrop(event: MouseEvent) {
  if (event.target !== dialog.value) return
  const rect = dialog.value.getBoundingClientRect()
  if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) close()
}
watch(() => props.media.id, () => { close(); failed.value = false })
onBeforeUnmount(() => { dialog.value?.close() })
async function save() {
  saving.value = true
  try { await download(`/media/${encodeURIComponent(props.media.id)}?download=1`, props.media.name, 'GET', '') }
  catch (e) { s.report(e) } finally { saving.value = false }
}
</script>
<template>
  <figure class="media-item" :class="{ 'media-tile': tile, 'media-visual': kind === 'image' || kind === 'video' }" :data-media-id="media.id">
    <button ref="trigger" type="button" class="media-preview" :class="{ 'media-placeholder': !url || failed || !kind || kind === 'audio' }" :aria-label="t('打开{0} {1}', [label, media.name])" aria-haspopup="dialog" @click="show">
      <img v-if="url && !failed && kind === 'image'" :src="url" :alt="media.name" loading="lazy" @error="failed = true" />
      <video v-else-if="url && !failed && kind === 'video'" :src="url" muted playsinline preload="metadata" :aria-label="t('视频 {0}', [media.name])" @error="failed = true" />
      <span v-else class="media-file-icon" aria-hidden="true">{{ symbol }}</span>
      <span v-if="kind === 'video' && url && !failed" class="media-play" aria-hidden="true">▶</span>
      <span v-if="tile" class="media-filename" :title="media.name">{{ media.name }}</span>
      <span v-else-if="!url || failed || !kind || kind === 'audio'" class="media-kind">{{ label }}</span>
    </button>
    <Teleport to="body">
      <dialog v-if="opened" ref="dialog" class="media-dialog" :aria-label="t('附件详情 {0}', [media.name])" @cancel.prevent="close" @click="backdrop" @keydown.stop>
        <header class="media-dialog-header"><strong>{{ label }}</strong><button type="button" :aria-label="t('关闭附件详情')" autofocus @click="close">{{ t("关闭 ×") }}</button></header>
        <div v-if="url && !failed && kind" class="media-dialog-preview">
          <img v-if="kind === 'image'" :src="url" :alt="media.name" @error="failed = true" />
          <audio v-else-if="kind === 'audio'" :src="url" controls preload="metadata" :aria-label="t('播放音频 {0}', [media.name])" @error="failed = true" />
          <video v-else-if="kind === 'video'" :src="url" controls playsinline preload="metadata" :aria-label="t('播放视频 {0}', [media.name])" @error="failed = true" />
        </div>
        <p v-if="failed || !url" class="muted">{{ t("预览暂不可用。") }}</p>
        <div class="media-details"><strong>{{ media.name }}</strong><span class="muted">{{ media.mime }}</span></div>
        <div class="actions media-dialog-actions">
          <button type="button" :disabled="saving || !url" :aria-label="t('下载附件 {0}', [media.name])" @click="save">{{ saving ? t("下载中…") : t("下载附件") }}</button>
          <slot name="actions" :close="close" />
        </div>
      </dialog>
    </Teleport>
  </figure>
</template>
<style scoped>
.media-item { margin: 0; min-width: 0; max-width: 100%; }
.media-visual:not(.media-tile) { width: min(100%, 560px); }
.media-visual .media-preview { width: 100%; min-height: 80px; }
.media-preview { display: block; position: relative; padding: 0; border: 0; border-radius: 8px; overflow: hidden; background: transparent; max-width: 100%; cursor: zoom-in; color: var(--text, #e2ddd4); }
.media-preview img, .media-preview video { display: block; width: 100%; min-height: 80px; max-width: 100%; max-height: 440px; object-fit: contain; }
.media-placeholder { display: flex; align-items: center; gap: 12px; padding: 16px 20px; background: var(--surface); cursor: pointer; }
.media-file-icon { font-size: 32px; line-height: 1; color: var(--accent); }
.media-kind { font-size: 13px; }
.media-play { position: absolute; left: 50%; top: 50%; transform: translate(-50%, -50%); padding: 12px 16px; border-radius: 50%; background: #0009; color: white; pointer-events: none; }
.media-tile .media-preview { width: 100%; aspect-ratio: 1; display: flex; align-items: center; justify-content: center; padding: 0; background: var(--surface); border: 1px solid var(--line); cursor: pointer; }
.media-tile .media-preview:hover { border-color: var(--accent); }
.media-tile img, .media-tile video { width: 100%; height: 100%; max-height: none; object-fit: cover; }
.media-tile .media-file-icon { margin-bottom: 24px; font-size: 40px; }
.media-filename { position: absolute; left: 0; right: 0; bottom: 0; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; padding: 8px; font-size: 12px; background: #171920e8; color: #eee; text-align: left; }
.media-dialog { width: min(520px, calc(100vw - 32px)); max-height: calc(100dvh - 40px); box-sizing: border-box; padding: 18px; margin: auto; overflow-y: auto; overscroll-behavior: contain; border: 1px solid var(--line); border-radius: 12px; background: #20232b; color: var(--text, #e2ddd4); box-shadow: 0 20px 70px #0008; }
.media-dialog::backdrop { background: #0009; }
.media-dialog-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 14px; }
.media-dialog-preview img, .media-dialog-preview video { display: block; max-width: 100%; max-height: 45dvh; margin: auto; object-fit: contain; }
.media-dialog-preview audio { display: block; width: 100%; }
.media-details { display: grid; gap: 6px; margin-top: 16px; overflow-wrap: anywhere; font-size: 13px; }
.media-dialog-actions { flex-wrap: wrap; margin-top: 16px; }
</style>

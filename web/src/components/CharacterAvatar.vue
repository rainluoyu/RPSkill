<script setup lang="ts">
import { t } from "../lib/i18n"
import { computed, ref, watch } from 'vue'
import { api, upload } from '../lib/api'
import type { MediaRecord } from '../lib/types'
import { segment } from '../lib/types'
import { mediaURL } from '../lib/media'
import { useWorkbenchStore } from '../stores/workbench'
const props = defineProps<{ character: string; avatar?: string; cardDirty?: boolean }>()
const emit = defineEmits<{ uploaded: []; remove: []; 'dirty-change': [dirty: boolean] }>()
const s = useWorkbenchStore(), items = ref<MediaRecord[]>([]), file = ref<File>(), loading = ref(false), failed = ref(false)
const input = ref<HTMLInputElement>()
const currentAvatar = computed(() => items.value.find(item => item.path === props.avatar))
watch(file, value => emit('dirty-change', !!value))
let epoch = 0
async function refresh() {
  const current = ++epoch, character = props.character
  loading.value = true; failed.value = false
  try { const records = await api<MediaRecord[]>(`/media?character=${segment(character)}`); if (current === epoch) items.value = records ?? [] }
  catch (e) { if (current === epoch) s.report(e) } finally { if (current === epoch) loading.value = false }
}
function clear() { file.value = undefined; if (input.value) input.value.value = '' }
watch(() => props.character, () => { items.value = []; clear(); void refresh() }, { immediate: true })
watch(() => props.avatar, () => { void refresh() })
async function send() {
  if (!file.value || s.pending || props.cardDirty) return
  const character = props.character
  s.pending++; s.error = ''
  try {
    await upload<MediaRecord>('/media', file.value, { character, avatar: 'true' })
    await s.sync()
    if (character === props.character) { clear(); await refresh(); emit('uploaded') }
  } catch (e) { s.report(e) } finally { s.pending-- }
}
</script>
<template>
  <section class="avatar-editor" :aria-label="t('角色头像管理')">
    <div class="avatar-current">
      <div class="avatar-preview">
        <img v-if="currentAvatar && !failed" :src="mediaURL(currentAvatar)" :alt="t('{0}的头像', [character])" @error="failed = true" />
        <span v-else class="muted">{{ loading ? t("正在加载…") : avatar ? t("头像暂不可用") : t("暂无头像") }}</span>
      </div>
      <button v-if="avatar" type="button" class="danger" :disabled="!!s.pending || cardDirty" @click="emit('remove')">{{ t("移除头像") }}</button>
    </div>
    <form class="form-grid avatar-upload" @submit.prevent="send">
      <label>{{ t("选择头像") }}<input ref="input" type="file" accept="image/png,image/jpeg,image/gif,image/webp" required :disabled="!!s.pending" @change="file = ($event.target as HTMLInputElement).files?.[0]" /></label>
      <p class="muted">{{ t("PNG、JPEG、GIF、WebP，最多 32 MiB。") }}</p>
      <p v-if="cardDirty" class="muted">{{ t("请先保存基础设定，再修改头像。") }}</p>
      <div class="actions"><button class="primary-button" :disabled="!!s.pending || !file || cardDirty">{{ avatar ? t("替换头像") : t("上传头像") }}</button><button v-if="file" type="button" :disabled="!!s.pending" @click="clear">{{ t("清除选择") }}</button></div>
    </form>
  </section>
</template>
<style scoped>
.avatar-editor { display: grid; grid-template-columns: 180px minmax(0, 1fr); gap: 24px; align-items: start; }
.avatar-current { display: grid; justify-items: center; gap: 12px; }
.avatar-preview { width: 180px; height: 180px; display: grid; place-items: center; overflow: hidden; border: 1px solid var(--line); border-radius: 12px; background: var(--surface); }
.avatar-preview img { display: block; width: 100%; height: 100%; object-fit: contain; }
.avatar-upload { min-width: 0; }
.avatar-upload .actions { margin-top: 0; }
@media (max-width: 600px) { .avatar-editor { grid-template-columns: minmax(0, 1fr); gap: 20px; } }
</style>

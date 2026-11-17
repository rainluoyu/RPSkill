<script setup lang="ts">
import { t } from "../lib/i18n"
import { computed, ref, watch } from 'vue'
import { useWorkbenchStore } from '../stores/workbench'
import MediaView from './MediaView.vue'
import CharacterPortrait from './CharacterPortrait.vue'
const s = useWorkbenchStore(), file = ref<File>(), input = ref<HTMLInputElement>()
const character = computed(() => s.enabledCharacters.find(c => c.metadata.id === s.inputIdentity))
const identityName = computed(() => character.value?.metadata.name || (s.inputIdentity === 'chat' ? t("交流") : t("导演")))
const uploadDisabled = computed(() => !!s.pending || !s.state || !!s.timeline?.archived)
async function retryUpload() {
  if (uploadDisabled.value || !file.value) return
  const selected = file.value
  if (await s.uploadAttachment(selected) && file.value === selected) {
    file.value = undefined; if (input.value) input.value.value = ''
  }
}
async function attach(event: Event) { file.value = (event.target as HTMLInputElement).files?.[0]; await retryUpload() }
watch(() => s.selectedTimeline, () => { file.value = undefined; if (input.value) input.value.value = '' })
function shortcut(e: KeyboardEvent) { if ((e.ctrlKey || e.metaKey) && e.key === 'Enter' && !e.isComposing) { e.preventDefault(); void s.send() } }
</script>
<template>
  <div class="composer-dock">
    <form class="composer" :aria-label="t('发送创作提示')" @submit.prevent="s.send()">
      <aside class="composer-identity" :aria-label="t('当前输入角色')">
        <CharacterPortrait class="identity-avatar" :identity="s.inputIdentity" />
        <label class="identity-picker"><span class="sr-only">{{ t("输入身份") }}</span><select v-model="s.inputIdentity" :aria-label="t('输入身份')" :title="identityName"><option value="director">{{ t("导演") }}</option><option value="chat">{{ t("交流") }}</option><option v-for="c in s.enabledCharacters" :key="c.metadata.id" :value="c.metadata.id">{{ c.metadata.name }}</option></select></label>
      </aside>
      <div class="composer-writing">
      <label class="draft-label"><span class="sr-only">{{ t("创作提示") }}</span><textarea v-model="s.draft" :aria-label="t('创作提示')" rows="3" :required="!s.draftMedia.length" :placeholder="t('写下行动、场景或想与 AI 交流的内容……')" @keydown="shortcut" /></label>
      <div v-if="s.draftMedia.length" class="draft-attachments" :aria-label="t('待发送附件')"><div v-for="media in s.draftMedia" :key="media.id"><MediaView :media="media" /><button type="button" :aria-label="t('移除待发送附件 {0}', [media.name])" @click="s.removeAttachment(media.id)">{{ t("移除附件") }}</button></div></div>
      <div class="composer-footer">
        <input ref="input" class="sr-only" type="file" tabindex="-1" :aria-label="t('上传剧情附件')" :disabled="uploadDisabled" @change="attach" />
        <button type="button" class="attachment-button" :disabled="uploadDisabled" :aria-label="t('添加附件')" @click="input?.click()"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m8 12 6-6a3 3 0 0 1 4 4l-8 8a5 5 0 0 1-7-7l9-9M6 14l8-8" /></svg><span>{{ t("添加附件") }}</span></button>
        <button v-if="file && !s.pending" type="button" class="attachment-retry" :disabled="uploadDisabled" @click="retryUpload">{{ t("重试上传") }}</button>
        <span class="composer-hint muted">{{ s.timeline?.archived ? t("本故事已归档") : 'Ctrl / ⌘ + Enter' }}</span>
        <button class="primary-button" :disabled="!!s.pending || (!s.draft.trim() && !s.draftMedia.length) || !s.state || s.timeline?.archived">{{ t("发送提示") }}</button>
      </div>
      </div>
    </form>
  </div>
</template>

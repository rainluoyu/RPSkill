<script setup lang="ts">
import { t } from "../lib/i18n"
import { computed, ref, watch } from 'vue'
import BuiltinPortrait from './BuiltinPortrait.vue'
import { useWorkbenchStore } from '../stores/workbench'
const props = defineProps<{ identity: string; clickable?: boolean }>()
const s = useWorkbenchStore(), url = ref(''), failed = ref(false)
const card = computed(() => s.characters.find(c => c.metadata.id === props.identity))
const name = computed(() => card.value?.metadata.name || (props.identity === 'chat' ? t("交流") : props.identity === 'director' ? t("导演") : props.identity))
const mark = computed(() => Array.from(name.value)[0])
const linked = computed(() => props.clickable && !!card.value)
watch(() => [props.identity, card.value?.sha256, card.value?.metadata.avatar], async (_, __, onCleanup) => {
  let stale = false; onCleanup(() => { stale = true })
  url.value = ''; failed.value = false
  const value = await s.avatarFor(props.identity)
  if (!stale) url.value = value
}, { immediate: true })
</script>
<template>
  <component :is="linked ? 'button' : 'span'" class="character-portrait" :type="linked ? 'button' : undefined" :title="name" :aria-label="linked ? t('查看人物卡 {0}', [name]) : undefined" @click="linked && s.openCharacter(identity)">
    <BuiltinPortrait v-if="identity === 'director' || identity === 'chat'" :identity="identity" /><img v-else-if="url && !failed" :src="url" :alt="t('{0}的头像', [name])" @error="failed = true" />
    <span v-else class="identity-placeholder" role="img" :aria-label="t('{0}的默认头像', [name])">{{ mark }}</span>
  </component>
</template>
<style scoped>
.character-portrait { display: inline-grid; position: relative; place-items: center; flex-shrink: 0; width: 44px; height: 44px; padding: 0; border: 1px solid #62533f; border-radius: 9px; overflow: hidden; background: #35312e; color: var(--accent); }
.character-portrait img { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; }
.identity-placeholder { font-size: 22px; }
.character-portrait.identity-avatar { width: 100%; height: auto; aspect-ratio: 1; }
</style>

<script setup lang="ts">
import { t } from "../lib/i18n"
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import CharacterPortrait from './CharacterPortrait.vue'
const props = defineProps<{ identities: string[] }>()
const names = computed(() => [...new Set(props.identities)])
const index = ref(0), hovered = ref(false), focused = ref(false)
const many = computed(() => names.value.length > 4)
const visible = computed(() => many.value ? Array.from({ length: 4 }, (_, i) => names.value[(index.value + i) % names.value.length]!) : names.value)
function move(step: number) { index.value = (index.value + step + names.value.length) % names.value.length }
let timer: ReturnType<typeof setInterval> | undefined
watch(() => props.identities, () => {
  index.value = 0; if (timer) clearInterval(timer)
  if (many.value) timer = setInterval(() => {
    if (!hovered.value && !focused.value && !document.hidden && !window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) move(1)
  }, 4000)
}, { immediate: true })
onBeforeUnmount(() => { if (timer) clearInterval(timer) })
</script>
<template>
  <div class="character-strip" :aria-label="t('输出涉及角色')" @mouseenter="hovered = true" @mouseleave="hovered = false" @focusin="focused = true" @focusout="focused = !!($event.relatedTarget && ($event.currentTarget as HTMLElement).contains($event.relatedTarget as Node))">
    <span class="cast-label">{{ t("涉及角色") }}</span>
    <button v-if="many" class="cast-arrow" :aria-label="t('上一组角色')" @click="move(-1)">‹</button>
    <div class="cast-portraits" aria-live="off"><CharacterPortrait v-for="name in visible" :key="name" :identity="name" clickable /></div>
    <button v-if="many" class="cast-arrow" :aria-label="t('下一组角色')" @click="move(1)">›</button>
  </div>
</template>
<style scoped>
.character-strip { display: flex; align-items: center; gap: 8px; margin: 12px 0; min-width: 0; flex-wrap: wrap; }
.cast-label { font-size: 11px; color: var(--muted); }
.cast-portraits { display: flex; gap: 6px; }
.cast-arrow { padding: 5px; font-size: 20px; border: 0; background: transparent; }
@media (max-width: 600px) { .character-strip { gap: 4px; } .cast-label { flex-basis: 100%; } .cast-portraits :deep(.character-portrait) { width: 38px; height: 38px; } }
</style>

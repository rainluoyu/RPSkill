<script setup lang="ts">
import { t } from "../lib/i18n"
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useWorkbenchStore } from '../stores/workbench'
const s = useWorkbenchStore(), popup = ref<HTMLElement>(), host = ref<HTMLElement | string>('body')
let modal: HTMLDialogElement | null = null
function detach() { modal?.removeEventListener('close', rehost); modal = null }
async function show() {
  await nextTick()
  if (s.error && popup.value?.isConnected) popup.value.showPopover?.()
}
function rehost() { detach(); host.value = 'body'; void show() }
let timer: ReturnType<typeof setTimeout> | undefined
function pause() { if (timer) clearTimeout(timer) }
function resume() { pause(); timer = setTimeout(() => { s.error = '' }, 6000) }
watch(() => s.error, async error => {
  pause(); detach()
  if (!error) return
  // A snackbar outside a native modal would be inert even in the top layer.
  modal = document.querySelector<HTMLDialogElement>('dialog[open]')
  host.value = modal || 'body'
  modal?.addEventListener('close', rehost, { once: true })
  await show()
  if (s.error === error) resume()
}, { immediate: true })
onBeforeUnmount(() => { pause(); detach() })
</script>
<template>
  <Teleport :to="host"><div v-if="s.error" ref="popup" class="error-snackbar" popover="manual" role="alert" @mouseenter="pause" @mouseleave="resume" @focusin="pause" @focusout="resume"><span>{{ s.error }}</span><button type="button" :aria-label="t('关闭错误')" @click="s.error = ''">×</button></div></Teleport>
</template>
<style scoped>
.error-snackbar { position: fixed; inset: auto; left: 50%; bottom: max(24px, env(safe-area-inset-bottom)); transform: translateX(-50%); margin: 0; padding: 12px 16px; width: max-content; max-width: min(620px, calc(100vw - 24px)); border: 1px solid #925c56; border-radius: 9px; background: #452d2d; color: #ffded8; box-shadow: 0 6px 30px #0008; z-index: 1000; font-size: 13px; }
.error-snackbar:popover-open { display: flex; align-items: center; gap: 14px; }
.error-snackbar span { overflow-wrap: anywhere; white-space: pre-wrap; }
.error-snackbar button { flex-shrink: 0; background: transparent; border: 0; color: inherit; }
</style>

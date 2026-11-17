<script setup lang="ts">
import { t } from "./lib/i18n"
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { panels, useWorkbenchStore } from './stores/workbench'
import ChatPanel from './components/ChatPanel.vue'
import Composer from './components/Composer.vue'
import CharacterPanel from './components/CharacterPanel.vue'
import WorldbookPanel from './components/WorldbookPanel.vue'
import TimelinePanel from './components/TimelinePanel.vue'
import ErrorSnackbar from './components/ErrorSnackbar.vue'
import LanguagePicker from './components/LanguagePicker.vue'
import ThemePanel from './components/ThemePanel.vue'
const s = useWorkbenchStore(), token = ref(''), modal = ref<HTMLElement>()
const currentPanel = computed(() => panels.find((p) => p.id === s.activePanel)!)
let previousFocus: HTMLElement | null = null
const open = computed(() => s.activePanel !== 'writing')
async function login() { const secret = token.value; token.value = ''; await s.bootstrap(secret) }
function close() { s.activePanel = 'writing' }
function confirmDiscard() {
  // A native confirmation protects unsaved management edits from incidental
  // closure. No draft text is copied into persistent browser storage.
  return modal.value?.dataset.unsaved !== 'true' || confirm(t("关闭弹窗会丢弃未保存的编辑，仍要继续吗？"))
}
function navigate(id: typeof s.activePanel) { s.activePanel = id }
let reverting = false
watch(() => s.activePanel, (value, before) => {
  if (reverting || value === before || before === 'writing') return
  if (!confirmDiscard()) { reverting = true; s.activePanel = before; reverting = false }
}, { flush: 'sync' })
function keydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && open.value) { e.preventDefault(); close(); return }
  if (e.key === 'Tab' && open.value) {
    const items = [...(modal.value?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled):not([type=hidden]),select:not(:disabled),textarea:not(:disabled),a[href],summary,[tabindex]:not([tabindex="-1"])') ?? [])].filter((el) => el.getClientRects().length)
    const first = items[0], last = items.at(-1)
    if (!first) { e.preventDefault(); modal.value?.focus() }
    else if (!modal.value?.contains(document.activeElement) || document.activeElement === modal.value) { e.preventDefault(); (e.shiftKey ? last : first)?.focus() }
    else if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last?.focus() }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first?.focus() }
  }
}
watch(() => [s.activePanel, s.selectedTimeline] as const, async ([value], [before]) => {
  if (before === 'writing' && value !== 'writing') previousFocus = document.activeElement as HTMLElement
  await nextTick()
  if (value !== 'writing') modal.value?.querySelector<HTMLElement>('.modal-close')?.focus()
  else previousFocus?.focus()
})
function resourceDirty(dirty: boolean) {
  if (modal.value) {
    if (dirty) modal.value.dataset.unsaved = 'true'
    else delete modal.value.dataset.unsaved
  }
}
onBeforeUnmount(() => s.dispose())
</script>
<template>
  <div v-if="!s.authenticated" class="login-shell"><section class="panel login-panel"><LanguagePicker /><h1>{{ t("进入你的故事世界") }}</h1><p v-if="s.booting" role="status">{{ t("正在检查本地会话……") }}</p><form v-else class="form-grid" @submit.prevent="login"><label>{{ t("访问令牌") }}<input v-model="token" :aria-label="t('访问令牌')" type="password" autocomplete="off" spellcheck="false" required /></label><button class="primary-button" :disabled="!token.trim()">{{ t("登录") }}</button></form><p class="muted">{{ t("使用启动服务时提供的令牌或访问链接登录。") }}</p></section></div>
  <div v-else class="workbench" @keydown="keydown">
    <div class="workbench-content" :inert="open ? true : undefined">
    <a class="skip-link" href="#main-content">{{ t("跳到剧情") }}</a>
    <header class="topbar"><div class="brand"><span aria-hidden="true">✦</span><strong>RPSkill</strong><span class="project-name">{{ s.project?.name }}</span></div>
      <nav :aria-label="t('主导航')"><button v-for="p in panels" :key="p.id" type="button" :class="{ active: s.activePanel === p.id }" :aria-pressed="s.activePanel === p.id" @click="navigate(p.id)"><span aria-hidden="true">{{ p.mark }}</span><span>{{ t(p.label) }}</span></button></nav>
    </header>
    <div class="workspace-body">
      <main id="main-content" tabindex="-1"><h1 class="sr-only">{{ t("剧情") }}</h1>
        <ChatPanel /><Composer />
      </main>
    </div>
    </div>
    <div v-if="open" class="modal-backdrop" @click.self="close()">
      <section :key="s.activePanel + '/' + s.selectedTimeline" ref="modal" class="management-modal" :class="s.activePanel === 'theme' ? 'modal-theme' : 'modal-wide'" role="dialog" aria-modal="true" aria-labelledby="management-title" tabindex="-1">
        <header class="modal-header"><div><h1 id="management-title">{{ t(currentPanel.label) }}</h1></div><button class="modal-close" :aria-label="t('关闭面板')" @click="close()">{{ t("关闭 ×") }}</button></header>
        <div class="modal-content">
          <WorldbookPanel v-if="s.activePanel === 'worldbook'" @dirty-change="resourceDirty" /><CharacterPanel v-else-if="s.activePanel === 'characters'" @dirty-change="resourceDirty" /><TimelinePanel v-else-if="s.activePanel === 'chats'" @dirty-change="resourceDirty" /><ThemePanel v-else-if="s.activePanel === 'theme'" /></div>
      </section>
    </div>
  </div>
  <ErrorSnackbar />
</template>

<style scoped>
.modal-theme { width: min(440px, calc(100vw - 32px)); }
</style>

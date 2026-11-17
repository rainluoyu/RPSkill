<script setup lang="ts">
import { t, formatDate } from "../lib/i18n"
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useWorkbenchStore } from '../stores/workbench'
import { segment } from '../lib/types'
import type { RPEvent } from '../lib/types'
import MediaView from './MediaView.vue'
import CharacterPortrait from './CharacterPortrait.vue'
import CharacterStrip from './CharacterStrip.vue'
import MarkdownContent from './MarkdownContent.vue'
const s = useWorkbenchStore()
const polishingLabels = { off: "关闭润色", basic: "基础润色", full: "完全润色" } as const
const edits = reactive(new Map<string, { id: string; position: string; text: string; original: string }>())
const edit = computed(() => edits.get(s.selectedTimeline))
const branches = reactive(new Map<string, { id: string; name: string }>())
const branch = computed(() => branches.get(s.selectedTimeline))
function beginBranch(event: RPEvent) {
  branches.set(s.selectedTimeline, { id: event.id, name: t("{0} · 分支", [s.timeline?.name || t("故事")]) })
}
async function saveBranch() {
  const line = s.selectedTimeline, draft = branches.get(line)
  if (!draft || s.pending) return
  if (await s.branchFrom(draft.id, draft.name)) branches.delete(line)
}
const historyLocked = computed(() => !!s.pending || !!s.timeline?.archived)
function beginEdit(event: RPEvent) {
  if (edit.value && edit.value.text !== edit.value.original && !confirm(t("放弃当前未保存的正文修改？"))) return
  edits.set(s.selectedTimeline, { id: event.id, position: String(event.position ?? event.id), text: event.text || '', original: event.text || '' })
}
function cancelEdit() {
  if (edit.value && edit.value.text !== edit.value.original && !confirm(t("放弃未保存的正文修改？"))) return
  edits.delete(s.selectedTimeline)
}
async function saveEdit() {
  const line = s.selectedTimeline, draft = edits.get(line)
  if (!draft || historyLocked.value) return
  if (await s.mutate(`/chats/${segment(line)}/events/${segment(draft.position)}`, 'PATCH', { text: draft.text })) {
    if (edits.get(line) === draft) edits.delete(line)
  }
}
async function remove(event: RPEvent) {
  const warning = t("永久删除从最新消息到这条消息之间的全部内容（包含本条）？角色状态文档和附件文件保留，其他故事不受影响。")
  if (historyLocked.value || !confirm(warning)) return
  const line = s.selectedTimeline
  if (await s.mutate(`/chats/${segment(line)}/events/${segment(String(event.position ?? event.id))}`, 'DELETE', { truncate: true })) {
    edits.delete(line); branches.delete(line)
  }
}
function beforeUnload(event: BeforeUnloadEvent) {
  if (Array.from(edits.values()).some(edit => edit.text !== edit.original)) { event.preventDefault(); event.returnValue = '' }
}
onMounted(() => window.addEventListener('beforeunload', beforeUnload))
onBeforeUnmount(() => window.removeEventListener('beforeunload', beforeUnload))
const visible = computed(() => s.displayEvents)
const scroller = ref<HTMLElement>()
const atLatest = ref(true)
let resizeObserver: ResizeObserver | undefined
let previousTop = 0, restoring = false, pinned = true
function bottom() {
  const el = scroller.value
  if (el) { el.scrollTop = el.scrollHeight; previousTop = el.scrollTop; pinned = true; atLatest.value = true }
}
function measurePosition() {
  const el = scroller.value
  if (el) atLatest.value = el.scrollHeight - el.scrollTop - el.clientHeight <= 4
}
onMounted(() => {
  if (typeof ResizeObserver === 'undefined' || !scroller.value) return
  resizeObserver = new ResizeObserver(() => {
    if (restoring) return
    if (pinned && !s.loadingHistory) bottom()
    else measurePosition()
  })
  resizeObserver.observe(scroller.value)
  const history = scroller.value.querySelector('.history')
  if (history) resizeObserver.observe(history)
})
onBeforeUnmount(() => resizeObserver?.disconnect())
function onScroll() {
  const el = scroller.value
  if (!el || restoring) return
  const upward = el.scrollTop < previousTop
  previousTop = el.scrollTop
  pinned = el.scrollHeight - el.scrollTop - el.clientHeight < 80
  measurePosition()
  if (upward && el.scrollTop < 120) void s.loadMore()
}
watch(() => s.events, async (items, previous) => {
  const el = scroller.value
  if (!el) return
  const line = s.selectedTimeline
  const anchor = [...el.querySelectorAll<HTMLElement>('[data-event-id]')].find(node => node.getBoundingClientRect().bottom > el.getBoundingClientRect().top)
  const offset = anchor?.getBoundingClientRect().top
  const prepend = !!previous[0] && items[0]?.id !== previous[0]?.id && items.some(e => e.id === previous[0]?.id)
  const follow = !previous.length || (pinned && !prepend)
  restoring = true
  await nextTick()
  if (line === s.selectedTimeline) {
    if (follow) bottom()
    else if (anchor?.isConnected && offset !== undefined) el.scrollTop += anchor.getBoundingClientRect().top - offset
    else el.scrollTop = Math.min(previousTop, el.scrollHeight)
    previousTop = el.scrollTop
    measurePosition()
  }
  restoring = false
}, { flush: 'pre' })
watch(() => s.scrollToLatest, async () => { await nextTick(); bottom() })
// Late-loading media must not push the latest message below the viewport.
function mediaLoaded() { if (pinned && !s.loadingHistory) bottom(); else measurePosition() }

const label = (e: RPEvent) => e.type === 'input' ? s.characters.find((c) => c.metadata.id === e.speaker)?.metadata.name || (e.speaker === 'chat' ? t("交流") : e.speaker === 'director' ? t("导演") : e.speaker) : e.kind === 'story' ? t("正式剧情") : t("AI 回复")
</script>
<template>
  <section class="rp-stage" :aria-label="t('剧情与交互')">
    <header class="stream-toolbar"><div class="story-mode"><span>{{ s.state?.settings.narrative.perspective === 'first' ? t("第一人称") : t("第三人称") }}</span><span data-testid="polishing-level">{{ s.state ? t(polishingLabels[s.state.settings.polishing.level]) : '' }}</span></div><span>{{ t("角色保护：") }}<strong data-testid="protected-character">{{ s.characters.find(c => c.metadata.id === s.state?.protected_character)?.metadata.name || s.state?.protected_character || t("无") }}</strong></span></header>
    <div ref="scroller" class="history-scroll" tabindex="0" :aria-label="t('剧情历史滚动区')" @scroll="onScroll" @load.capture="mediaLoaded">
      <div v-if="s.hasMore || s.loadingHistory" class="history-load"><button :disabled="s.loadingHistory" @click="s.loadMore()">{{ s.loadingHistory ? t("加载中……") : t("加载更早消息") }}</button></div>
      <div class="history" data-testid="history" :aria-busy="s.loadingHistory">
        <div v-if="!visible.length" class="empty-story"><span aria-hidden="true" class="story-emblem">✦</span><h2>{{ t("故事，从这一刻开始") }}</h2><p>{{ t("选择身份，写下第一段行动或想法。") }}</p></div>
        <article v-for="event in visible" :id="`event-${event.id}`" :key="event.id" :data-event-id="event.id" :data-kind="event.type === 'output' ? event.kind : event.type" :class="['event', event.type, event.kind]" tabindex="-1">
          <header class="event-meta"><CharacterPortrait v-if="event.type === 'input'" class="message-portrait" :identity="event.speaker || 'director'" clickable /><span v-else class="event-avatar" aria-hidden="true">{{ event.kind === 'story' ? '✦' : '○' }}</span><strong>{{ label(event) }}</strong><time>{{ formatDate(event.time) }}</time></header>
          <CharacterStrip v-if="event.type === 'output' && event.characters?.length" :identities="event.characters" />
          <p v-if="event.type === 'input'" class="input-text">{{ event.text }}</p><MarkdownContent v-else :text="event.text || ''" />
          <div v-if="event.media?.length" class="inline-media"><MediaView v-for="media in event.media" :key="media.id" :media="media" /></div>
          <form v-if="edit?.id === event.id" class="output-editor form-grid" @submit.prevent="saveEdit">
            <label>{{ t("编辑消息") }}<textarea v-model="edit.text" :aria-label="t('编辑正文 {0}', [event.id])" :disabled="!!s.pending" rows="10" /></label>
            <p class="muted">{{ t("保存后替换原文，不自动重新生成回复，也不改变角色状态、附件和后续剧情。") }}</p>
            <div class="actions"><button :disabled="historyLocked || (!edit.text.trim() && !event.media?.length)" :aria-label="t('保存正文 {0}', [event.id])">{{ t("保存修改") }}</button><button type="button" :disabled="!!s.pending" @click="cancelEdit">{{ t("取消编辑") }}</button></div>
          </form>
          <form v-if="branch?.id === event.id" class="form-grid" :aria-label="t('创建故事分支')" @submit.prevent="saveBranch">
            <label>{{ t("新故事名称") }}<input v-model="branch.name" :aria-label="t('分支故事名称')" :disabled="!!s.pending" required /></label>
            <p class="muted">{{ t("复制截至本条消息的历史和附件，原故事不变。") }}</p>
            <div class="actions"><button :disabled="!!s.pending || !branch.name.trim()">{{ t("创建分支") }}</button><button type="button" :disabled="!!s.pending" @click="branches.delete(s.selectedTimeline)">{{ t("取消分支") }}</button></div>
          </form>
          <div class="actions message-actions" role="group" :aria-label="t('消息操作')"><button :disabled="historyLocked" :aria-label="t('编辑{0} {1}', [event.type === 'input' ? t('输入') : t('输出'), event.id])" @click="beginEdit(event)">{{ t("编辑") }}</button><button class="danger" :disabled="historyLocked" :aria-label="t('删除至{0} {1}', [event.type === 'input' ? t('输入') : t('输出'), event.id])" @click="remove(event)">{{ t("删除至") }}</button><button :disabled="!!s.pending" :aria-label="t('从此处分支 {0}', [event.id])" @click="beginBranch(event)">{{ t("分支") }}</button></div>
        </article>
      </div>

    </div>
    <footer v-if="!atLatest && visible.length" class="history-navigation"><button :disabled="s.syncing" @click="s.latest()">{{ t("跳到最新") }}</button></footer>
  </section>
</template>

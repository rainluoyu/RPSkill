<script setup lang="ts">
import { t } from "../lib/i18n"
import CharacterAvatar from './CharacterAvatar.vue'
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { useWorkbenchStore } from '../stores/workbench'
import { getOwn, segment } from '../lib/types'
import type { Character } from '../lib/types'

const emit = defineEmits<{ 'dirty-change': [dirty: boolean] }>()
const s = useWorkbenchStore()
const mode = ref<'list' | 'editor'>('list'), tab = ref<'card' | 'state' | 'media'>('card')
const search = ref(''), editing = ref(''), baseHash = ref(''), baseline = ref('')
const editor = ref<HTMLElement>()
const form = reactive({ name: '', enabled: true, avatar: '', body: '' })
const stateDocument = ref(''), stateBaseline = ref('')
const mediaDirty = ref(false)
const selected = computed(() => s.characters.find(c => c.metadata.id === editing.value))
const characters = computed(() => s.characters.filter(c => c.metadata.name.toLocaleLowerCase().includes(search.value.trim().toLocaleLowerCase())))
const dynamic = computed(() => getOwn(s.state?.characters, editing.value) || '')
const cardDirty = computed(() => mode.value === 'editor' && JSON.stringify(form) !== baseline.value)
const stateDirty = computed(() => mode.value === 'editor' && stateDocument.value !== stateBaseline.value)
const dirty = computed(() => cardDirty.value || stateDirty.value || mediaDirty.value)
const enabled = (c: Character) => getOwn(s.state?.settings.characters, c.metadata.id) ?? c.metadata.enabled
watch(dirty, value => emit('dirty-change', value), { immediate: true })
watch(dynamic, value => { if (!stateDirty.value) { stateDocument.value = value; stateBaseline.value = value } })

async function open(c?: Character) {
  editing.value = c?.metadata.id || ''; baseHash.value = c?.sha256 || ''
  Object.assign(form, { name: c?.metadata.name || '', enabled: c?.metadata.enabled ?? true, avatar: c?.metadata.avatar || '', body: c?.metadata.body || '' })
  baseline.value = JSON.stringify(form)
  stateDocument.value = dynamic.value; stateBaseline.value = dynamic.value; mediaDirty.value = false
  tab.value = 'card'; mode.value = 'editor'
  await nextTick()
  editor.value?.scrollIntoView({ block: 'start' })
  editor.value?.querySelector<HTMLInputElement>('input[data-field="character-name"]')?.focus()
}
watch(() => s.characterTarget, id => {
  if (!id) return
  const card = s.characters.find(c => c.metadata.id === id)
  s.characterTarget = ''
  if (card) void open(card)
}, { immediate: true })
function back() {
  if (s.pending || (dirty.value && !confirm(t("放弃角色未保存的修改？")))) return
  mode.value = 'list'; stateDocument.value = dynamic.value; stateBaseline.value = dynamic.value; mediaDirty.value = false
}
function content(avatar = form.avatar) { return `---\nenabled: ${form.enabled}\n${avatar ? `avatar: ${JSON.stringify(avatar)}\n` : ''}---\n${form.body}` }
async function save() {
  form.name = form.name.trim()
  if (!form.name) { s.error = t("名称不能为空"); return }
  if (editing.value && !selected.value) { s.error = t("角色已被删除，请返回列表。"); return }
  if (editing.value && selected.value?.sha256 !== baseHash.value && !confirm(t("角色卡已被其他操作修改，确定覆盖？"))) return
  if (editing.value && editing.value !== form.name && mediaDirty.value) { s.error = t("请先上传或清除已选择的头像，再修改角色名称。"); return }
  const path = editing.value ? `/characters/${segment(editing.value)}` : '/characters'
  if (await s.mutate(path, editing.value ? 'PUT' : 'POST', editing.value ? { name: form.name, content: content() } : { id: form.name, content: content() })) {
    const saved = s.characters.find(c => c.metadata.id === form.name)
    if (saved) {
      editing.value = saved.metadata.id; baseHash.value = saved.sha256
      Object.assign(form, { name: saved.metadata.name, enabled: saved.metadata.enabled, avatar: saved.metadata.avatar || '', body: saved.metadata.body })
      baseline.value = JSON.stringify(form)
    }
  }
}
async function remove(c: Character) {
  if (!confirm(t("删除角色“{0}”？{1}历史引用会阻止删除。", [c.metadata.name, dirty.value ? t("未保存的修改将被丢弃。") : '']))) return
  if (await s.mutate(`/characters/${segment(c.metadata.id)}`, 'DELETE', {})) {
    mode.value = 'list'; stateDocument.value = dynamic.value; stateBaseline.value = dynamic.value; mediaDirty.value = false
  }
}
async function toggle(c: Character) { await s.mutate(`/chats/${segment(s.selectedTimeline)}/settings`, 'PATCH', { characters: { [c.metadata.id]: !enabled(c) } }) }
function resetState() {
  if (stateDirty.value && !confirm(t("放弃故事状态未保存的修改？"))) return
  stateDocument.value = dynamic.value; stateBaseline.value = dynamic.value
}
async function update() {
  const expected = dynamic.value
  if (expected !== stateBaseline.value && !confirm(t("故事状态已被其他操作修改，确定用当前文档覆盖？"))) return
  const document = stateDocument.value
  if (await s.mutate(`/chats/${segment(s.selectedTimeline)}/characters/${segment(editing.value)}/updates`, 'POST', { document, expected_document: expected })) {
    stateDocument.value = document; stateBaseline.value = document
  }
}
async function removeAvatar() {
  if (s.pending || cardDirty.value || !selected.value?.metadata.avatar) return
  if (selected.value.sha256 !== baseHash.value) { s.error = t("角色卡已更新，请返回列表后重新打开。"); return }
  if (!confirm(t("移除当前头像？图片文件会保留。"))) return
  if (await s.mutate(`/characters/${segment(editing.value)}`, 'PUT', { name: form.name, content: content('') })) uploaded()
}
function uploaded() {
  const c = selected.value
  // Only advance the card version when the upload did not race an external card edit.
  if (!c || cardDirty.value || c.metadata.name !== form.name || c.metadata.body !== form.body || c.metadata.enabled !== form.enabled) return
  form.avatar = c.metadata.avatar || ''; baseHash.value = c.sha256; baseline.value = JSON.stringify(form)
}
</script>

<template>
  <section class="character-manager">
    <template v-if="mode === 'list'">
      <div class="character-toolbar">
        <input v-model="search" type="search" :aria-label="t('搜索角色')" :placeholder="t('搜索角色')" />
        <button class="primary-button" :disabled="!!s.pending" @click="open()">{{ t("新建角色") }}</button>
      </div>
      <ul class="character-list">
        <li v-for="c in characters" :key="c.metadata.id" :class="{ 'character-disabled': !enabled(c) }">
          <button class="character-name" :aria-label="t('编辑角色 {0}', [c.metadata.name])" :disabled="!!s.pending" @click="open(c)">
            <span class="character-monogram" aria-hidden="true">{{ Array.from(c.metadata.name)[0] }}</span>
            <strong>{{ c.metadata.name }}</strong>
          </button>
          <div class="character-tools">
            <button role="switch" :aria-checked="enabled(c)" :aria-label="t('启用角色 {0}', [c.metadata.name])" :disabled="!!s.pending" @click="toggle(c)">{{ enabled(c) ? t("已启用") : t("已停用") }}</button>
            <button class="danger" :aria-label="t('删除角色 {0}', [c.metadata.name])" :disabled="!!s.pending" @click="remove(c)">{{ t("删除") }}</button>
          </div>
        </li>
      </ul>
      <p v-if="!characters.length" class="muted">{{ s.characters.length ? t("没有匹配的角色。") : t("暂无角色，点击“新建角色”添加。") }}</p>
    </template>
    <section v-else ref="editor" class="character-editor">
      <header class="character-toolbar">
        <button :disabled="!!s.pending" @click="back">{{ t("← 返回列表") }}</button>
        <strong class="character-title">{{ selected?.metadata.name || t("新建角色") }}</strong>
        <button v-if="selected" role="switch" :aria-checked="enabled(selected)" :aria-label="t('启用角色 {0}', [selected.metadata.name])" :disabled="!!s.pending" @click="toggle(selected)">{{ enabled(selected) ? t("已启用") : t("已停用") }}</button>
      </header>
      <nav v-if="selected" class="character-tabs" :aria-label="t('角色详情')">
        <button :aria-pressed="tab === 'card'" :class="{ active: tab === 'card' }" @click="tab = 'card'">{{ t("基础设定") }}{{ cardDirty ? ' ·' : '' }}</button>
        <button :aria-pressed="tab === 'state'" :class="{ active: tab === 'state' }" @click="tab = 'state'">{{ t("当前故事状态") }}{{ stateDirty ? ' ·' : '' }}</button>
        <button :aria-pressed="tab === 'media'" :class="{ active: tab === 'media' }" @click="tab = 'media'">{{ t("头像") }}{{ mediaDirty ? ' ·' : '' }}</button>
      </nav>
      <form v-show="tab === 'card'" @submit.prevent="save">
        <fieldset class="form-grid character-fields" :disabled="!!s.pending">
          <label>{{ t("角色名称") }}<input v-model="form.name" :aria-label="t('角色名称')" data-field="character-name" required /></label>
          <label>{{ t("角色设定") }}<textarea v-model="form.body" :aria-label="t('角色 Markdown')" rows="16" /></label>
          <div class="actions"><button class="primary-button" :disabled="!cardDirty">{{ t("保存角色卡") }}</button><button type="button" @click="back">{{ t("返回") }}</button></div>
        </fieldset>
      </form>
      <section v-if="selected" v-show="tab === 'state'" :aria-label="t('角色故事状态')">
        <form @submit.prevent="update">
          <fieldset class="form-grid character-fields" :disabled="!!s.pending">
            <label>{{ t("故事状态 · {0}", [s.timeline?.name || '']) }}<textarea v-model="stateDocument" :aria-label="t('故事状态文档')" rows="16" :placeholder="t('记录当前经历、关系、伤势、物品与目标……')" /></label>
            <div class="actions"><button class="primary-button" :disabled="!stateDirty">{{ t("保存状态") }}</button><button type="button" :disabled="!stateDirty && dynamic === stateBaseline" @click="resetState">{{ t("还原") }}</button></div>
          </fieldset>
        </form>
      </section>
      <CharacterAvatar v-if="selected" v-show="tab === 'media'" :key="editing" :character="editing" :avatar="selected.metadata.avatar" :card-dirty="cardDirty" @uploaded="uploaded" @remove="removeAvatar" @dirty-change="mediaDirty = $event" />
    </section>
  </section>
</template>

<style scoped>
.character-toolbar { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; margin-bottom: 16px; }
.character-toolbar > input { flex: 1 1 180px; }
.character-title { flex: 1; min-width: 0; overflow-wrap: anywhere; }
.character-list { list-style: none; padding: 0; margin: 0; }
.character-list > li { display: flex; align-items: center; gap: 12px; padding: 12px; border: 1px solid var(--line); border-radius: 8px; background: var(--surface); margin-bottom: 10px; }
.character-name { display: flex; align-items: center; gap: 12px; min-width: 0; flex: 1; text-align: left; background: transparent; border-color: transparent; overflow-wrap: anywhere; }
.character-monogram { display: grid; place-items: center; flex: 0 0 40px; height: 40px; background: #3b3530; border-radius: 10px; color: var(--accent); font-size: 20px; }
.character-disabled .character-name { opacity: .55; }
.character-tools { display: flex; flex-wrap: wrap; gap: 8px; flex-shrink: 0; }
.character-tabs { display: flex; flex-wrap: wrap; gap: 8px; border-bottom: 1px solid var(--line); padding-bottom: 12px; margin-bottom: 20px; }
.character-tabs button { background: transparent; border-color: transparent; }
.character-tabs button.active { color: var(--accent); background: var(--surface); border-color: var(--line); }
.character-fields { border: 0; padding: 0; margin: 0; min-width: 0; }
@media (max-width: 600px) {
  .character-list > li { flex-wrap: wrap; gap: 8px; }
  .character-name { flex-basis: 100%; padding: 4px 0; }
  .character-tools { margin-left: auto; }
  .character-tabs { gap: 2px; }
  .character-tabs button { padding-inline: 8px; }
}
</style>

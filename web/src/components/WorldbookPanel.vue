<script setup lang="ts">
import { t } from "../lib/i18n"
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { useWorkbenchStore } from '../stores/workbench'
import { getOwn, segment } from '../lib/types'
import type { WorldEntry } from '../lib/types'

const emit = defineEmits<{ 'dirty-change': [dirty: boolean] }>()
const s = useWorkbenchStore()
const mode = ref<'list' | 'editor'>('list')
const expanded = reactive(new Set<string>())
const search = ref(''), newCategory = ref(''), creatingCategory = ref(false)
const recent = ref(0), recentDirty = ref(false)
const editing = ref(''), baseHash = ref(''), baseline = ref('')
const editor = ref<HTMLElement>()
const form = reactive({ category: '', slug: '', name: '', enabled: true, activation: 'keyword' as WorldEntry['metadata']['activation'], keys: '', priority: 50, body: '' })
const dirty = computed(() => mode.value === 'editor' && JSON.stringify(form) !== baseline.value)
watch(() => dirty.value || recentDirty.value || !!newCategory.value.trim(), value => emit('dirty-change', value), { immediate: true })
watch(() => s.state?.settings.worldbook.scan_recent, value => {
  if (!recentDirty.value) recent.value = value ?? 0
}, { immediate: true })
const categories = computed(() => [...new Set([...s.worldbookCategories, ...s.worldbook.map(d => d.category)])].sort())
const groups = computed(() => categories.value.map(id => ({
  id,
  entries: s.worldbook.filter(d => d.category === id && (!search.value.trim() || `${id} ${d.metadata.name} ${(d.metadata.keys || []).join(' ')}`.toLocaleLowerCase().includes(search.value.trim().toLocaleLowerCase()))),
})).filter(group => !search.value.trim() || group.entries.length))
const selected = computed(() => s.worldbook.find(d => d.path === editing.value))
const enabled = (d: WorldEntry) => getOwn(s.state?.settings.worldbook.entries, `${d.category}/${d.slug}`) ?? d.metadata.enabled
const categoryEnabled = (id: string) => getOwn(s.state?.settings.worldbook.categories, id) !== false
const activationNames = { always: "始终激活", keyword: "关键词触发" }

async function open(category: string, d?: WorldEntry) {
  editing.value = d?.path || ''
  baseHash.value = d?.sha256 || ''
  Object.assign(form, d ? { ...d.metadata, category: d.category, slug: d.slug, keys: (d.metadata.keys || []).join('\n'), activation: d.metadata.activation } : { category, slug: '', name: '', enabled: true, activation: 'keyword', keys: '', priority: 50, body: '' })
  baseline.value = JSON.stringify(form)
  mode.value = 'editor'
  await nextTick()
  editor.value?.scrollIntoView({ block: 'start' })
  editor.value?.querySelector<HTMLInputElement>('input[data-field="entry-name"]')?.focus()
}
function back() {
  if (s.pending || (dirty.value && !confirm(t("放弃此条目未保存的修改？")))) return
  mode.value = 'list'
}
function content() {
  return `---\nenabled: ${form.enabled}\nactivation: ${JSON.stringify(form.activation)}\nkeys: ${JSON.stringify(form.keys.split('\n').map(k => k.trim()).filter(Boolean))}\npriority: ${form.priority}\n---\n${form.body}`
}
async function save() {
  form.name = form.name.trim()
  if (!form.name) { s.error = t("名称不能为空"); return }
  if (editing.value && !selected.value) { s.error = t("条目已被删除，请返回列表。"); return }
  if (editing.value && selected.value?.sha256 !== baseHash.value && !confirm(t("条目已被其他操作修改，确定覆盖？"))) return
  const path = `/worldbook/${segment(form.category)}/${segment(form.slug)}`
  const ok = await s.mutate(editing.value ? path : '/worldbook', editing.value ? 'PUT' : 'POST', editing.value ? { name: form.name, content: content() } : { category: form.category, slug: form.name, content: content() })
  if (ok) mode.value = 'list'
}
async function remove(d: WorldEntry) {
  if (!confirm(t("删除世界书条目“{0}”？", [d.metadata.name]))) return
  if (await s.mutate(`/worldbook/${segment(d.category)}/${segment(d.slug)}`, 'DELETE', {})) mode.value = 'list'
}
async function toggleCategory(id: string) {
  await s.mutate(`/chats/${segment(s.selectedTimeline)}/settings`, 'PATCH', { worldbook: { categories: { [id]: !categoryEnabled(id) } } })
}
async function toggleEntry(d: WorldEntry) {
  await s.mutate(`/chats/${segment(s.selectedTimeline)}/settings`, 'PATCH', { worldbook: { entries: { [`${d.category}/${d.slug}`]: !enabled(d) } } })
}
async function addCategory() {
  const id = newCategory.value.trim()
  if (categories.value.includes(id)) { s.error = t("分类已存在"); return }
  if (await s.mutate('/worldbook/categories', 'POST', { name: id })) {
    newCategory.value = ''; creatingCategory.value = false; search.value = ''
  }
}
async function saveSettings() {
  if (await s.mutate(`/chats/${segment(s.selectedTimeline)}/settings`, 'PATCH', { worldbook: { scan_recent: recent.value } })) {
    recentDirty.value = false
    recent.value = s.state?.settings.worldbook.scan_recent ?? 0
  }
}
</script>

<template>
  <section class="worldbook-manager">
    <template v-if="mode === 'list'">
      <div class="wb-toolbar">
        <input v-model="search" type="search" :aria-label="t('搜索世界书')" :placeholder="t('搜索条目、分类或关键词')" />
        <button :disabled="!!s.pending" :aria-expanded="creatingCategory" @click="creatingCategory = !creatingCategory">{{ t("新建分类") }}</button>
        <button :disabled="!!s.pending" @click="s.mutate('/worldbook/refresh', 'POST', {})">{{ t("刷新") }}</button>
      </div>
      <form v-show="creatingCategory" class="panel toolbar wrap" @submit.prevent="addCategory">
        <label>{{ t("分类名称") }}<input v-model="newCategory" :aria-label="t('新分类 ID')" :placeholder="t('如 魔法、地点')" maxlength="60" required /></label>
        <button :disabled="!!s.pending">{{ t("创建分类") }}</button>
      </form>
      <form class="wb-settings" @submit.prevent="saveSettings">
        <label>{{ t("关键词回看条数") }}<input v-model.number="recent" :aria-label="t('世界书关键词回看条数')" type="number" min="0" max="1000" step="1" required @input="recentDirty = true" /></label>
        <button :disabled="!!s.pending || !recentDirty">{{ t("保存") }}</button>
      </form>

      <p v-if="!categories.length" class="muted">{{ t("还没有世界书分类。新建分类后添加条目。") }}</p>
      <p v-else-if="!groups.length" class="muted">{{ t("没有匹配的条目。") }}</p>
      <section v-for="group in groups" :key="group.id" class="wb-category" :aria-label="t('分类 {0}', [group.id])">
        <header class="wb-category-header">
          <button class="wb-category-heading" :aria-expanded="expanded.has(group.id)" :aria-label="t('展开分类 {0}', [group.id])" @click="expanded.has(group.id) ? expanded.delete(group.id) : expanded.add(group.id)"><span aria-hidden="true">{{ expanded.has(group.id) ? '▾' : '▸' }}</span><strong>{{ group.id }}</strong><span class="muted">{{ t("{0} 条", [group.entries.length]) }}</span></button>
          <div class="wb-category-tools">
            <button role="switch" :aria-checked="categoryEnabled(group.id)" :aria-label="t('启用分类 {0}', [group.id])" :disabled="!!s.pending" @click="toggleCategory(group.id)">{{ categoryEnabled(group.id) ? t("分类已启用") : t("分类已停用") }}</button>
            <button :disabled="!!s.pending" @click="open(group.id)">{{ t("添加条目") }}</button>
          </div>
        </header>
        <div v-show="expanded.has(group.id)">
          <ul class="wb-entries">
            <li v-for="entry in group.entries" :key="entry.path" class="wb-entry" :class="{ 'wb-disabled': !enabled(entry) || !categoryEnabled(group.id) }">
              <button class="wb-entry-content" :aria-label="t('编辑条目 {0}', [entry.metadata.id])" :disabled="!!s.pending" @click="open(group.id, entry)">
                <strong>{{ entry.metadata.name }}</strong>
                <span class="wb-entry-info"><span>{{ t(activationNames[entry.metadata.activation]) }}</span><span v-if="entry.metadata.activation === 'keyword'">{{ (entry.metadata.keys || []).join(' · ') }}</span><span>{{ t("优先级 {0}", [entry.metadata.priority]) }}</span></span>
              </button>
              <div class="wb-entry-actions">
                <button role="switch" :aria-checked="enabled(entry)" :aria-label="t('启用条目 {0}', [entry.metadata.id])" :disabled="!!s.pending" @click="toggleEntry(entry)">{{ enabled(entry) ? t("已启用") : t("已停用") }}</button>
                <button class="danger" :aria-label="t('删除条目 {0}', [entry.metadata.id])" :disabled="!!s.pending" @click="remove(entry)">{{ t("删除") }}</button>
              </div>
            </li>
          </ul>
          <p v-if="!group.entries.length" class="wb-empty muted">{{ t("暂无条目") }}</p>
        </div>
      </section>
    </template>
    <section v-else ref="editor" class="panel wb-editor">
      <header class="wb-toolbar"><button :disabled="!!s.pending" @click="back">{{ t("← 返回列表") }}</button><span class="muted">{{ form.category }} / {{ editing ? form.name : t("新条目") }}</span></header>
      <form class="form-grid" @submit.prevent="save">
        <label>{{ t("名称") }}<input v-model="form.name" :aria-label="t('条目名称')" data-field="entry-name" required /></label>
        <div class="two-columns">
          <label>{{ t("激活方式") }}<select v-model="form.activation" :aria-label="t('激活方式')"><option value="always">{{ t("始终激活") }}</option><option value="keyword">{{ t("关键词触发") }}</option></select></label>
          <label>{{ t("优先级") }}<input v-model.number="form.priority" :aria-label="t('优先级')" type="number" step="1" required /></label>
        </div>
        <label v-if="form.activation === 'keyword'">{{ t("关键词（每行一个）") }}<textarea v-model="form.keys" :aria-label="t('关键词')" rows="2" required /></label>
        <label>{{ t("正文") }}<textarea v-model="form.body" :aria-label="t('世界书 Markdown')" rows="14" /></label>

        <div class="actions"><button class="primary-button" :disabled="!!s.pending">{{ t("保存条目") }}</button><button type="button" :disabled="!!s.pending" @click="back">{{ t("取消") }}</button><button v-if="selected" type="button" class="danger" :disabled="!!s.pending" @click="remove(selected)">{{ t("删除条目") }}</button></div>
      </form>
    </section>
  </section>
</template>

<style scoped>
.wb-toolbar { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-bottom: 16px; }
.wb-toolbar > input { flex: 1 1 220px; }
.wb-category { border: 1px solid var(--line); border-radius: 8px; background: var(--surface); margin-bottom: 12px; overflow: hidden; }
.wb-category-header { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; padding: 10px 14px; }
.wb-category-heading { display: flex; align-items: center; flex: 1 1 140px; gap: 8px; min-width: 0; text-align: left; padding: 6px 0; background: transparent; border: 0; overflow-wrap: anywhere; }
.wb-category-heading > span { font-size: 12px; }
.wb-category-tools { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 8px; margin-left: auto; }
.wb-settings { display: flex; align-items: center; gap: 10px; margin: 0 0 16px; flex-wrap: wrap; }
.wb-settings label { flex-direction: row; align-items: center; }
.wb-settings input { width: 85px; }
.wb-entries { list-style: none; padding: 0; margin: 0; }
.wb-entry { display: flex; align-items: center; gap: 12px; padding: 12px 14px; border-top: 1px solid var(--line); }
.wb-entry-content { display: flex; flex-direction: column; align-items: flex-start; gap: 8px; flex: 1; min-width: 0; text-align: left; background: transparent; border-color: transparent; overflow-wrap: anywhere; }
.wb-entry-content strong { font-size: 14px; }
.wb-entry-info { display: flex; flex-wrap: wrap; gap: 6px 14px; color: var(--muted); font-size: 12px; }
.wb-entry-actions { display: flex; flex-wrap: wrap; gap: 6px; }
.wb-disabled .wb-entry-content { opacity: .55; }
button[role='switch'][aria-checked='true'] { border-color: #887455; color: var(--accent); }
.wb-empty { padding: 0 16px 14px; font-size: 13px; }
.wb-editor textarea[aria-label='关键词'] { min-height: 65px; }
@media (max-width: 600px) {
  .wb-entry { flex-direction: column; align-items: stretch; gap: 8px; }
  .wb-entry-actions { justify-content: flex-end; }
  .wb-toolbar > input { flex-basis: 100%; }
}
</style>

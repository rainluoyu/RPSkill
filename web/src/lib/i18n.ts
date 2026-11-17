import { readonly, ref } from 'vue'
import english from '../locales/en'

export type Locale = 'zh-CN' | 'en'
const storageKey = 'rps.ui-language'
function initialLocale(): Locale {
  try {
    const saved = localStorage.getItem(storageKey)
    if (saved === 'zh-CN' || saved === 'en') return saved
  } catch { /* Language selection still works when storage is unavailable. */ }
  return typeof navigator !== 'undefined' && navigator.language.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en'
}
const current = ref<Locale>(initialLocale())
export const locale = readonly(current)
export function setLocale(value: string) {
  if (value !== 'zh-CN' && value !== 'en') return
  current.value = value
  updateDocument()
  try { localStorage.setItem(storageKey, value) } catch { /* In-memory preference is enough. */ }
}
export function t(key: string, values: readonly unknown[] = []): string {
  const template = current.value === 'en' && Object.hasOwn(english, key) ? english[key as keyof typeof english] : key
  // One pass: braces and HTML inside interpolated names remain plain data.
  return template.replace(/\{(\d+)\}/g, (placeholder, index) => index < values.length ? String(values[index]) : placeholder)
}
export function formatDate(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(current.value, { dateStyle: 'short', timeStyle: 'medium' }).format(date)
}
function updateDocument() {
  if (typeof document === 'undefined') return
  document.documentElement.lang = current.value
  document.title = t('RPSkill · 小说工作台')
  document.querySelector('meta[name="description"]')?.setAttribute('content', t('RPSkill 本地小说式角色扮演工作台'))
}
updateDocument()

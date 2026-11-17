import { createApp, nextTick } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import App from '../src/App.vue'
import { useWorkbenchStore } from '../src/stores/workbench'
import { formatDate, locale, setLocale, t } from '../src/lib/i18n'
import english from '../src/locales/en'
import { fakeAPI, input, project, state, status, timelines } from './fixtures'

let dispose: (() => void) | undefined
beforeEach(() => {
 setLocale('zh-CN')
 vi.stubGlobal('fetch', vi.fn(fakeAPI))
 Element.prototype.scrollIntoView = vi.fn()
})
afterEach(() => { dispose?.(); dispose = undefined; vi.unstubAllGlobals(); setLocale('zh-CN') })
function mount(authenticated = true) {
 const pinia = createPinia(); setActivePinia(pinia)
 const s = useWorkbenchStore()
 Object.assign(s, { booting: false, authenticated, project, timelines, status, selectedTimeline: 'main', state })
 const root = document.createElement('div'); document.body.appendChild(root)
 const app = createApp(App).use(pinia); app.mount(root)
 dispose = () => { app.unmount(); root.remove() }
 return { s, root }
}
it('switches the workbench live without translating story content, names or drafts', async () => {
 const { s, root } = mount()
 s.draft = '导演 {0} <b>draft</b>'; s.inputIdentity = 'director'
 s.characters = [{metadata:{id:'交流角色',name:'交流角色',enabled:true,body:''},content:'',path:'',sha256:'x',assets:[]}]
 s.events = [input]
 await nextTick()
 const textarea = root.querySelector('textarea')!
 expect(root.querySelector('.topbar select')).toBeNull()
 root.querySelector<HTMLButtonElement>('.topbar nav button:last-child')!.click(); await nextTick()
 expect(s.activePanel).toBe('theme')
 expect(root.querySelector<HTMLSelectElement>('[aria-label="界面主题"]')?.disabled).toBe(true)
 expect([...root.querySelectorAll('.appearance-options select')].map(el => el.getAttribute('aria-label'))).toEqual(['界面主题', '语言'])
 const picker = root.querySelector<HTMLSelectElement>('.appearance-options select[aria-label="语言"]')!
 picker.value = 'en'; picker.dispatchEvent(new Event('change', {bubbles:true})); await nextTick()
 expect([...root.querySelectorAll('.topbar nav button')].map(x=>x.textContent)).toEqual(['✦Story','▤Worldbook','♙Characters','⑂Stories','◐Theme'])
 expect(root.querySelector('#management-title')?.textContent).toBe('Theme')
 expect(root.querySelector('.appearance-options select[aria-label="Language"]')).toBe(picker)
 root.querySelector<HTMLButtonElement>('[aria-label="Close panel"]')!.click(); await nextTick()
 expect(root.textContent).not.toContain('Running')
 expect(root.textContent).toContain('Basic polishing')
 expect(root.querySelector('.composer-identity svg')?.getAttribute('aria-label')).toBe('Portrait of Director')
 expect([...root.querySelectorAll('.identity-picker option')].map(x=>x.textContent)).toEqual(['Director','Chat','交流角色'])
 expect(root.textContent).toContain('下雨了')
 expect(root.querySelector('textarea')).toBe(textarea)
 expect(textarea.value).toBe('导演 {0} <b>draft</b>')
 expect(s.inputIdentity).toBe('director')
 s.activePanel = 'chats'; await nextTick()
 expect(root.querySelector('[aria-label="Search stories"]')).not.toBeNull()
 root.querySelector<HTMLButtonElement>('[aria-label="Story settings: 主故事"]')!.click(); await nextTick()
 expect(root.querySelector('[aria-label="Story details"]')?.textContent).toContain('Media')
 setLocale('zh-CN'); await nextTick()
 expect(root.querySelector('[aria-label="故事详情"]')?.textContent).toContain('媒体')
 expect(s.draft).toBe('导演 {0} <b>draft</b>')
 expect(vi.mocked(fetch).mock.calls.every(([,init])=>!init?.method || init.method==='GET')).toBe(true)
})
it('localizes login and both inline SVG identities without external image loads', async () => {
 const { s, root } = mount(false)
 setLocale('en'); await nextTick()
 expect(root.querySelector('[aria-label="Access token"]')).not.toBeNull()
 s.authenticated = true; s.inputIdentity = 'chat'; await nextTick()
 expect(root.querySelector('.composer-identity svg')?.getAttribute('aria-label')).toBe('Portrait of Chat')
 expect(root.querySelector('.composer-identity svg image, .composer-identity svg use')).toBeNull()
 expect(root.querySelector('.identity-placeholder')).toBeNull()
 setLocale('zh-CN'); await nextTick()
 expect(root.querySelector('.composer-identity svg')?.getAttribute('aria-label')).toBe('交流的头像')
})
it('persists language only in browser preferences and formats dates in that locale', () => {
 setLocale('en')
 expect(localStorage.getItem('rps.ui-language')).toBe('en')
 expect(document.documentElement.lang).toBe('en')
 const date = '2026-11-17T01:02:03Z'
 expect(formatDate(date)).toBe(new Intl.DateTimeFormat('en',{dateStyle:'short',timeStyle:'medium'}).format(new Date(date)))
 setLocale('zh-CN')
 expect(formatDate(date)).toBe(new Intl.DateTimeFormat('zh-CN',{dateStyle:'short',timeStyle:'medium'}).format(new Date(date)))
 setLocale('unknown'); expect(locale.value).toBe('zh-CN')
 expect(formatDate('invalid')).toBe('invalid')
})
it('keeps dictionary placeholders consistent and never reinterprets user interpolation', () => {
 const slots = (s: string) => [...s.matchAll(/\{\d+\}/g)].map(x=>x[0]).sort()
 for (const [key, value] of Object.entries(english)) expect(slots(value), key).toEqual(slots(key))
 setLocale('en')
 expect(t('故事状态 · {0}', ['{1}<script>'])).toBe('Story state · {1}<script>')
 expect(t('unknown error')).toBe('unknown error')
})

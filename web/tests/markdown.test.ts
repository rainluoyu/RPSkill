import { setLocale } from '../src/lib/i18n'
import { describe, expect, it } from 'vitest'
import { renderMarkdown } from '../src/lib/markdown'

function render(source: string): HTMLDivElement {
  const container = document.createElement('div')
  container.innerHTML = renderMarkdown(source)
  return container
}

describe('safe Markdown boundary', () => {
  it('renders narrative headings, emphasis, paragraphs, quotes and lists', () => {
    const result = render('# 第一章\n\n**雨夜**，*书房*。\n\n> 留下一盏灯\n\n- Alice\n- Bob')
    expect(result.querySelector('h1')?.textContent).toBe('第一章')
    expect(result.querySelector('strong')?.textContent).toBe('雨夜')
    expect(result.querySelector('em')?.textContent).toBe('书房')
    expect(result.querySelector('blockquote')?.textContent).toContain('留下一盏灯')
    expect(result.querySelectorAll('li')).toHaveLength(2)
  })

  it('supports code and tables without enabling raw HTML', () => {
    const result = render('```html\n<script>alert(1)</script>\n```\n\n| 名称 | 状态 |\n| --- | --- |\n| Alice | 清醒 |')
    expect(result.querySelector('pre code')?.textContent).toContain('<script>')
    expect(result.querySelector('script')).toBeNull()
    expect(result.querySelectorAll('td')).toHaveLength(2)
  })

  it.each([
    '<script>alert(1)</script>',
    '<img src=x onerror=alert(1)>',
    '<svg onload=alert(1)><a href="javascript:alert(1)">x</a></svg>',
    '<iframe src="https://example.com"></iframe>',
    '<form><input name="location"></form>',
    '<style>body{display:none}</style>',
    '<math><mtext><img src=x onerror=alert(1)></mtext></math>',
  ])('keeps raw HTML inert: %s', (payload) => {
    const result = render(payload)
    expect(result.querySelector('script,img,svg,iframe,form,input,style,math')).toBeNull()
    expect(result.textContent).toContain(payload)
  })

  it.each([
    '[link](javascript:alert%281%29)',
    '[link](JaVaScRiPt:alert%281%29)',
    '[link](jav&#x61;script:alert%281%29)',
    '[link](data:text/html;base64,PHNjcmlwdD4=)',
    '[link](vbscript:msgbox%281%29)',
    '[link](//example.com/tracker)',
    '[link](/unconfirmed-server-route)',
  ])('does not create an active unsafe or unconfirmed URL: %s', (payload) => {
    expect(render(payload).querySelector('a[href]')).toBeNull()
  })

  it('preserves explicit HTTPS links with isolated browsing context', () => {
    const link = render('[官方资料](https://example.com/docs "资料")').querySelector('a')
    expect(link?.getAttribute('href')).toBe('https://example.com/docs')
    expect(link?.getAttribute('target')).toBe('_blank')
    expect(link?.getAttribute('rel')).toBe('noopener noreferrer')
    expect(link?.getAttribute('title')).toBe('资料')
  })

  it('allows local anchors and email links', () => {
    const result = render('[章节](#chapter) [联系](mailto:writer@example.com)')
    expect(Array.from(result.querySelectorAll('a'), (link) => link.getAttribute('href')))
      .toEqual(['#chapter', 'mailto:writer@example.com'])
  })

  it('never loads arbitrary Markdown images even when attachments are supported', () => {
    setLocale('zh-CN')
    const result = render('![场景](https://example.com/tracker.png) ![头像](/media/avatar.png)')
    expect(result.querySelector('img')).toBeNull()
    expect(result.textContent).toContain('[图片：场景，请查看消息附件]')
    expect(result.textContent).not.toContain('https://example.com')
  })

  it('escapes markup inside image alt text', () => {
    const result = render('![<img src=x onerror=alert(1)>](https://example.com/x.png)')
    expect(result.querySelector('img')).toBeNull()
    expect(result.textContent).toContain('<img src=x onerror=alert(1)>')
  })

  it('renders an empty source safely', () => {
    expect(renderMarkdown('')).toBe('')
  })
})

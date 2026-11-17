import { t } from "./i18n"
import DOMPurify from 'dompurify'
import MarkdownIt from 'markdown-it'

const markdown = new MarkdownIt({
  html: false,
  linkify: false,
  breaks: true,
})

markdown.renderer.rules.link_open = (tokens, index, options, _env, renderer) => {
  const token = tokens[index]!
  token.attrSet('target', '_blank')
  token.attrSet('rel', 'noopener noreferrer')
  return renderer.renderToken(tokens, index, options)
}

// Only explicit event media references are previewed by MediaView. Markdown
// images never load arbitrary URLs (including remote tracking pixels).
markdown.renderer.rules.image = (tokens, index) => {
  const alt = tokens[index]!.content || t("图片")
  return markdown.utils.escapeHtml(t("[图片：{0}，请查看消息附件]", [alt]))
}

/** The only Markdown-to-HTML boundary; never render unsanitized story content. */
export function renderMarkdown(source: string): string {
  return DOMPurify.sanitize(markdown.render(source), {
    ALLOWED_TAGS: [
      'p', 'br', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'strong', 'em', 's',
      'blockquote', 'pre', 'code', 'ul', 'ol', 'li', 'hr',
      'table', 'thead', 'tbody', 'tr', 'th', 'td', 'a',
    ],
    ALLOWED_ATTR: ['href', 'title', 'target', 'rel', 'start'],
    // These are renderer-generated, non-URL attributes.
    ADD_URI_SAFE_ATTR: ['target', 'rel', 'start'],
    ALLOW_DATA_ATTR: false,
    ALLOW_ARIA_ATTR: false,
    ALLOWED_URI_REGEXP: /^(?:https?:|mailto:|#)/i,
  })
}

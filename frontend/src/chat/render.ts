// The fixed message rendering pipeline (design D9):
//   markdown-it (raw HTML off, images become links, only http/https/mailto links)
//   → token transforms for @mentions, ~channels and :emoji: (text tokens, never HTML strings)
//   → render → DOMPurify with an explicit allowlist → the caller binds that string with v-html, nothing added after.
// User data other than the message text (display names, channel names) always goes through text bindings.
import markdownit, { type MarkdownIt, type StateCore, type Token } from 'markdown-it'
import DOMPurify from 'dompurify'
import { nameToEmoji } from 'gemoji'

const SAFE_URL = /^(?:https?:|mailto:)/i

const ALLOWED_TAGS = [
  'p',
  'br',
  'strong',
  'em',
  'del',
  's',
  'code',
  'pre',
  'blockquote',
  'ul',
  'ol',
  'li',
  'a',
  'span',
  'h1',
  'h2',
  'h3',
  'h4',
  'h5',
  'h6',
  'table',
  'thead',
  'tbody',
  'tr',
  'th',
  'td',
  'hr',
]
const SPAN_CLASSES = new Set(['mention', 'mention-me', 'channel-link', 'emoji'])

export interface RenderContext {
  /** the viewer's username: their mentions are highlighted */
  me?: string
}

function createMarkdown(): MarkdownIt {
  const md = markdownit({ html: false, linkify: true, breaks: true, typographer: false })
  // link destinations arrive here already entity-decoded ("&#106;avascript:" is "javascript:")
  md.validateLink = (url: string) => SAFE_URL.test(url.trim())

  // images never load: they render as a link to the image
  md.renderer.rules.image = (tokens, idx) => {
    const tok = tokens[idx]!
    const src = String(tok.attrGet('src') ?? '')
    const alt = tok.children?.map((c) => c.content).join('') || src
    const text = md.utils.escapeHtml(alt)
    if (!SAFE_URL.test(src)) return text
    return `<a href="${md.utils.escapeHtml(src)}" target="_blank" rel="noopener noreferrer">${text}</a>`
  }
  // every link opens in a new tab without referrer or opener
  md.renderer.rules.link_open = (tokens, idx, options, _env, self) => {
    tokens[idx]!.attrSet('target', '_blank')
    tokens[idx]!.attrSet('rel', 'noopener noreferrer')
    return self.renderToken(tokens, idx, options)
  }
  md.core.ruler.push('meethub_tokens', transformText)
  return md
}

// @username (letters, digits, . _ -; trailing . _ - are punctuation), ~channel, :emoji:
const TOKEN_RE = /(^|[^\p{L}\p{N}@._-])@([\p{L}\p{N}._-]+)|(^|[^\p{L}\p{N}~_-])~([a-z0-9_-]+)|:([a-z0-9_+-]+):/giu
const CHANNEL_WIDE = new Set(['channel', 'all', 'here'])

function transformText(state: StateCore): void {
  const me = (state.env as RenderContext).me?.toLowerCase()
  for (const block of state.tokens) {
    if (block.type !== 'inline' || !block.children) continue
    const out: Token[] = []
    let insideLink = 0
    for (const tok of block.children) {
      if (tok.type === 'link_open') insideLink++
      if (tok.type === 'link_close') insideLink--
      if (tok.type !== 'text' || insideLink > 0) {
        out.push(tok)
        continue
      }
      out.push(...splitText(state, tok.content, me))
    }
    block.children = out
  }
}

function text(state: StateCore, content: string): Token {
  const t = new state.Token('text', '', 0)
  t.content = content
  return t
}

function span(state: StateCore, cls: string, content: string): Token[] {
  const open = new state.Token('span_open', 'span', 1)
  open.attrSet('class', cls)
  return [open, text(state, content), new state.Token('span_close', 'span', -1)]
}

function splitText(state: StateCore, content: string, me: string | undefined): Token[] {
  const out: Token[] = []
  let last = 0
  for (const m of content.matchAll(TOKEN_RE)) {
    const start = m.index
    if (m[2] !== undefined) {
      // @mention: the leading separator stays plain text, trailing punctuation is not part of the name
      const name = m[2].replace(/[._-]+$/, '')
      if (!name) continue
      const at = start + m[1]!.length
      out.push(text(state, content.slice(last, at)))
      const lower = name.toLowerCase()
      const cls = lower === me || CHANNEL_WIDE.has(lower) ? 'mention mention-me' : 'mention'
      out.push(...span(state, cls, '@' + name))
      last = at + 1 + name.length
    } else if (m[4] !== undefined) {
      const at = start + m[3]!.length
      out.push(text(state, content.slice(last, at)))
      out.push(...span(state, 'channel-link', '~' + m[4]))
      last = at + 1 + m[4].length
    } else if (m[5] !== undefined) {
      const emoji = nameToEmoji[m[5]]
      if (!emoji) continue
      out.push(text(state, content.slice(last, start)))
      out.push(...span(state, 'emoji', emoji))
      last = start + m[0].length
    }
  }
  out.push(text(state, content.slice(last)))
  return out.filter((t) => t.type !== 'text' || t.content !== '')
}

let purifier: ReturnType<typeof DOMPurify> | null = null

function sanitize(html: string): string {
  if (!purifier) {
    purifier = DOMPurify(window)
    purifier.addHook('uponSanitizeAttribute', (node, data) => {
      const tag = node.nodeName.toLowerCase()
      if (data.attrName === 'class') {
        const keep = tag === 'span' && data.attrValue.split(/\s+/).every((c) => SPAN_CLASSES.has(c))
        if (!keep) data.keepAttr = false
      } else if (data.attrName === 'target' || data.attrName === 'rel') {
        if (tag !== 'a') data.keepAttr = false
      }
    })
    purifier.addHook('afterSanitizeAttributes', (node) => {
      if (node.nodeName.toLowerCase() === 'a') {
        node.setAttribute('target', '_blank')
        node.setAttribute('rel', 'noopener noreferrer')
      }
    })
  }
  return purifier.sanitize(html, {
    ALLOWED_TAGS,
    ALLOWED_ATTR: ['href', 'target', 'rel', 'class'],
    ALLOWED_URI_REGEXP: SAFE_URL,
    ALLOW_DATA_ATTR: false,
    ALLOW_ARIA_ATTR: false,
  })
}

const md = createMarkdown()

/** renderMessage turns a message into sanitized HTML for v-html. */
export function renderMessage(source: string, ctx: RenderContext = {}): string {
  return sanitize(md.render(source, { ...ctx }))
}

/** renderInline is the one-line variant for channel headers and purposes. */
export function renderInline(source: string, ctx: RenderContext = {}): string {
  return sanitize(md.renderInline(source, { ...ctx }))
}

/** emojiFor returns the character of an emoji short name ("+1" → 👍), or the name when unknown. */
export function emojiFor(name: string): string {
  return nameToEmoji[name] ?? `:${name}:`
}

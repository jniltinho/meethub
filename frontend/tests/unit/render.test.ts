import { describe, expect, it } from 'vitest'
import { renderInline, renderMessage } from '@/chat/render'

// what a browser would do with the HTML: parse it and look at the result
function dom(html: string): HTMLElement {
  const div = document.createElement('div')
  div.innerHTML = html
  return div
}

function noScriptVectors(html: string) {
  const el = dom(html)
  expect(el.querySelectorAll('img, script, iframe, object, embed, svg, style, form, input').length).toBe(0)
  for (const node of el.querySelectorAll('*')) {
    for (const attr of node.getAttributeNames()) {
      expect(attr.startsWith('on')).toBe(false)
      expect(['href', 'target', 'rel', 'class']).toContain(attr)
    }
  }
  for (const a of el.querySelectorAll('a')) {
    expect(a.getAttribute('href')).toMatch(/^(https?:|mailto:)/i)
    expect(a.getAttribute('rel')).toBe('noopener noreferrer')
    expect(a.getAttribute('target')).toBe('_blank')
  }
}

describe('message rendering (messages spec: safe rendering)', () => {
  const payloads = [
    '<img src=x onerror=alert(1)>',
    '<script>alert(1)</script>',
    '[x](javascript:alert(1))',
    '[x](JaVaScRiPt:alert(1))',
    '[x](&#106;avascript:alert(1))',
    '[x](&#x6A;avascript&colon;alert(1))',
    '[x](java\tscript:alert(1))',
    '[x](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)',
    '[x](vbscript:msgbox(1))',
    '<a href="javascript:alert(1)">x</a>',
    '<svg onload=alert(1)>',
    '[x](https://ok.example "title" onmouseover=alert(1))',
    '`<img src=x onerror=alert(1)>`',
    '```\n<script>alert(1)</script>\n```',
  ]
  for (const p of payloads) {
    it(`neutralizes ${JSON.stringify(p)}`, () => {
      const html = renderMessage(p)
      noScriptVectors(html)
      expect(html).not.toMatch(/<(img|script|svg)\b/i)
      // a dangerous scheme may remain as escaped text, never as a link
      for (const a of dom(html).querySelectorAll('a')) expect(a.getAttribute('href')).not.toMatch(/script:|data:/i)
    })
  }

  it('turns images into links and loads nothing', () => {
    const el = dom(renderMessage('![t](https://evil.example/pixel)'))
    expect(el.querySelector('img')).toBeNull()
    const a = el.querySelector('a')!
    expect(a.getAttribute('href')).toBe('https://evil.example/pixel')
    expect(a.textContent).toBe('t')
  })

  it('keeps raw HTML as text', () => {
    const el = dom(renderMessage('<b>bold?</b>'))
    expect(el.querySelector('b')).toBeNull()
    expect(el.textContent).toContain('<b>bold?</b>')
  })

  it('renders Markdown with safe links', () => {
    const el = dom(renderMessage('**Deploy** at _17h_: https://ci.example/run/1 and `code`'))
    expect(el.querySelector('strong')!.textContent).toBe('Deploy')
    expect(el.querySelector('em')!.textContent).toBe('17h')
    expect(el.querySelector('a')!.getAttribute('href')).toBe('https://ci.example/run/1')
    expect(el.querySelector('code')!.textContent).toBe('code')
  })

  it('marks mentions, highlighting the viewer and channel-wide ones, but not inside code', () => {
    const el = dom(renderMessage('@ana.silva, @bruno and @channel — `@ana.silva`', { me: 'ana.silva' }))
    const spans = [...el.querySelectorAll('span.mention')].map((s) => [
      s.textContent,
      s.classList.contains('mention-me'),
    ])
    expect(spans).toEqual([
      ['@ana.silva', true],
      ['@bruno', false],
      ['@channel', true],
    ])
    expect(el.querySelector('code')!.textContent).toBe('@ana.silva')
  })

  it('mentions are text: a hostile name cannot inject markup', () => {
    const el = dom(renderMessage('@<img src=x onerror=alert(1)>'))
    expect(el.querySelector('img')).toBeNull()
    noScriptVectors(el.innerHTML)
  })

  it('replaces known emoji names and leaves unknown ones', () => {
    const el = dom(renderMessage('ship it :rocket: :not_an_emoji:'))
    expect(el.querySelector('span.emoji')!.textContent).toBe('🚀')
    expect(el.textContent).toContain(':not_an_emoji:')
  })

  it('channel references and e-mail addresses', () => {
    const el = dom(renderMessage('see ~devops, mail x@ana.io'))
    expect(el.querySelector('span.channel-link')!.textContent).toBe('~devops')
    expect(el.querySelector('span.mention')).toBeNull()
  })

  it('inline variant for headers has no block elements', () => {
    const html = renderInline('# Title\n\n**Deploys** on Fridays: no')
    expect(dom(html).querySelector('p, h1')).toBeNull()
    noScriptVectors(html)
  })
})

// Text transforms of the composer's formatting toolbar: pure functions over the text and the selection, so they
// are unit-tested without a DOM. Each returns the new text and the selection to restore.
export interface Edit {
  text: string
  start: number
  end: number
}

/** wrapSelection surrounds the selection with a mark (bold, italic, code). */
export function wrapSelection(text: string, start: number, end: number, mark: string): Edit {
  return {
    text: text.slice(0, start) + mark + text.slice(start, end) + mark + text.slice(end),
    start: start + mark.length,
    end: end + mark.length,
  }
}

/** prefixLines starts every line touched by the selection with "- " (bulleted) or "1. ", "2. "… (numbered). */
export function prefixLines(text: string, start: number, end: number, numbered: boolean): Edit {
  const from = text.lastIndexOf('\n', start - 1) + 1
  const nl = text.indexOf('\n', end)
  const to = nl < 0 ? text.length : nl
  const lines = text.slice(from, to).split('\n')
  const out = lines.map((l, i) => (numbered ? `${i + 1}. ` : '- ') + l.replace(/^(\s*)(?:[-*]|\d+\.)\s+/, '$1'))
  const block = out.join('\n')
  return { text: text.slice(0, from) + block + text.slice(to), start: from, end: from + block.length }
}

/** linkSelection turns the selection into a Markdown link and selects the address to type over. */
export function linkSelection(text: string, start: number, end: number): Edit {
  const label = text.slice(start, end) || 'text'
  const before = text.slice(0, start) + '[' + label + ']('
  return { text: before + 'https://' + ')' + text.slice(end), start: before.length, end: before.length + 8 }
}

/** insertAt replaces the selection with s and puts the caret after it. */
export function insertAt(text: string, start: number, end: number, s: string): Edit {
  return { text: text.slice(0, start) + s + text.slice(end), start: start + s.length, end: start + s.length }
}

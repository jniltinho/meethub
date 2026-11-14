// Client side of the message search: the words to highlight in a result (the same split the server makes, minus the
// from: and in: filters) and the text cut into plain and matched parts, so results render without v-html.
export function searchTerms(query: string): string[] {
  const terms: string[] = []
  const re = /"([^"]*)"?|(\S+)/g
  for (const m of query.matchAll(re)) {
    const term = (m[1] ?? m[2] ?? '').trim().toLowerCase()
    if (!term || (m[2] && /^(from|in):/i.test(m[2]))) continue
    terms.push(term)
  }
  return terms
}

export interface Part {
  text: string
  hit: boolean
}

/** fold lower-cases a character and drops its accents, as the server's search text does */
function fold(ch: string): string {
  return ch
    .normalize('NFD')
    .replace(/\p{Mn}/gu, '')
    .toLowerCase()
}

/**
 * highlightParts marks where the terms match the way the server matches them (message-search spec): a word at the
 * beginning of a word of the text, a term of several words those words in sequence, ignoring case and accents.
 */
export function highlightParts(text: string, terms: string[]): Part[] {
  // the folded text, with the index in text of each folded character
  let folded = ''
  const at: number[] = []
  for (let i = 0; i < text.length;) {
    const ch = String.fromCodePoint(text.codePointAt(i)!)
    const f = fold(ch)
    folded += f
    for (let k = 0; k < f.length; k++) at.push(i) // one entry per UTF-16 unit, as indexOf counts
    i += ch.length
  }
  at.push(text.length)
  const marks: [number, number][] = []
  for (const term of terms) {
    const words = fold(term)
      .split(/[^\p{L}\p{N}]+/u)
      .filter(Boolean)
    if (words.length === 0) continue
    // at the beginning of a word; a term of several words: those words, whole, in sequence
    const re = new RegExp(
      '(?<![\\p{L}\\p{N}])' + words.join('[^\\p{L}\\p{N}]+') + (words.length > 1 ? '(?![\\p{L}\\p{N}])' : ''),
      'gu',
    )
    for (const m of folded.matchAll(re)) marks.push([at[m.index]!, at[m.index + m[0].length]!])
  }
  marks.sort((a, b) => a[0] - b[0])
  const parts: Part[] = []
  let pos = 0
  for (const [from, to] of marks) {
    if (to <= pos) continue
    const start = Math.max(from, pos)
    if (start > pos) parts.push({ text: text.slice(pos, start), hit: false })
    parts.push({ text: text.slice(start, to), hit: true })
    pos = to
  }
  if (pos < text.length) parts.push({ text: text.slice(pos), hit: false })
  return parts
}

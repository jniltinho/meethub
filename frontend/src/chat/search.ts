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

const wordChar = /[\p{L}\p{N}]/u

/** fold lower-cases a character and drops its accents, as the server's search text does */
function fold(ch: string): string {
  return ch
    .normalize('NFD')
    .replace(/\p{Mn}/gu, '')
    .toLowerCase()
}

/**
 * highlightParts marks where the terms match the way the server matches them (message-search spec): each word of a
 * term at the beginning of a word of the text, ignoring case and accents.
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
  const words = terms.flatMap((t) => fold(t).split(/[^\p{L}\p{N}]+/u)).filter(Boolean)
  const marks: [number, number][] = []
  for (const w of words) {
    for (let i = folded.indexOf(w); i >= 0; i = folded.indexOf(w, i + 1)) {
      if (i > 0 && wordChar.test(folded[i - 1]!)) continue // inside a word
      marks.push([at[i]!, at[i + w.length]!])
    }
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

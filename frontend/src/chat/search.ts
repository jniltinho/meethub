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

export function highlightParts(text: string, terms: string[]): Part[] {
  const lower = text.toLowerCase()
  const marks: [number, number][] = []
  for (const term of terms) {
    if (!term) continue
    for (let i = lower.indexOf(term); i >= 0; i = lower.indexOf(term, i + term.length)) marks.push([i, i + term.length])
  }
  marks.sort((a, b) => a[0] - b[0])
  const parts: Part[] = []
  let at = 0
  for (const [from, to] of marks) {
    if (to <= at) continue
    const start = Math.max(from, at)
    if (start > at) parts.push({ text: text.slice(at, start), hit: false })
    parts.push({ text: text.slice(start, to), hit: true })
    at = to
  }
  if (at < text.length) parts.push({ text: text.slice(at), hit: false })
  return parts
}

import { describe, expect, it } from 'vitest'
import { highlightParts, searchTerms } from '@/chat/search'

const hits = (text: string, q: string) =>
  highlightParts(text, searchTerms(q))
    .filter((p) => p.hit)
    .map((p) => p.text)

describe('search highlights', () => {
  it('marks the beginning of words, ignoring case and accents, as the server matches', () => {
    expect(hits('RELATÓRIO de vendas', 'relatorio')).toEqual(['RELATÓRIO'])
    expect(hits('the deployment and the redeploy', 'deploy')).toEqual(['deploy'])
    expect(hits('Ação rápida', 'acao RAPIDA')).toEqual(['Ação', 'rápida'])
    expect(hits('snake_case here', 'snake_case')).toEqual(['snake_case'])
    expect(hits('release notes are ready', '"release notes" from:ana')).toEqual(['release notes'])
    expect(hits('releases notesdraft; release notes', '"release notes"')).toEqual(['release notes'])
    expect(hits('deploy', 'ploy')).toEqual([])
  })
  it('keeps the text whole', () => {
    const text = 'Señal 🚀 en la canción'
    expect(
      highlightParts(text, ['cancion'])
        .map((p) => p.text)
        .join(''),
    ).toBe(text)
    expect(hits(text, 'cancion senal')).toEqual(['Señal', 'canción'])
  })
})

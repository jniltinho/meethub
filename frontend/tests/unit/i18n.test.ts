import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { englishErrors, errorMessage, locale, t } from '@/i18n'
import ptBR from '@/i18n/pt-BR'
import es from '@/i18n/es'

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((f) => {
    const p = join(dir, f)
    if (statSync(p).isDirectory()) return sources(p)
    return /\.(ts|vue)$/.test(f) && !f.endsWith('.test.ts') ? [p] : []
  })
}

// every literal passed to t('…') in the SPA
function usedKeys(): Set<string> {
  const keys = new Set<string>()
  const re = /\bt\(\s*(['"`])((?:\\.|(?!\1).)*)\1/g
  for (const file of sources(join(__dirname, '../../src'))) {
    for (const m of readFileSync(file, 'utf8').matchAll(re)) keys.add(m[2]!.replace(/\\(['"`])/g, '$1'))
  }
  return keys
}

describe('catalogs', () => {
  const keys = usedKeys()
  for (const [name, catalog] of [
    ['pt-BR', ptBR],
    ['es', es],
  ] as const) {
    it(`${name} translates every string used in the SPA`, () => {
      expect([...keys].filter((k) => !(k in catalog))).toEqual([])
    })
    it(`${name} translates every API error code`, () => {
      expect(Object.keys(englishErrors).filter((c) => !(`error.${c}` in catalog))).toEqual([])
    })
    it(`${name} has no stale entries`, () => {
      const errorKeys = new Set(Object.keys(englishErrors).map((c) => `error.${c}`))
      expect(Object.keys(catalog).filter((k) => !keys.has(k) && !errorKeys.has(k))).toEqual([])
    })
  }
})

describe('t', () => {
  afterEach(() => {
    locale.value = 'en'
  })
  it('is English by default', () => {
    expect(locale.value).toBe('en')
    expect(t('Untranslated source string')).toBe('Untranslated source string')
  })
  it('falls back to English for a missing entry', () => {
    locale.value = 'es'
    expect(t('Untranslated source string')).toBe('Untranslated source string')
  })
  it('fills placeholders', () => {
    expect(t('Hello {name}', { name: 'Ana' })).toBe('Hello Ana')
  })
  it('translates error codes, falling back to the server message', () => {
    locale.value = 'pt-BR'
    expect(errorMessage('invalid_credentials', 'x')).toBe('E-mail ou senha inválidos.')
    expect(errorMessage('some_new_code', 'Server text.')).toBe('Server text.')
  })
})

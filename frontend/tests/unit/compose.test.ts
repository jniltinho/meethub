import { afterEach, describe, expect, it } from 'vitest'
import { insertAt, linkSelection, prefixLines, wrapSelection } from '@/chat/compose'
import { formatDay } from '@/chat/format'
import { locale } from '@/i18n'

describe('composer toolbar transforms', () => {
  it('wraps the selection and keeps it selected', () => {
    expect(wrapSelection('say hello now', 4, 9, '**')).toEqual({ text: 'say **hello** now', start: 6, end: 11 })
    expect(wrapSelection('', 0, 0, '`')).toEqual({ text: '``', start: 1, end: 1 })
  })
  it('numbers the selected lines', () => {
    const e = prefixLines('intro\nfirst\nsecond\nend', 8, 15, true)
    expect(e.text).toBe('intro\n1. first\n2. second\nend')
    expect(e.text.slice(e.start, e.end)).toBe('1. first\n2. second')
  })
  it('bullets the line of the caret and replaces an existing list mark', () => {
    expect(prefixLines('one\ntwo', 5, 5, false).text).toBe('one\n- two')
    expect(prefixLines('1. one\n2. two', 0, 13, false).text).toBe('- one\n- two')
    expect(prefixLines('- one\n- two', 0, 11, true).text).toBe('1. one\n2. two')
  })
  it('makes a link and selects its address', () => {
    const e = linkSelection('see docs here', 4, 8)
    expect(e.text).toBe('see [docs](https://) here')
    expect(e.text.slice(e.start, e.end)).toBe('https://')
    expect(linkSelection('', 0, 0).text).toBe('[text](https://)')
  })
  it('inserts at the caret', () => {
    expect(insertAt('ab', 1, 1, '@')).toEqual({ text: 'a@b', start: 2, end: 2 })
  })
})

describe('day separators carry the date', () => {
  const now = new Date(2026, 0, 21, 15, 0).getTime()
  const day = 86400000
  afterEach(() => {
    locale.value = 'en'
  })
  it('English', () => {
    expect(formatDay(now, now)).toBe('Today, Jan 21')
    expect(formatDay(now - day, now)).toBe('Yesterday, Jan 20')
    expect(formatDay(now - 10 * day, now)).toBe('Sunday, January 11')
    expect(formatDay(now - 40 * day, now)).toMatch(/2025/)
  })
  it('Portuguese', () => {
    locale.value = 'pt-BR'
    expect(formatDay(now, now)).toMatch(/^Hoje, 21 de jan\.?$/)
    expect(formatDay(now - day, now)).toMatch(/^Ontem, 20 de jan\.?$/)
  })
  it('Spanish', () => {
    locale.value = 'es'
    expect(formatDay(now, now)).toMatch(/^Hoy, 21 ene\.?$/)
  })
})

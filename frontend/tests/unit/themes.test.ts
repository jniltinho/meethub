import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { themeCatalog } from '@/themes/catalog'

const root = join(__dirname, '../..')

// registry ids from the Go source of truth
function registry(): { id: string; scheme: string }[] {
  const go = readFileSync(join(root, '../internal/theme/theme.go'), 'utf8')
  return [...go.matchAll(/\{ID: "([^"]+)", Label: "[^"]*", Scheme: "(light|dark)"/g)].map((m) => ({
    id: m[1]!,
    scheme: m[2]!,
  }))
}

// ids that have a [data-theme='id'] rule in the theme CSS files
function styled(): Set<string> {
  const ids = new Set<string>()
  const dir = join(root, 'src/themes')
  for (const f of readdirSync(dir).filter((f) => f.endsWith('.css'))) {
    for (const m of readFileSync(join(dir, f), 'utf8').matchAll(/\[data-theme=['"]([a-z0-9-]+)['"]\]/g)) ids.add(m[1]!)
  }
  return ids
}

describe('theme lockstep', () => {
  const reg = registry()
  it('reads 22 themes from the Go registry', () => {
    expect(reg).toHaveLength(22)
  })
  it('the picker catalog equals the registry, in order and scheme', () => {
    expect(themeCatalog.map((t) => ({ id: t.id, scheme: t.scheme }))).toEqual(reg)
  })
  it('every registry id has a style rule and every style rule a registry id', () => {
    expect([...styled()].sort()).toEqual(reg.map((t) => t.id).sort())
  })
  it('every theme file is imported by style.css', () => {
    const css = readFileSync(join(root, 'src/style.css'), 'utf8')
    const files = readdirSync(join(root, 'src/themes')).filter((f) => f.endsWith('.css'))
    expect(files.filter((f) => !css.includes(`./themes/${f}`))).toEqual([])
  })
})

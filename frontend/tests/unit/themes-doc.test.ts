import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { expect, it } from 'vitest'

it('docs/themes.md lists exactly the roles of the contract', () => {
  const root = join(__dirname, '../..')
  const css = readFileSync(join(root, 'src/style.css'), 'utf8')
  const doc = readFileSync(join(root, '../docs/themes.md'), 'utf8')
  const roles = new Set([...css.matchAll(/^\s+--color-([a-z0-9-]+):/gm)].map((m) => m[1]))
  const documented = new Set([...doc.matchAll(/^\| `--color-([a-z0-9-]+)` \|/gm)].map((m) => m[1]))
  expect([...documented].sort()).toEqual([...roles].sort())
})

import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, test, type Page } from '@playwright/test'
import { themeCatalog } from '../../src/themes/catalog'
import { login, OWNER } from './helpers'

const here = dirname(fileURLToPath(import.meta.url))

interface Recipe {
  id: string
  fg: string
  bg: string[]
  size: 'normal' | 'large' | 'ui'
  aaaOnly?: boolean
  segmentedOnly?: boolean
}
const matrix = JSON.parse(readFileSync(join(here, '../../src/themes/matrix.json'), 'utf8')) as {
  recipes: Recipe[]
  aaa: string[]
}
// MeetHub's own roles, on top of ad-manager's recipes
const meethubRecipes: Recipe[] = [
  { id: 'rail/item', fg: 'on-rail', bg: ['rail'], size: 'normal' },
  { id: 'rail/on', fg: 'on-rail-on', bg: ['rail-on'], size: 'normal' },
  { id: 'sidebar/text', fg: 'on-sidebar', bg: ['sidebar'], size: 'normal' },
  { id: 'sidebar/muted', fg: 'on-sidebar-muted', bg: ['sidebar'], size: 'normal' },
  { id: 'sidebar/on', fg: 'on-sidebar-on', bg: ['sidebar-on'], size: 'normal' },
  { id: 'toast/info', fg: 'on-toast-info', bg: ['toast-info'], size: 'normal' },
  { id: 'alert/info', fg: 'on-info-soft', bg: ['info-soft'], size: 'normal' },
  { id: 'alert/ok', fg: 'on-ok-soft', bg: ['ok-soft'], size: 'normal' },
  { id: 'alert/warn', fg: 'on-warn-soft', bg: ['warn-soft'], size: 'normal' },
  { id: 'alert/err', fg: 'on-err-soft', bg: ['err-soft'], size: 'normal' },
  { id: 'field-error', fg: 'err', bg: ['surface'], size: 'normal' },
  // chat (chat-ui spec): mention badges, the viewer's mention highlight, presence rings, avatars, hovered sidebar rows
  { id: 'chat/mention-badge', fg: 'on-err-fill', bg: ['err-fill'], size: 'normal' },
  { id: 'chat/mention-me', fg: 'on-warn-soft', bg: ['warn-soft'], size: 'normal' },
  { id: 'chat/mention', fg: 'link', bg: ['surface'], size: 'normal' },
  { id: 'chat/mention-hover', fg: 'link', bg: ['row-hover'], size: 'normal' },
  { id: 'chat/presence-ring', fg: 'on-sidebar', bg: ['sidebar'], size: 'ui' },
  { id: 'chat/avatar', fg: 'on-strong-fill', bg: ['strong-fill'], size: 'normal' },
  { id: 'chat/sidebar-hover', fg: 'on-sidebar', bg: ['sidebar-hover'], size: 'normal' },
  { id: 'chat/composer', fg: 'on-field', bg: ['field'], size: 'normal' },
  // the approved layout: details and search in the side panel, pinned banner and mark, search marks, jump highlight
  { id: 'chat/details', fg: 'fg', bg: ['panel'], size: 'normal' },
  { id: 'chat/details-muted', fg: 'fg-muted', bg: ['panel'], size: 'normal' },
  { id: 'chat/pinned-banner', fg: 'fg', bg: ['surface-2'], size: 'normal' },
  { id: 'chat/pinned-banner-muted', fg: 'fg-muted', bg: ['surface-2'], size: 'normal' },
  { id: 'chat/pinned-mark', fg: 'link', bg: ['surface'], size: 'normal' },
  { id: 'chat/search-mark', fg: 'on-warn-soft', bg: ['warn-soft', 'panel'], size: 'normal' },
  { id: 'chat/jump-highlight', fg: 'on-warn-soft', bg: ['warn-soft'], size: 'normal' },
  { id: 'chat/menu-hover', fg: 'on-selection', bg: ['selection'], size: 'normal' },
  { id: 'chat/favorite-star', fg: 'warn', bg: ['surface'], size: 'ui' },
  // files: cards and rows (also hovered), the tray's error, the drop overlay, the channel header's flat actions
  { id: 'chat/file-name', fg: 'fg-strong', bg: ['row-alt', 'row-hover', 'panel'], size: 'normal' },
  { id: 'chat/file-meta', fg: 'fg-muted', bg: ['row-alt', 'row-hover', 'panel'], size: 'normal' },
  { id: 'chat/upload-error', fg: 'err', bg: ['row-alt'], size: 'normal' },
  { id: 'chat/drop-overlay', fg: 'fg-strong', bg: ['panel'], size: 'normal' },
  { id: 'chat/header-action', fg: 'fg-muted', bg: ['surface'], size: 'normal' },
  { id: 'chat/header-action-hover', fg: 'fg', bg: ['row-hover'], size: 'normal' },
  // meetings: the room's stage, tile labels and marks, round controls, the panel, the details card and tag
  { id: 'meeting/stage', fg: 'on-page', bg: ['page'], size: 'normal' },
  { id: 'meeting/tile-label', fg: 'on-page', bg: ['page'], size: 'normal' },
  { id: 'meeting/speaking', fg: 'focus', bg: ['surface'], size: 'ui' },
  { id: 'meeting/hand', fg: 'on-warn-fill', bg: ['warn-fill'], size: 'normal' },
  { id: 'meeting/control', fg: 'fg', bg: ['surface-2'], size: 'ui' },
  { id: 'meeting/control-on', fg: 'on-selection', bg: ['selection'], size: 'ui' },
  { id: 'meeting/control-off', fg: 'on-danger', bg: ['danger'], size: 'ui' },
  { id: 'meeting/panel', fg: 'fg', bg: ['panel'], size: 'normal' },
  { id: 'meeting/card', fg: 'fg', bg: ['meeting-card'], size: 'normal' },
  { id: 'meeting/card-muted', fg: 'fg-muted', bg: ['meeting-card'], size: 'normal' },
  { id: 'meeting/card-icon', fg: 'on-action', bg: ['action'], size: 'ui' },
  { id: 'meeting/in-room', fg: 'on-ok-soft', bg: ['ok-soft'], size: 'normal' },
  { id: 'meeting/sidebar-count', fg: 'on-sidebar-muted', bg: ['sidebar'], size: 'normal' },
]
const recipes = [...matrix.recipes.filter((r) => !r.segmentedOnly), ...meethubRecipes]

// every role of the contract: the --color-* custom properties declared in style.css
const contractRoles = [
  ...new Set(
    [...readFileSync(join(here, '../../src/style.css'), 'utf8').matchAll(/^\s+--color-([a-z0-9-]+):/gm)].map(
      (m) => m[1]!,
    ),
  ),
]

// resolve evaluates roles in the page: each role's computed color as sRGB [r, g, b, a] through a canvas.
async function resolve(page: Page, theme: string, roles: string[]) {
  return page.evaluate(
    ({ theme, roles }) => {
      document.documentElement.setAttribute('data-theme', theme)
      const probe = document.createElement('div')
      document.body.append(probe)
      const ctx = document.createElement('canvas').getContext('2d', { willReadFrequently: true })!
      const out: Record<string, number[] | null> = {}
      for (const role of roles) {
        probe.style.color = 'rgb(1, 2, 3)'
        probe.style.color = `var(--color-${role})`
        const c = getComputedStyle(probe).color
        if (!c || c === 'rgb(1, 2, 3)' || !CSS.supports('color', c)) {
          out[role] = null
          continue
        }
        ctx.clearRect(0, 0, 1, 1)
        ctx.fillStyle = '#000'
        ctx.fillStyle = c
        ctx.fillRect(0, 0, 1, 1)
        const [r, g, b, a] = ctx.getImageData(0, 0, 1, 1).data
        out[role] = [r!, g!, b!, a! / 255]
      }
      probe.remove()
      return out
    },
    { theme, roles },
  )
}

const lum = ([r, g, b]: number[]) => {
  const ch = (v: number) => {
    const s = v / 255
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
  }
  return 0.2126 * ch(r!) + 0.7152 * ch(g!) + 0.0722 * ch(b!)
}
const over = (top: number[], under: number[]) => {
  const a = top[3]!
  return [0, 1, 2].map((i) => top[i]! * a + under[i]! * (1 - a)).concat(1)
}
const ratio = (a: number[], b: number[]) => {
  const [l1, l2] = [lum(a), lum(b)].sort((x, y) => y - x)
  return (l1! + 0.05) / (l2! + 0.05)
}

test.describe('every theme', () => {
  test.beforeEach(async ({ page }) => {
    await login(page)
    await page.goto('/__gallery')
    await expect(page.getByRole('heading', { name: 'Feedback gallery' })).toBeVisible() // profile and theme applied
  })

  for (const theme of themeCatalog) {
    test(`${theme.id}: contract roles resolve and meet WCAG AA`, async ({ page }) => {
      const roles = [...new Set([...contractRoles, ...recipes.flatMap((r) => [r.fg, ...r.bg]), 'page'])]
      const colors = await resolve(page, theme.id, roles)
      expect(
        roles.filter((r) => colors[r] === null),
        'unresolved roles',
      ).toEqual([])

      const aaa = matrix.aaa.includes(theme.id)
      const failures: string[] = []
      for (const r of recipes) {
        if (r.aaaOnly && !aaa) continue
        let bg = [255, 255, 255, 1]
        for (const layer of [...r.bg, 'page'].reverse()) bg = over(colors[layer]!, bg)
        const fg = over(colors[r.fg]!, bg)
        const need = r.size === 'normal' ? (aaa ? 7 : 4.5) : aaa && r.size === 'large' ? 4.5 : 3
        const got = ratio(fg, bg)
        if (got < need - 0.01) failures.push(`${r.id} ${got.toFixed(2)} < ${need}`)
      }
      expect(failures, `contrast failures in ${theme.id}`).toEqual([])

      // the shared corner radii: 6 px on controls, 8 px on containers, in every theme
      const radii = await page.evaluate(() => {
        const of = (sel: string) => [
          ...new Set([...document.querySelectorAll(sel)].map((e) => getComputedStyle(e).borderRadius)),
        ]
        return { controls: of('.btn, .btn-ghost, .btn-danger, .inp'), containers: of('.alert, .toast, dialog.pnl') }
      })
      expect(radii).toEqual({ controls: ['6px'], containers: ['8px'] })
    })
  }

  test('meethub-dark primary button uses the AAA primary: white on it reaches 7:1', async ({ page }) => {
    const c = await resolve(page, 'meethub-dark', ['action', 'on-action'])
    expect(c.action!.slice(0, 3)).toEqual([0x3d, 0x41, 0xcf])
    expect(ratio(c['on-action']!, c.action!)).toBeGreaterThanOrEqual(7)
  })
})

test.describe('theme precedence', () => {
  test('a fresh dark browser paints the organization default first', async ({ browser }) => {
    const ctx = await browser.newContext({ colorScheme: 'dark' })
    const page = await ctx.newPage()
    await page.addInitScript(() => {
      const w = window as unknown as { __firstTheme?: string }
      new MutationObserver((_, obs) => {
        if (document.body) {
          w.__firstTheme = document.documentElement.dataset.theme
          obs.disconnect()
        }
      }).observe(document, { childList: true, subtree: true })
    })
    await page.goto('/login')
    expect(await page.evaluate(() => (window as unknown as { __firstTheme: string }).__firstTheme)).toBe('meethub-dark')
    await ctx.close()
  })

  test('a light browser gets meethub-light', async ({ browser }) => {
    const ctx = await browser.newContext({ colorScheme: 'light' })
    const page = await ctx.newPage()
    await page.goto('/login')
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'meethub-light')
    await ctx.close()
  })

  test('the chosen theme follows the user, survives a reload without a flash, and is forgotten on logout', async ({
    browser,
  }) => {
    const a = await browser.newContext()
    const pa = await a.newPage()
    await login(pa)
    await pa.goto('/settings/appearance')
    await pa.locator("[data-theme-id='matrix']").click()
    await expect(pa.locator('html')).toHaveAttribute('data-theme', 'matrix')

    // reload: the theme is set before <body> exists (no frame with another theme)
    await pa.addInitScript(() => {
      const w = window as unknown as { __firstTheme?: string }
      new MutationObserver((_, obs) => {
        if (document.body) {
          w.__firstTheme = document.documentElement.dataset.theme
          obs.disconnect()
        }
      }).observe(document, { childList: true, subtree: true })
    })
    await pa.reload()
    expect(await pa.evaluate(() => (window as unknown as { __firstTheme: string }).__firstTheme)).toBe('matrix')

    // another browser: the preference comes from the server after login
    const b = await browser.newContext()
    const pb = await b.newPage()
    await login(pb, OWNER.email, OWNER.password)
    await expect(pb.locator('html')).toHaveAttribute('data-theme', 'matrix')

    // shared browser: logout returns to the organization default
    await pa.getByRole('button', { name: 'Log out' }).click()
    await expect(pa).toHaveURL(/\/login/)
    await expect(pa.locator('html')).toHaveAttribute('data-theme', 'meethub-dark')

    // put the owner back on the default theme for the next specs
    await pb.goto('/settings/appearance')
    await pb.locator("[data-theme-id='meethub-dark']").click()
    await expect(pb.locator('html')).toHaveAttribute('data-theme', 'meethub-dark')
    await a.close()
    await b.close()
  })
})

import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import { login } from './helpers'

async function axe(page: Page) {
  const r = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()
  return r.violations
    .filter((v) => v.impact === 'serious' || v.impact === 'critical')
    .map((v) => `${v.id}: ${v.nodes[0]?.target}`)
}

test('phone width: drawer sidebar, bottom bar, no horizontal scroll', async ({ browser }) => {
  const ctx = await browser.newContext({ viewport: { width: 360, height: 740 } })
  const page = await ctx.newPage()
  await login(page)
  await expect(page.locator('.sidebar')).not.toBeInViewport()
  await expect(page.locator('.side-panel')).toBeHidden() // the details panel is for wide windows only
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await page.getByRole('button', { name: 'Open menu' }).click()
  await expect(page.locator('.sidebar')).toBeInViewport()
  await page.getByRole('button', { name: 'Close menu' }).click()
  await page.getByRole('link', { name: 'Meetings' }).click()
  await expect(page).toHaveURL(/\/meetings$/)
  await ctx.close()
})

test('desktop: four regions and a collapsible side panel', async ({ page }) => {
  await login(page)
  await expect(page.getByRole('navigation', { name: 'Main navigation' })).toBeVisible()
  const panel = page.getByRole('complementary', { name: 'Side panel' })
  await expect(panel).toBeVisible()
  await page.getByRole('button', { name: 'Close side panel' }).click()
  await expect(panel).toBeHidden()
  await page.getByRole('button', { name: 'Open side panel' }).click()
  await expect(panel).toBeVisible()
})

test('Full HD, QHD and 4K: the shell fits the window at every zoom step and the page never scrolls', async ({
  browser,
}) => {
  // QHD and 4K without OS scaling zoom the layout (style.css: 1.25 and 1.6); 100dvh ignores that zoom
  for (const [width, height, zoom] of [
    [1920, 1080, '1'],
    [2560, 1440, '1.25'],
    [3840, 2160, '1.6'],
  ] as const) {
    const ctx = await browser.newContext({ viewport: { width, height } })
    const page = await ctx.newPage()
    await login(page)
    await page.goto('/conversations')
    await expect(page.locator('[data-composer]')).toBeFocused() // focusing used to scroll a too-tall shell
    const box = await page.evaluate(() => {
      const r = document.querySelector('.shell')!.getBoundingClientRect()
      const c = document.querySelector('[data-composer]')!.getBoundingClientRect()
      return {
        zoom: getComputedStyle(document.documentElement).zoom,
        top: Math.round(r.top),
        bottomGap: Math.round(window.innerHeight - r.bottom),
        rightGap: Math.round(window.innerWidth - r.right),
        composerInside: c.bottom <= window.innerHeight,
        scrolled: document.body.scrollTop + document.documentElement.scrollTop,
      }
    })
    expect(box, `${width}x${height}`).toEqual({
      zoom,
      top: 0,
      bottomGap: 0,
      rightGap: 0,
      composerInside: true,
      scrolled: 0,
    })
    await ctx.close()
  }
})

test('no serious accessibility violations', async ({ page }) => {
  await page.goto('/login')
  expect(await axe(page), 'login').toEqual([])
  await login(page)
  expect(await axe(page), 'layout').toEqual([])
  await page.goto('/settings/appearance')
  expect(await axe(page), 'settings').toEqual([])
  await page.goto('/__gallery')
  expect(await axe(page), 'gallery').toEqual([])
})

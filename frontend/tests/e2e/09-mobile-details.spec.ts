// Channel details on phones and tablets (add-mobile-channel-details): below 1024 px the Members action and the
// "Channel details" menu item open the details full screen; the close button, Escape and Back close them and the
// focus returns to the control that opened them. Runs after the chat suites (#projects exists).
import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Browser, type Page } from '@playwright/test'
import { composer, login } from './helpers'

async function phoneOrTablet(browser: Browser, width: number): Promise<Page> {
  const ctx = await browser.newContext({ viewport: { width, height: 800 } })
  const page = await ctx.newPage()
  await login(page)
  await expect(page).toHaveURL(/\/channels\//)
  const team = decodeURIComponent(new URL(page.url()).pathname.split('/')[2]!)
  await page.goto(`/teams/${team}/channels/projects`)
  await expect(composer(page)).toBeVisible()
  return page
}

const details = (page: Page) => page.locator('.side-panel .channel-details')
const members = (page: Page) => page.locator('.channel-view .channel-members')

async function serious(page: Page) {
  const r = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()
  return r.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical').map((v) => v.id)
}

for (const width of [390, 820]) {
  test(`${width} px: Members opens the details full screen; close button, Escape and Back close them`, async ({
    browser,
  }) => {
    const page = await phoneOrTablet(browser, width)
    await expect(details(page)).toBeHidden() // nothing asked for yet

    await members(page).click()
    await expect(details(page)).toBeVisible()
    await expect(details(page).getByRole('heading', { name: /participants/i })).toBeVisible()
    const box = await page.locator('.side-panel').boundingBox()
    expect(box?.width).toBe(width) // the whole width: the conversation is behind
    await expect(page.locator('#details-members')).toBeFocused()
    expect(await serious(page), 'details full screen').toEqual([])

    // the close button
    await page.getByRole('button', { name: 'Close details' }).click()
    await expect(details(page)).toBeHidden()
    await expect(members(page)).toBeFocused()

    // Escape
    await members(page).click()
    await expect(details(page)).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(details(page)).toBeHidden()
    await expect(members(page)).toBeFocused()

    // the browser's Back, which stays on the channel
    const url = page.url()
    await members(page).click()
    await expect(details(page)).toBeVisible()
    await page.goBack()
    await expect(details(page)).toBeHidden()
    expect(page.url()).toBe(url)
    await expect(members(page)).toBeFocused()
    await page.context().close()
  })

  test(`${width} px: "Channel details" in the channel menu opens them; closing focuses the menu button`, async ({
    browser,
  }) => {
    const page = await phoneOrTablet(browser, width)
    await page.getByRole('button', { name: 'More actions' }).click()
    await page.getByRole('menuitem', { name: 'Channel details' }).click()
    await expect(details(page)).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(details(page)).toBeHidden()
    await expect(page.getByRole('button', { name: 'More actions' })).toBeFocused()
    await page.context().close()
  })
}

test('1280 px: the details stay in the side panel, without the close button', async ({ page }) => {
  await login(page)
  await expect(page).toHaveURL(/\/channels\//)
  const team = decodeURIComponent(new URL(page.url()).pathname.split('/')[2]!)
  await page.goto(`/teams/${team}/channels/projects`)
  await members(page).click()
  await expect(details(page)).toBeVisible()
  await expect(page.getByRole('button', { name: 'Close details' })).toBeHidden()
  const box = await page.locator('.side-panel').boundingBox()
  expect(box?.width).toBeLessThan(400)
})

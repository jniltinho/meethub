import { expect, test } from '@playwright/test'
import { themeCatalog } from '../../src/themes/catalog'
import { login } from './helpers'

test.beforeEach(async ({ page }) => {
  await login(page)
  await page.goto('/__gallery')
  await expect(page.getByRole('heading', { name: 'Feedback gallery' })).toBeVisible() // profile and theme applied
})

test('toasts: four kinds, errors announced assertively, close button', async ({ page }) => {
  await page.locator('[data-open=toasts]').click()
  await expect(page.locator('.toast')).toHaveCount(4)
  await expect(page.locator('.toast.err')).toHaveAttribute('aria-live', 'assertive')
  await expect(page.locator('.toast.info')).toHaveAttribute('aria-live', 'polite')
  await page.locator('.toast.err').getByRole('button', { name: 'Close' }).click()
  await expect(page.locator('.toast')).toHaveCount(3)
})

test('confirm dialog: modal, safe focus, Escape cancels and focus returns', async ({ page }) => {
  const opener = page.locator('[data-open=confirm]')
  await opener.focus()
  await page.keyboard.press('Enter')
  const dialog = page.getByRole('alertdialog', { name: 'Confirmation' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(opener).toBeFocused()
})

test('typed confirm enables only with the expected text', async ({ page }) => {
  await page.locator('[data-open=typed]').click()
  const dialog = page.getByRole('alertdialog')
  const ok = dialog.getByRole('button', { name: 'Confirm' })
  await expect(ok).toBeDisabled()
  await dialog.getByRole('textbox').fill('ANA@example.com ')
  await expect(ok).toBeEnabled()
  await ok.click()
  await expect(dialog).toBeHidden()
})

test('alert dialog closes with OK', async ({ page }) => {
  await page.locator('[data-open=alert]').click()
  const dialog = page.getByRole('dialog', { name: 'Notice' })
  await expect(dialog.getByRole('button', { name: 'OK' })).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(dialog).toBeHidden()
})

test('validation: no browser bubble, the themed invalid state', async ({ page }) => {
  await page.locator('[data-open=validate]').click()
  await expect(page.locator('#g-email')).toHaveClass(/invalid/)
  await expect(page.locator('#g-email')).toHaveAttribute('aria-invalid', 'true')
  await page.locator('#g-email').fill('x')
  await expect(page.locator('#g-email')).not.toHaveClass(/invalid/)
})

// visual baselines (the CSP forbids injected styles, so motion is stopped by Playwright itself)
test.describe('screenshots', () => {
  test.use({ reducedMotion: 'reduce' })
  // every toast, alert and the dialog in every theme (baselines committed under __screenshots__)
  for (const theme of themeCatalog) {
    test(`gallery screenshot: ${theme.id}`, async ({ page }) => {
      await page.evaluate((id) => document.documentElement.setAttribute('data-theme', id), theme.id)
      await page.locator('[data-open=toasts]').click()
      await expect(page.locator('.toast.show')).toHaveCount(4)
      await page.locator('[data-open=confirm]').click()
      await expect(page.getByRole('alertdialog')).toBeVisible()
      await expect(page).toHaveScreenshot(`gallery-${theme.id}.png`, { animations: 'disabled', caret: 'hide' })
    })
  }
})

import { expect, test, type Page } from '@playwright/test'
import { OWNER } from './helpers'

// The e2e stack configures the throwaway provider of tests/e2e/idp ("Acme ID", auto-creation for example.com).
test.describe.configure({ mode: 'serial' })
test.use({ ignoreHTTPSErrors: true }) // the provider's certificate is made when its container starts

async function viaProvider(page: Page, email: string, verified = true) {
  await page.context().clearCookies()
  await page.goto('/login')
  await page.getByRole('link', { name: 'Sign in with Acme ID' }).click()
  await expect(page.getByRole('heading', { name: 'Test provider' })).toBeVisible()
  await page.getByLabel('E-mail').fill(email)
  if (!verified) await page.getByLabel('Verified').uncheck()
  await page.getByRole('button', { name: 'Continue' }).click()
}

test('both ways offered: the password form and the single sign-on button', async ({ page }) => {
  await page.context().clearCookies()
  await page.goto('/login')
  await expect(page.getByRole('link', { name: 'Sign in with Acme ID' })).toBeVisible()
  await expect(page.getByLabel('E-mail')).toBeVisible()
  await expect(page.getByLabel('Password', { exact: true })).toBeVisible()
})

test('an existing person signs in through the provider', async ({ page }) => {
  await viaProvider(page, OWNER.email)
  await expect(page).toHaveURL(/\/channels\/general$/)
})

test('a new person of an allowed domain gets an account', async ({ page }) => {
  await viaProvider(page, 'sso.new@example.com')
  await expect(page).not.toHaveURL(/\/login/)
  const me = await page.evaluate(async () => (await fetch('/api/v1/me')).json())
  expect(me.user.email).toBe('sso.new@example.com')
  expect(me.role).toBe('member')
})

test('a refused sign-in returns to the login page with a generic message', async ({ page }) => {
  await viaProvider(page, OWNER.email, false) // e-mail not verified
  await expect(page).toHaveURL(/\/login\?sso=failed/)
  await expect(page.locator('.alert-error')).toHaveText('The sign-in could not be completed.')
})

test('single sign-on only: no password form', async ({ page }) => {
  await page.context().clearCookies()
  await page.route('**/api/v1/bootstrap', async (route) => {
    const res = await route.fetch()
    await route.fulfill({ response: res, json: { ...(await res.json()), password_login: false } })
  })
  await page.goto('/login')
  await expect(page.getByRole('link', { name: 'Sign in with Acme ID' })).toBeVisible()
  await expect(page.getByLabel('E-mail')).toHaveCount(0)
})

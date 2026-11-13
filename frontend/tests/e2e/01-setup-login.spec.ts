import { expect, test } from '@playwright/test'
import { collectCspViolations, login, OWNER, SETUP_TOKEN } from './helpers'

test.describe.configure({ mode: 'serial' })

test('first run: the setup page, a wrong token, then the owner is created', async ({ page }) => {
  const csp = await collectCspViolations(page)
  await page.goto('/conversations')
  await expect(page).toHaveURL(/\/setup/)
  await expect(page.getByRole('heading', { name: 'Set up MeetHub' })).toBeVisible()

  // empty submit: themed invalid state, no browser bubble
  await page.getByRole('button', { name: 'Create organization' }).click()
  await expect(page.locator('.field-error').first()).toHaveText('This field is required.')
  await expect(page.locator('#token')).toHaveClass(/invalid/)

  const fill = async (token: string) => {
    await page.locator('#token').fill(token)
    await page.locator('#org').fill('Example Company Ltd')
    await page.locator('#name').fill(OWNER.name)
    await page.locator('#email').fill(OWNER.email)
    await page.locator('#password').fill(OWNER.password)
    await page.getByRole('button', { name: 'Create organization' }).click()
  }
  await fill('wrong-token')
  await expect(page.getByRole('alert')).toContainText('The setup token is not valid.')
  await fill(SETUP_TOKEN)
  await expect(page).toHaveURL(/\/conversations/)
  await expect(page.getByRole('heading', { name: 'Conversations' })).toBeFocused()
  expect(await csp()).toEqual([])
})

test('deep link while logged out returns there after login; open redirects are refused', async ({ page }) => {
  await page.goto('/settings/appearance')
  await expect(page).toHaveURL(/\/login\?next=(%2F|\/)settings(%2F|\/)appearance/)
  await page.getByLabel('E-mail').fill(OWNER.email)
  await page.getByLabel('Password', { exact: true }).fill(OWNER.password)
  await page.getByRole('button', { name: 'Log in' }).click()
  await expect(page).toHaveURL(/\/settings\/appearance$/)

  await page.context().clearCookies()
  await page.goto('/login?next=https://evil.example/')
  await page.getByLabel('E-mail').fill(OWNER.email)
  await page.getByLabel('Password', { exact: true }).fill(OWNER.password)
  await page.getByRole('button', { name: 'Log in' }).click()
  await expect(page).toHaveURL(/127\.0\.0\.1|localhost|meethub/)
  await expect(page).toHaveURL(/\/conversations$/)
})

test('a wrong password shows a themed error alert', async ({ page }) => {
  await page.goto('/login')
  await page.getByLabel('E-mail').fill(OWNER.email)
  await page.getByLabel('Password', { exact: true }).fill('not the password')
  await page.getByRole('button', { name: 'Log in' }).click()
  await expect(page.locator('.alert-error')).toContainText('Invalid e-mail or password.')
})

test('English by default, Portuguese and Spanish on request', async ({ page }) => {
  await page.goto('/login')
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Log in to your workspace')
  await page.getByLabel('Language').selectOption('pt-BR')
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Entre no seu espaço de trabalho')
  await page.getByLabel('Idioma').selectOption('es')
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Inicie sesión en su espacio de trabajo')
  await page.getByLabel('Idioma').selectOption('en')
})

test('logging out ends the session', async ({ page }) => {
  await login(page)
  await page.getByRole('button', { name: 'Log out' }).click()
  await expect(page).toHaveURL(/\/login/)
  await page.goto('/conversations')
  await expect(page).toHaveURL(/\/login/)
})

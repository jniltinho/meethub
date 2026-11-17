import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import { collectCspViolations, login, OWNER, SETUP_TOKEN } from './helpers'

test.describe.configure({ mode: 'serial' })

async function axe(page: Page) {
  const r = await new AxeBuilder({ page }).include('main').withTags(['wcag2a', 'wcag2aa']).analyze()
  return r.violations
    .filter((v) => v.impact === 'serious' || v.impact === 'critical')
    .map((v) => `${v.id}: ${v.nodes[0]?.target}`)
}

// The setup wizard (setup-wizard spec): the token, the diagnosis, the account, the optional steps through
// Administration's routes, the review and Finish applying one setting through the restart.
test('first run: the wizard from the token to a set-up server', async ({ page }) => {
  test.setTimeout(240_000)
  const csp = await collectCspViolations(page)
  const heading = page.locator('[data-wizard-heading]')
  const next = page.locator('[data-wizard-next]')
  await page.goto('/conversations')
  await expect(page).toHaveURL(/\/setup/)
  await expect(page.getByRole('heading', { name: 'Set up MeetHub' })).toBeVisible()
  await expect(heading).toHaveText('Access')
  await expect(page.locator('[data-wizard-position]')).toHaveText('Step 1 of 7')
  await expect(page.locator('[data-step="access"]')).toHaveAttribute('aria-current', 'step')
  await expect(page.locator('main code').first()).toContainText('setup-token')
  expect(await axe(page), 'the token step').toEqual([])

  // empty: themed invalid state, no browser bubble; a wrong token is refused
  await next.click()
  await expect(page.locator('.field-error').first()).toHaveText('This field is required.')
  await expect(page.locator('#token')).toHaveClass(/invalid/)
  await page.locator('#token').fill('wrong-token')
  await next.click()
  await expect(page.locator('[data-wizard-error]')).toContainText('The setup token is not valid.')
  await page.locator('#token').fill(SETUP_TOKEN)
  await next.click()

  // the diagnosis: nothing blocks, what is optional is a note, the heading has the focus
  await expect(heading).toHaveText('This server')
  await expect(page.locator('.wizard-heading')).toBeFocused()
  await expect(page.locator('[data-part="database"][data-level="ok"]')).toContainText('Database (')
  await expect(page.locator('[data-part="redis"][data-level="ok"]')).toBeVisible()
  await expect(page.locator('[data-part="email"][data-level="note"]')).toContainText('No e-mail is sent')
  await expect(page.locator('[data-level="problem"]')).toHaveCount(0)
  expect(await axe(page), 'the diagnosis').toEqual([])
  // the MeetHub themes hold the wizard to enhanced contrast (AAA), the stepper and the hints included
  for (const theme of ['meethub-dark', 'meethub-light']) {
    await page.evaluate((id) => (document.documentElement.dataset.theme = id), theme)
    const r = await new AxeBuilder({ page }).include('main').withTags(['wcag2aa', 'wcag2aaa']).analyze()
    expect(
      r.violations.map((v) => `${v.id}: ${v.nodes[0]?.target}`),
      `enhanced contrast in ${theme}`,
    ).toEqual([])
  }
  await page.locator('[data-diagnosis-again]').click()
  await expect(page.locator('[data-part="database"][data-level="ok"]')).toBeVisible()
  await page.locator('[data-step="access"]').click() // a finished step is reachable again
  await expect(page.locator('#token')).toHaveValue(SETUP_TOKEN)
  await next.click()
  await next.click()

  // the account: hints, the password twice
  await expect(heading).toHaveText('Organization and account')
  await page.locator('#org').fill('Example Company Ltd')
  await page.locator('#name').fill(OWNER.name)
  await page.locator('#email').fill(OWNER.email)
  await page.locator('#password').fill(OWNER.password)
  await page.locator('#again').fill('another password!')
  await page.getByRole('button', { name: 'Create organization' }).click()
  await expect(page.locator('.field-error').first()).toHaveText('The two passwords are not the same.')
  await page.locator('#again').fill(OWNER.password)
  expect(await axe(page), 'the account step').toEqual([])
  await page.getByRole('button', { name: 'Create organization' }).click()

  // the optional steps are the owner's: what the environment sets is locked, the token step is closed
  await expect(heading).toHaveText('Meetings and recordings')
  await expect(page.locator('[data-step="access"]')).toBeDisabled()
  await expect(page.locator('[data-setting="recordings.enabled"] [data-locked]')).toContainText(
    'Set by MEETHUB_RECORDINGS_ENABLED',
  )
  await expect(page.locator('[data-setting="recordings.mode"] select')).toBeEnabled()
  await next.click()
  await expect(heading).toHaveText('AI summaries')
  await expect(page.locator('[data-wizard-ai-key] [data-locked]')).toContainText('Set by MEETHUB_AI_API_KEY')
  await expect(page.locator('main')).not.toContainText('e2e-throwaway-ai-key')
  const language = page.locator('[data-setting="ai.language"] input')
  await expect(language).toBeEnabled()
  await language.fill('en')
  expect(await axe(page), 'the AI step').toEqual([])
  await next.click()

  // the review names the change; Finish applies it through the restart and asks to keep it
  await expect(heading).toHaveText('Review')
  await expect(page.locator('[data-review-list]')).toContainText('Language of the meetings: en')
  await expect(page.locator('[data-reauth]')).toHaveCount(0) // nothing sensitive
  await page.locator('[data-wizard-finish]').click()
  await expect(heading).toHaveText('Finish')
  await expect(page.locator('[data-job="apply"]')).toHaveAttribute('data-state', 'running')
  await page.locator('[data-settings-keep]').click({ timeout: 120_000 })
  await expect(page.locator('[data-wizard-done]')).toContainText('MeetHub is set up.', { timeout: 90_000 })
  await expect(page.locator('[data-job="apply"]')).toHaveAttribute('data-state', 'done')
  expect(await axe(page), 'the last step').toEqual([])

  // an owner comes back to the optional steps; the change is in effect
  await page.goto('/setup')
  await expect(heading).toHaveText('Meetings and recordings')
  await next.click()
  await expect(page.locator('[data-setting="ai.language"] input')).toHaveValue('en')
  await next.click()
  await expect(page.locator('[data-review-nothing]')).toBeVisible()
  await page.locator('[data-wizard-finish]').click()
  await page.locator('[data-wizard-open]').click()
  // Conversations opens the default channel of the owner's team, focus in its composer
  await expect(page).toHaveURL(/\/channels\/general$/)
  await expect(page.locator('[data-composer]')).toBeFocused()
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
  await expect(page).toHaveURL(/\/channels\/general$/)
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

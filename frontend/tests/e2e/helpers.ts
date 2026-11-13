import { expect, type Page } from '@playwright/test'

export const SETUP_TOKEN = process.env.E2E_SETUP_TOKEN ?? 'e2e-setup-token'
export const OWNER = { name: 'Ana Silva', email: 'ana@example.com', password: 'a long password!' }

export async function login(page: Page, email = OWNER.email, password = OWNER.password): Promise<void> {
  await page.goto('/login')
  await page.getByLabel('E-mail').fill(email)
  await page.getByLabel('Password', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Log in' }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

// collectCspViolations records every CSP violation the page reports.
export async function collectCspViolations(page: Page): Promise<() => Promise<string[]>> {
  await page.addInitScript(() => {
    const w = window as unknown as { __csp: string[] }
    w.__csp = []
    document.addEventListener('securitypolicyviolation', (e) => w.__csp.push(`${e.violatedDirective} ${e.blockedURI}`))
  })
  return () => page.evaluate(() => (window as unknown as { __csp: string[] }).__csp)
}

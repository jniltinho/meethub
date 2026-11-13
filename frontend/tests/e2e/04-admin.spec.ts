import { expect, test } from '@playwright/test'
import { login } from './helpers'

test.describe.configure({ mode: 'serial' })
const MEMBER = { name: 'Bruno Martins', email: 'bruno@example.com', password: 'bruno long password' }

test('an admin creates and disables a member; the member is logged out at once', async ({ page, browser }) => {
  await login(page)
  await page.goto('/admin/users')
  await expect(page.getByRole('heading', { name: 'Users' })).toBeVisible()
  await page.getByRole('button', { name: 'New user' }).click()
  await page.locator('#nu-name').fill(MEMBER.name)
  await page.locator('#nu-email').fill(MEMBER.email)
  await page.locator('#nu-pw').fill(MEMBER.password)
  await page.getByRole('button', { name: 'Create', exact: true }).click()
  await expect(page.locator('.toast').first()).toContainText(`User ${MEMBER.name} created.`)

  // the member logs in elsewhere and cannot open the administration
  const other = await browser.newContext()
  const mp = await other.newPage()
  await login(mp, MEMBER.email, MEMBER.password)
  await expect(mp.getByRole('link', { name: 'Administration' })).toHaveCount(0)
  await mp.goto('/admin/users')
  await expect(mp).toHaveURL(/\/channels\/general$/)
  await expect(mp.locator('.toast.err')).toContainText('You are not allowed to open this page.')

  // disable with a themed confirm
  const row = page.getByRole('row', { name: new RegExp(MEMBER.email) })
  await row.getByRole('button', { name: 'Disable' }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Disable' }).click()
  await expect(row).toContainText('Disabled')

  // the member's next request fails: a toast and the login page
  await mp.goto('/settings/profile')
  await expect(mp).toHaveURL(/\/login/)
  await other.close()

  await page.goto('/admin/audit')
  await page.getByLabel('Action').selectOption('user.disabled')
  await expect(page.getByRole('row', { name: /user\.disabled/ })).toHaveCount(1)
})

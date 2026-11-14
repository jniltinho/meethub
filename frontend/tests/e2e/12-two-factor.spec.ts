import { createHmac } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import { login, OWNER } from './helpers'

test.describe.configure({ mode: 'serial' })

// totp is what an authenticator app shows for a base32 secret at a time (RFC 6238: SHA-1, 6 digits, 30 s)
function totp(secret: string, at = Date.now()): string {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = ''
  for (const ch of secret) bits += alphabet.indexOf(ch).toString(2).padStart(5, '0')
  const key = Buffer.from(bits.match(/.{8}/g)!.map((b) => parseInt(b, 2)))
  const msg = Buffer.alloc(8)
  msg.writeBigUInt64BE(BigInt(Math.floor(at / 1000 / 30)))
  const sum = createHmac('sha1', key).update(msg).digest()
  const off = sum[sum.length - 1]! & 0x0f
  return String((sum.readUInt32BE(off) & 0x7fffffff) % 1_000_000).padStart(6, '0')
}

async function password(page: Page) {
  await page.context().clearCookies()
  await page.goto('/login')
  await page.getByLabel('E-mail').fill(OWNER.email)
  await page.getByLabel('Password', { exact: true }).fill(OWNER.password)
  await page.getByRole('button', { name: 'Log in' }).click()
}

let secret = ''
let codes: string[] = []

test('enroll an authenticator app in Settings → Security', async ({ page }) => {
  await login(page)
  await page.goto('/settings/security')
  await expect(page.getByRole('heading', { name: 'Security' })).toBeVisible()
  await page.getByRole('button', { name: 'Add an authenticator app' }).click()
  await expect(page.getByRole('img', { name: 'QR code for the authenticator app' })).toBeVisible()
  secret = (await page.locator('[data-secret]').textContent())!.trim()
  expect(secret).toMatch(/^[A-Z2-7]{32}$/)

  await page.getByLabel('Authentication code').fill(totp(secret) === '000000' ? '111111' : '000000')
  await page.getByRole('button', { name: 'Turn on' }).click()
  await expect(page.locator('.alert-error')).toContainText('That code is not valid.')

  await page.getByLabel('Authentication code').fill(totp(secret))
  await page.getByRole('button', { name: 'Turn on' }).click()
  const list = page.locator('[data-recovery-codes] code')
  await expect(list).toHaveCount(10)
  codes = (await list.allTextContents()).map((c) => c.trim())
  await page.getByRole('button', { name: 'I saved them' }).click()
  await expect(page.getByText('Two-factor authentication is on. 10 recovery codes left.')).toBeVisible()
})

test('log in with the password, then a code; a wrong code says only that', async ({ page }) => {
  await password(page)
  const code = page.getByLabel('Authentication code')
  await expect(code).toBeFocused()
  await expect(page).toHaveURL(/\/login/)
  await code.fill('000000' === totp(secret, Date.now() + 30_000) ? '111111' : '000000')
  await page.getByRole('button', { name: 'Verify' }).click()
  await expect(page.locator('.alert-error')).toHaveText('That code is not valid.')
  await expect(code).toBeFocused()
  // the enrollment used the current step; the app's next code is within the tolerance
  await code.fill(totp(secret, Date.now() + 30_000))
  await page.getByRole('button', { name: 'Verify' }).click()
  await expect(page).toHaveURL(/\/channels\/general$/)
})

test('log in with a recovery code, then turn two-factor off with another one', async ({ page }) => {
  await password(page)
  await page.getByRole('button', { name: 'Use a recovery code' }).click()
  await page.getByLabel('Recovery code').fill(codes[0]!)
  await page.getByRole('button', { name: 'Verify' }).click()
  await expect(page).toHaveURL(/\/channels\/general$/)

  await page.goto('/settings/security')
  await expect(page.getByText('Two-factor authentication is on. 9 recovery codes left.')).toBeVisible()
  await page.getByLabel('Password', { exact: true }).fill(OWNER.password)
  await page.getByLabel('Authentication code or recovery code').fill(codes[1]!)
  await page.getByRole('button', { name: 'Turn off two-factor authentication' }).click()
  await expect(page.getByRole('button', { name: 'Add an authenticator app' })).toBeVisible()

  await page.context().clearCookies()
  await login(page) // the password alone again
})

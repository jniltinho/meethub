import { crc32, deflateSync } from 'node:zlib'
import { expect, type Browser, type Page } from '@playwright/test'

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

// ── chat helpers ─────────────────────────────────────────────────────────────
export const BRUNO = { name: 'Bruno Martins', email: 'bruno.chat@example.com', password: 'a long password!' }
export const CARLA = { name: 'Carla Pereira', email: 'carla.chat@example.com', password: 'a long password!' }

/** api calls the API as the page's user (session cookie + CSRF token from /me) */
export async function api<T = unknown>(page: Page, method: string, path: string, body?: unknown): Promise<T> {
  return page.evaluate(
    async ({ method, path, body }) => {
      const me = await (await fetch('/api/v1/me')).json()
      const r = await fetch('/api/v1' + path, {
        method,
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': me.csrf_token },
        body: method === 'GET' ? undefined : JSON.stringify(body ?? {}),
      })
      const text = await r.text()
      if (!r.ok) throw new Error(`${method} ${path}: ${r.status} ${text}`)
      return text ? JSON.parse(text) : null
    },
    { method, path, body },
  )
}

export async function as(browser: Browser, who: { email: string; password: string }): Promise<Page> {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await login(page, who.email, who.password)
  return page
}

export const composer = (page: Page) => page.locator('.channel-view [data-composer]')
export const timeline = (page: Page) => page.locator('.channel-view .timeline')

export async function send(page: Page, text: string) {
  await composer(page).fill(text)
  await composer(page).press('Enter')
}

/** png encodes a solid w×h picture (no image library in the runner) */
export function png(w: number, h: number): Buffer {
  const chunk = (type: string, data: Buffer) => {
    const body = Buffer.concat([Buffer.from(type), data])
    const len = Buffer.alloc(4)
    len.writeUInt32BE(data.length)
    const crc = Buffer.alloc(4)
    crc.writeUInt32BE(crc32(body))
    return Buffer.concat([len, body, crc])
  }
  const ihdr = Buffer.alloc(13)
  ihdr.writeUInt32BE(w, 0)
  ihdr.writeUInt32BE(h, 4)
  ihdr[8] = 8 // bits per channel
  ihdr[9] = 2 // RGB
  const row = Buffer.concat([Buffer.from([0]), Buffer.alloc(w * 3, 0x66)]) // filter byte, then pixels
  const raw = Buffer.concat(Array.from({ length: h }, () => row))
  return Buffer.concat([
    Buffer.from('\x89PNG\r\n\x1a\n', 'latin1'),
    chunk('IHDR', ihdr),
    chunk('IDAT', deflateSync(raw)),
    chunk('IEND', Buffer.alloc(0)),
  ])
}

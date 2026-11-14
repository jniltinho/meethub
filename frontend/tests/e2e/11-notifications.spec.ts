// Notifications (add-notifications): Settings → Notifications with the browser's permission granted, denied and not
// asked yet; a desktop notification for a mention while another channel is open, none while looking at it; choosing
// it opens the message. The browser's Notification is replaced by a recorder (headless browsers show none). The test
// stack has no SMTP: Settings says e-mail notifications are not available. Runs after the chat suites.
import { expect, test, type Browser, type Page } from '@playwright/test'
import { api, as, BRUNO, login, OWNER } from './helpers'

type Perm = 'granted' | 'denied' | 'default'

async function withNotifications(browser: Browser, perm: Perm): Promise<Page> {
  const ctx = await browser.newContext()
  await ctx.addInitScript((initial: Perm) => {
    const shown: { title: string; body: string; tag: string; onclick: (() => void) | null }[] = []
    ;(window as unknown as { __shown: typeof shown }).__shown = shown
    class Recorder {
      static permission: Perm = initial
      static async requestPermission() {
        Recorder.permission = 'granted'
        return 'granted'
      }
      onclick: (() => void) | null = null
      constructor(title: string, opts: { body?: string; tag?: string } = {}) {
        shown.push(Object.assign(this, { title, body: opts.body ?? '', tag: opts.tag ?? '' }))
      }
      close() {}
    }
    ;(window as unknown as { Notification: typeof Recorder }).Notification = Recorder
  }, perm)
  const page = await ctx.newPage()
  await login(page)
  return page
}

const shown = (page: Page) =>
  page.evaluate(() => (window as unknown as { __shown: { title: string; body: string }[] }).__shown)

test('Settings → Notifications: permission granted, denied and not asked; e-mail not available', async ({
  browser,
}) => {
  for (const [perm, text] of [
    ['granted', 'This browser shows them.'],
    ['denied', 'blocked in this browser'],
  ] as const) {
    const page = await withNotifications(browser, perm)
    await page.goto('/settings/notifications')
    await expect(page.getByRole('heading', { name: 'Notifications' })).toBeVisible()
    await expect(page.locator(`[data-permission="${perm}"]`)).toContainText(text)
    await expect(page.getByText('E-mail notifications are not available on this server.')).toBeVisible()
    await page.context().close()
  }
  const page = await withNotifications(browser, 'default')
  await page.goto('/settings/notifications')
  await page.getByRole('button', { name: 'Allow desktop notifications' }).click()
  await expect(page.locator('[data-permission="granted"]')).toBeVisible()
  // the preference is saved
  await page.getByLabel('All messages of my conversations').check()
  await expect(page.locator('.toast', { hasText: 'Notifications saved.' }).first()).toBeVisible()
  const me = await api<{ preferences: { desktop_notifications: string } }>(page, 'GET', '/me')
  expect(me.preferences.desktop_notifications).toBe('all')
  await page.getByLabel('Direct messages and mentions', { exact: true }).check()
  await expect
    .poll(
      async () =>
        (await api<{ preferences: { desktop_notifications: string } }>(page, 'GET', '/me')).preferences
          .desktop_notifications,
    )
    .toBe('mentions')
  await page.context().close()
})

test('a mention while another channel is open shows a notification that opens it; none while looking', async ({
  browser,
}) => {
  const ana = await withNotifications(browser, 'granted')
  const bruno = await as(browser, BRUNO)
  await ana.goto('/conversations')
  await expect(ana).toHaveURL(/\/channels\//)
  const team = decodeURIComponent(new URL(ana.url()).pathname.split('/')[2]!)
  const teams = await api<{ items: { id: string; name: string }[] }>(ana, 'GET', '/users/me/teams')
  const teamId = teams.items.find((x) => x.name === team)!.id
  const list = await api<{ items: { id: string; name: string }[] }>(ana, 'GET', `/users/me/teams/${teamId}/channels`)
  const projects = list.items.find((c) => c.name === 'projects')!.id
  await api(bruno, 'POST', `/channels/${projects}/members`, {}).catch(() => undefined)
  await ana.goto(`/teams/${team}/channels/general`)
  await expect(ana.locator('.channel-view [data-composer]')).toBeVisible()
  const me = await api<{ user: { username: string } }>(ana, 'GET', '/me')

  const before = (await shown(ana)).length
  const p = await api<{ id: string }>(bruno, 'POST', '/posts', {
    channel_id: projects,
    message: `@${me.user.username} the desktop notification test`,
    pending_post_id: `e2e-notify-${Date.now()}`,
  })
  await expect.poll(async () => (await shown(ana)).length).toBe(before + 1)
  const n = (await shown(ana))[before]!
  expect(n.title).toContain(BRUNO.name)
  expect(n.title).toContain('#Projects')
  expect(n.body).toContain('the desktop notification test')
  await ana.evaluate((i) => (window as unknown as { __shown: { onclick: () => void }[] }).__shown[i]!.onclick(), before)
  await expect(ana).toHaveURL(/\/channels\/projects$/)
  await expect(ana.locator(`.channel-view [data-post-id="${p.id}"]`)).toHaveClass(/highlight/)

  // looking at #projects: no notification
  const now = (await shown(ana)).length
  await api(bruno, 'POST', '/posts', {
    channel_id: projects,
    message: `@${me.user.username} while you look`,
    pending_post_id: `e2e-notify-look-${Date.now()}`,
  })
  await expect(ana.locator('.channel-view .timeline')).toContainText('while you look')
  expect((await shown(ana)).length).toBe(now)
  await ana.context().close()
  await bruno.context().close()
})

test('the unsubscribe page refuses a link that is not valid', async ({ page }) => {
  await page.goto(`/unsubscribe?token=${'0'.repeat(64)}`)
  await page.getByRole('button', { name: 'Turn off these e-mails' }).click()
  await expect(page.getByRole('alert')).toContainText('This link is not valid any more.')
  void OWNER
})

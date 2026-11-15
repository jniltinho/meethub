// Administration → Settings (add-admin-settings: admin-settings spec) against the e2e stack, where the server runs
// under the real applier in its test mode (`settings-applier --supervise`, confirmation window 20 s): a change
// applied and kept, Restore default, one not confirmed and rolled back, a locked key (MEETHUB_OIDC_NAME), a refused
// value, an admin without access, the read-only page without an applier, accessibility and contrast.
import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import { api, as, OWNER } from './helpers'

test.describe.configure({ mode: 'serial' })
test.setTimeout(240_000)

const ADMIN = { email: 'adele.admin@example.com', password: 'a long password!' }
const row = (page: Page, key: string) => page.locator(`[data-setting="${key}"]`)
const input = (page: Page, key: string) => page.locator(`#set-${key.replace('.', '\\.')}`)
// open goes to a sub-tab of the page by its link
async function open(page: Page, tab: string) {
  await page.locator(`[data-settings-tabs] [data-tab="${tab}"]`).click()
  await expect(page.locator(`[data-settings-tabs] [data-tab="${tab}"]`)).toHaveAttribute('aria-current', 'page')
}

async function axe(page: Page) {
  const r = await new AxeBuilder({ page }).include('main').withTags(['wcag2a', 'wcag2aa']).analyze()
  return r.violations
    .filter((v) => v.impact === 'serious' || v.impact === 'critical')
    .map((v) => `${v.id}: ${v.nodes[0]?.target}`)
}

// apply presses "Apply and restart" and accepts the themed dialog, which names what changes
async function apply(page: Page, names: string[]) {
  await page.locator('[data-settings-apply]').click()
  const dialog = page.locator('dialog[open]')
  await expect(dialog).toBeVisible()
  for (const n of names) await expect(dialog).toContainText(n)
  await expect(dialog).toContainText('The server restarts')
  await dialog.getByRole('button', { name: 'Apply and restart' }).click()
  await expect(page.locator('[data-settings-progress]')).toBeVisible()
}

async function bootstrapFiles(page: Page) {
  return (await (await page.request.get('/api/v1/bootstrap')).json()).files as { max_per_post: number }
}

test('the page: groups, sources, a lock, the read-only values; an admin has no access', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  await api(ana, 'POST', '/admin/users', {
    email: ADMIN.email,
    display_name: 'Adele Admin',
    role: 'admin',
    password: ADMIN.password,
  })
  await ana.goto('/admin')
  await ana.locator('a[href="/admin/settings"]').click()
  await expect(ana).toHaveURL(/\/admin\/settings$/)
  await expect(ana.getByRole('heading', { name: 'Settings', level: 1 })).toBeVisible()
  await expect(ana.locator('[data-settings-readonly]')).toHaveCount(0)
  // one sub-tab per group, the first one open; the open tab is in the address
  await expect(ana.locator('[data-settings-tabs] [data-tab="files"]')).toHaveAttribute('aria-current', 'page')
  await expect(row(ana, 'files.max_per_post')).toContainText('Default')
  await expect(row(ana, 'session.idle_timeout')).toHaveCount(0)
  for (const [tab, g] of [
    ['sessions', 'Sessions'],
    ['logging', 'Logging'],
    ['sso', 'Single sign-on'],
  ] as const) {
    await open(ana, tab)
    await expect(ana).toHaveURL(new RegExp(`/admin/settings\\?tab=${tab}$`))
    await expect(ana.getByRole('heading', { name: g, level: 2 })).toBeVisible()
  }
  // set by the e2e stack's environment: locked, with the variable named
  await expect(input(ana, 'oidc.name')).toBeDisabled()
  await expect(row(ana, 'oidc.name').locator('[data-locked]')).toContainText('Set by MEETHUB_OIDC_NAME')
  expect(await axe(ana), 'a group tab').toEqual([])
  // recordings: on or off is a list; the e2e stack turns them on by its environment, so that one is locked
  await open(ana, 'recordings')
  await expect(input(ana, 'recordings.enabled')).toHaveValue('true')
  await expect(input(ana, 'recordings.enabled')).toBeDisabled()
  await expect(row(ana, 'recordings.enabled').locator('[data-locked]')).toContainText(
    'Set by MEETHUB_RECORDINGS_ENABLED',
  )
  await expect(input(ana, 'recordings.mode')).toBeEnabled()
  await expect(row(ana, 'recordings.max_bytes')).toContainText('4 GB')
  expect(await axe(ana), 'the recordings tab').toEqual([])
  // read-only with the reason; a secret only as set
  await open(ana, 'read-only')
  const secret = ana.locator('[data-settings-readonly-list] tr[data-key="oidc.client_secret"]')
  await expect(secret).toContainText('Set')
  await expect(secret).not.toContainText('e2e-throwaway-client-secret')
  await expect(ana.locator('tr[data-key="smtp.host"]')).toContainText('secrets or people')
  await expect(ana.locator('tr[data-key="recordings.retention_days"]')).toContainText('cannot be taken back')
  expect(await axe(ana), 'the read-only tab').toEqual([])
  // the meeting summaries setting is a tab of the same page
  await open(ana, 'summaries')
  await expect(ana.getByRole('heading', { name: 'Meeting summaries' })).toBeVisible()
  await expect(ana.locator('[data-settings-readonly-list]')).toHaveCount(0)

  const adele = await as(browser, ADMIN)
  await adele.goto('/admin')
  await expect(adele.locator('a[href="/admin/settings"]')).toHaveCount(0)
  await adele.goto('/admin/settings')
  await expect(adele).not.toHaveURL(/\/admin\/settings/)
  await expect(api(adele, 'GET', '/admin/settings')).rejects.toThrow(/403/)
})

test('a refused value: named on the page, refused by the server', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  await ana.goto('/admin/settings?tab=sessions')
  await input(ana, 'session.idle_timeout').fill('1s')
  await expect(row(ana, 'session.idle_timeout').locator('.field-error')).toContainText('from 5 minutes to 90 days')
  await expect(ana.locator('[data-settings-apply]')).toBeDisabled()
  await expect(api(ana, 'PUT', '/admin/settings', { changes: { 'session.idle_timeout': '1s' } })).rejects.toThrow(
    /400 .*at least 5m/,
  )
  await expect(api(ana, 'PUT', '/admin/settings', { changes: { 'smtp.host': 'evil.example.com' } })).rejects.toThrow(
    /400 .*setting_not_editable/,
  )
  await expect(api(ana, 'PUT', '/admin/settings', { changes: { 'oidc.name': 'Other' } })).rejects.toThrow(
    /409 .*MEETHUB_OIDC_NAME/,
  )
})

test('a change applied and kept, then its default restored', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  await ana.goto('/admin/settings')
  await input(ana, 'files.max_per_post').fill('12')
  await expect(ana.locator('[data-pending-count]')).toHaveText('1 pending change')
  // the change waits while another tab is open, counted on its own tab
  await open(ana, 'sessions')
  await expect(ana.locator('[data-tab="files"] [data-tab-pending]')).toContainText('1')
  await expect(ana.locator('[data-pending-count]')).toHaveText('1 pending change')
  await open(ana, 'files')
  await expect(input(ana, 'files.max_per_post')).toHaveValue('12')
  await apply(ana, ['Files per message: 12'])
  const keep = ana.locator('[data-settings-keep]')
  await expect(keep).toBeVisible({ timeout: 90_000 })
  await expect(ana.locator('[data-settings-countdown]')).toContainText('s left')
  expect(await axe(ana), 'waiting for the confirmation').toEqual([])
  await keep.click()
  await expect(ana.locator('[data-settings-result][data-outcome="applied"]')).toBeVisible({ timeout: 60_000 })
  expect((await bootstrapFiles(ana)).max_per_post).toBe(12)
  await expect(input(ana, 'files.max_per_post')).toHaveValue('12')
  await expect(row(ana, 'files.max_per_post')).toContainText('From config.toml')
  await expect(row(ana, 'files.max_per_post')).toContainText('Default: 10')

  await row(ana, 'files.max_per_post').locator('[data-restore]').click()
  await expect(input(ana, 'files.max_per_post')).toHaveValue('10')
  await apply(ana, ['Files per message: 10'])
  await expect(keep).toBeVisible({ timeout: 90_000 })
  await keep.click()
  await expect(ana.locator('[data-settings-result][data-outcome="applied"]')).toBeVisible({ timeout: 60_000 })
  expect((await bootstrapFiles(ana)).max_per_post).toBe(10)
  await expect(row(ana, 'files.max_per_post').locator('[data-is-default]')).toBeVisible()
  const audit = await api<{ events: { action: string }[] }>(ana, 'GET', '/admin/audit?action=settings.applied')
  expect(audit.events.length).toBe(2)
})

test('a change nobody confirms is rolled back', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  await ana.goto('/admin/settings?tab=logging')
  await input(ana, 'log.level').selectOption('warn')
  await apply(ana, ['Log level: warn'])
  await expect(ana.locator('[data-settings-keep]')).toBeVisible({ timeout: 90_000 })
  // nobody presses it: after the window the previous file is back and the server restarts with it
  await expect(ana.locator('[data-settings-result][data-outcome="rolled_back"]')).toBeVisible({ timeout: 150_000 })
  await expect(ana.locator('[data-settings-result]')).toContainText('not confirmed')
  await expect(input(ana, 'log.level')).toHaveValue('info')
})

test('without an applier the page is read-only and says why', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  await ana.route('**/api/v1/admin/settings', async (route) => {
    const res = await route.fetch()
    const body = await res.json()
    body.applier = { available: false, reason: 'no_applier' }
    await route.fulfill({ response: res, json: body })
  })
  await ana.goto('/admin/settings')
  await expect(ana.locator('[data-settings-readonly]')).toContainText('configured by environment variables')
  for (const key of ['files.max_size_bytes', 'files.max_per_post']) await expect(input(ana, key)).toBeDisabled()
  await expect(ana.locator('[data-settings-apply]')).toHaveCount(0)
  for (const [tab, key] of [
    ['sessions', 'session.idle_timeout'],
    ['logging', 'log.level'],
  ] as const) {
    await open(ana, tab)
    await expect(input(ana, key)).toBeDisabled()
  }
  await open(ana, 'files')
  expect(await axe(ana), 'read-only page').toEqual([])
})

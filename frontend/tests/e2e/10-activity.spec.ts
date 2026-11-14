// Activity (add-activity-feed): mentions of the user and replies in their threads, the unseen badge on the
// navigation kept live in every tab, cleared by opening Activity, and each item opening its message. Runs after the
// chat suites (Ana, Bruno, Carla and #projects exist).
import { expect, test, type Page } from '@playwright/test'
import { api, as, BRUNO, CARLA, login, OWNER } from './helpers'

test.describe.configure({ mode: 'serial' })

const badge = (page: Page) => page.locator('.section-nav [data-activity-badge]')
let n = 0
const key = () => `e2e-activity-${Date.now()}-${n++}`

async function projects(page: Page): Promise<string> {
  await page.goto('/conversations')
  await expect(page).toHaveURL(/\/channels\//)
  const team = decodeURIComponent(new URL(page.url()).pathname.split('/')[2]!)
  const teams = await api<{ items: { id: string; name: string }[] }>(page, 'GET', '/users/me/teams')
  const id = teams.items.find((x) => x.name === team)!.id
  const list = await api<{ items: { id: string; name: string }[] }>(page, 'GET', `/users/me/teams/${id}/channels`)
  return list.items.find((c) => c.name === 'projects')!.id
}

test('mention and thread replies: a live badge in two tabs, cleared by opening Activity, items open their message', async ({
  browser,
}) => {
  const ana = await as(browser, OWNER)
  const anaPhone = await browser.newContext({ viewport: { width: 390, height: 800 } }).then((c) => c.newPage())
  await login(anaPhone)
  const bruno = await as(browser, BRUNO)
  const carla = await as(browser, CARLA)
  const channel = await projects(ana)
  await api(carla, 'POST', `/channels/${channel}/members`, {}).catch(() => undefined) // already in on a rerun
  await api(bruno, 'POST', `/channels/${channel}/members`, {}).catch(() => undefined)
  // start from nothing unseen
  await ana.goto('/activity')
  await expect(ana.getByRole('heading', { name: 'Activity' })).toBeVisible()
  await ana.goto('/conversations')
  await expect(badge(ana)).toHaveCount(0)

  const me = await api<{ user: { username: string } }>(ana, 'GET', '/me')
  const mention = await api<{ id: string }>(bruno, 'POST', '/posts', {
    channel_id: channel,
    message: `@${me.user.username} can you review the activity page?`,
    pending_post_id: key(),
  })
  await expect(badge(ana)).toHaveText(/^\s*1/) // live, on another channel
  await expect(anaPhone.locator('.bottom-bar [data-activity-badge]')).toHaveText(/^\s*1/)

  const root = await api<{ id: string }>(ana, 'POST', '/posts', {
    channel_id: channel,
    message: 'Thread about the activity release',
    pending_post_id: key(),
  })
  await api(carla, 'POST', '/posts', {
    channel_id: channel,
    root_id: root.id,
    message: 'sounds good',
    pending_post_id: key(),
  })
  const reply = await api<{ id: string }>(bruno, 'POST', '/posts', {
    channel_id: channel,
    root_id: root.id,
    message: 'I can help with the release',
    pending_post_id: key(),
  })
  await expect(badge(ana)).toHaveText(/^\s*3/)
  // @channel creates nothing
  await api(bruno, 'POST', '/posts', { channel_id: channel, message: '@channel deploy at 17h', pending_post_id: key() })

  // opening Activity clears the badge here and in the other tab
  await ana
    .getByRole('link', { name: /Activity/ })
    .first()
    .click()
  await expect(ana).toHaveURL(/\/activity$/)
  const items = ana.locator('[data-activity]') // newest first; earlier suites may have mentioned Ana too
  await expect(items.nth(2)).toContainText('can you review the activity page?')
  await expect(items.nth(1)).toContainText('sounds good')
  await expect(items.first()).toContainText('replied in a thread in #')
  await expect(items.first()).toContainText('I can help with the release')
  await expect(items.first()).toContainText(BRUNO.name)
  await expect(badge(ana)).toHaveCount(0)
  await expect(anaPhone.locator('.bottom-bar [data-activity-badge]')).toHaveCount(0)

  // the mention opens #projects at that message, highlighted
  await ana.locator('[data-activity]', { hasText: 'can you review the activity page?' }).first().click()
  await expect(ana).toHaveURL(/\/channels\/projects$/)
  await expect(ana.locator(`.channel-view [data-post-id="${mention.id}"]`)).toHaveClass(/highlight/)

  // the reply opens its thread with the reply highlighted
  await ana.goto('/activity')
  await ana.locator('[data-activity]', { hasText: 'I can help with the release' }).first().click()
  await expect(ana).toHaveURL(/\/channels\/projects$/)
  await expect(ana.locator('.thread-panel')).toContainText('I can help with the release')
  await expect(ana.locator(`.thread-panel [data-post-id="${reply.id}"]`)).toHaveClass(/highlight/)
  for (const p of [ana, anaPhone, bruno, carla]) await p.context().close()
})

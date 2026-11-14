// The screens of the approved layout (add-chat-mockup-parity): team rail, quick themes, profile pictures, channel
// header and details, pins, search and jumping to a message. Runs after the chat suite (users, #projects and the
// team Ops exist).
import { crc32, deflateSync } from 'node:zlib'
import { expect, test, type Page } from '@playwright/test'
import { api, as, BRUNO, composer, login, OWNER, send, timeline } from './helpers'

test.describe.configure({ mode: 'serial' })

let teamName = ''
const projects = () => `/teams/${teamName}/channels/projects`
const tile = (page: Page, team: string) => page.locator(`nav.rail [data-team="${team}"]`)
const details = (page: Page) => page.locator('.side-panel .channel-details')
const msg = (page: Page, text: string) => timeline(page).locator('.msg', { hasText: text })

/** png encodes a solid w×h picture (no image library in the runner) */
function png(w: number, h: number): Buffer {
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
const PNG = png(64, 48)

test('team rail: a tile per team, a live mention badge, each team reopens where it was left', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  const bruno = await as(browser, BRUNO)
  await bruno.goto('/conversations')
  await expect(bruno).toHaveURL(/\/channels\//)
  teamName = decodeURIComponent(new URL(bruno.url()).pathname.split('/')[2]!)
  await bruno.goto(projects())
  await expect(composer(bruno)).toBeFocused()
  await expect(tile(bruno, teamName)).toHaveAttribute('aria-current', 'true')
  await expect(tile(bruno, 'ops')).toBeVisible()

  const me = await api<{ user: { username: string } }>(bruno, 'GET', '/me')
  const teams = await api<{ items: { id: string; name: string }[] }>(ana, 'GET', '/users/me/teams')
  const ops = teams.items.find((t) => t.name === 'ops')!.id
  const channels = await api<{ items: { id: string; name: string }[] }>(ana, 'GET', `/users/me/teams/${ops}/channels`)
  await api(ana, 'POST', '/posts', {
    channel_id: channels.items.find((c) => c.name === 'general')!.id,
    message: `@${me.user.username} the rail should show this`,
    pending_post_id: 'e2e-rail-1',
  })
  await expect(tile(bruno, 'ops').locator('.chat-badge')).toHaveText('1', { timeout: 2000 })
  await expect(tile(bruno, 'ops')).toHaveAccessibleName('Ops, 1 mentions')

  await tile(bruno, 'ops').click()
  await expect(bruno).toHaveURL(/\/teams\/ops\/channels\/general$/)
  await expect(composer(bruno)).toBeFocused()
  await expect(tile(bruno, 'ops').locator('.chat-badge')).toHaveCount(0)
  // back to the first team: the channel last visited there, not its default one
  await tile(bruno, teamName).click()
  await expect(bruno).toHaveURL(/\/channels\/projects$/)
  await bruno.getByRole('link', { name: 'Browse or create teams' }).click()
  await expect(bruno).toHaveURL(/\/teams$/)
  await ana.context().close()
  await bruno.context().close()
})

test('phone: team tiles in the drawer, sections in the bottom bar', async ({ browser }) => {
  const ctx = await browser.newContext({ viewport: { width: 360, height: 740 } })
  const page = await ctx.newPage()
  await login(page, BRUNO.email, BRUNO.password)
  await page.goto(projects())
  await expect(page.locator('nav.rail')).toBeHidden()
  await expect(page.locator('.bottom-bar').getByRole('link', { name: 'Conversations' })).toBeVisible()
  await page.getByRole('button', { name: 'Open menu' }).click()
  const drawerTile = page.locator('.sidebar .drawer-teams [data-team="ops"]')
  await expect(drawerTile).toBeVisible()
  await drawerTile.click()
  await expect(page).toHaveURL(/\/teams\/ops\/channels\/general$/)
  await expect(page.locator('.sidebar')).not.toBeInViewport()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await ctx.close()
})

test('quick themes: one click changes and saves the theme; the link opens the full picker', async ({ page }) => {
  await login(page)
  await page.goto(projects())
  await page.getByRole('button', { name: 'Quick themes' }).click()
  await expect(page.locator('.quick-themes-pop')).toBeInViewport({ ratio: 1 }) // whole, not cut by the window's edge
  await page.locator('[data-quick-theme="meethub-light"]').click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'meethub-light')
  await expect
    .poll(async () => (await api<{ preferences: { theme: string } }>(page, 'GET', '/me')).preferences.theme)
    .toBe('meethub-light')
  await page.getByRole('button', { name: 'Quick themes' }).click()
  await page.locator('[data-quick-theme="meethub-dark"]').click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'meethub-dark')
  await page.getByRole('button', { name: 'Quick themes' }).click()
  await page.getByRole('link', { name: 'All themes' }).click()
  await expect(page).toHaveURL(/\/settings\/appearance$/)
})

test('emoji picker: the whole set by category, and a search by description', async ({ page }) => {
  await login(page)
  await page.goto(projects())
  await page.getByRole('button', { name: 'Insert emoji' }).click()
  const picker = page.getByRole('dialog', { name: 'Choose an emoji' })
  const grid = picker.locator('.emoji-grid .emoji-btn')
  await expect(grid).toHaveCount(24) // frequently used
  await picker.getByRole('tab', { name: 'Animals & Nature' }).click()
  await expect(picker.getByRole('tab', { name: 'Animals & Nature' })).toHaveAttribute('aria-selected', 'true')
  expect(await grid.count()).toBeGreaterThan(100)
  await picker.getByRole('tab', { name: 'Flags' }).click()
  expect(await grid.count()).toBeGreaterThan(200)
  await picker.getByRole('searchbox', { name: 'Search emoji' }).fill('face with tears of joy') // a description, not a name
  await expect(grid).toHaveCount(1)
  await grid.first().click()
  await expect(composer(page)).toHaveValue(':joy: ')
})

test('profile picture: a non-image is refused, a new picture reaches the other browser', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  const bruno = await as(browser, BRUNO)
  const me = await api<{ user: { id: string } }>(ana, 'GET', '/me')
  await bruno.goto(projects())
  await expect(msg(bruno, 'Release checklist?')).toBeVisible()
  const picture = bruno.locator(`.timeline img[src*="/users/${me.user.id}/image"]`)
  await expect(picture).toHaveCount(0)

  await ana.goto('/settings/profile')
  const input = ana.locator('[data-picture-input]')
  await input.setInputFiles({ name: 'notes.pdf', mimeType: 'application/pdf', buffer: Buffer.from('%PDF-1.4') })
  await expect(ana.getByRole('alert')).toContainText('not a PNG, JPEG or GIF image')
  await expect(ana.getByRole('button', { name: 'Save picture' })).toHaveCount(0)
  await input.setInputFiles({ name: 'me.png', mimeType: 'image/png', buffer: PNG })
  await ana.getByRole('button', { name: 'Save picture' }).click()
  await expect(ana.locator(`.sidebar-foot img[src*="/users/${me.user.id}/image"]`)).toBeVisible()
  await expect(picture.first()).toBeVisible({ timeout: 2000 }) // user_updated reached Bruno
  expect(await picture.first().evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(256)

  await ana.getByRole('button', { name: 'Remove picture' }).click()
  await expect(picture).toHaveCount(0, { timeout: 2000 })
  await ana.context().close()
  await bruno.context().close()
})

test('header and sidebar: star, favorites, collapsed sections, the menu of a member and of an administrator', async ({
  browser,
}) => {
  const ana = await as(browser, OWNER)
  const bruno = await as(browser, BRUNO)
  await bruno.goto(projects())
  await bruno.getByRole('button', { name: 'Add to favorites' }).click()
  await expect(bruno.getByRole('button', { name: 'Remove from favorites' })).toHaveAttribute('aria-pressed', 'true')
  await expect(bruno.locator('a.chat-item[data-favorite]', { hasText: 'Projects' })).toBeVisible()
  await expect(bruno.locator('a.chat-item:not([data-favorite])', { hasText: 'Projects' })).toHaveCount(0)

  const section = bruno.locator('[data-section="channels"]')
  await section.click()
  await expect(section).toHaveAttribute('aria-expanded', 'false')
  await bruno.reload()
  await expect(bruno.locator('[data-section="channels"]')).toHaveAttribute('aria-expanded', 'false')
  await expect(bruno.locator('a.chat-item[data-favorite]', { hasText: 'Projects' })).toBeVisible() // favorite kept
  await bruno.locator('[data-section="channels"]').click()
  await bruno.getByRole('button', { name: 'Remove from favorites' }).click()
  await expect(bruno.locator('a.chat-item[data-favorite]')).toHaveCount(0)

  // a member: details and leave; the administrator (Ana created the channel): edit and archive too
  await bruno.getByRole('button', { name: 'More actions' }).click()
  const menu = bruno.getByRole('menu', { name: 'More actions' })
  await expect(menu.getByRole('menuitem')).toHaveText(['Channel details', 'Leave channel'])
  await bruno.keyboard.press('Escape')
  await ana.goto(projects())
  await ana.getByRole('button', { name: 'More actions' }).click()
  await expect(ana.getByRole('menu').getByRole('menuitem')).toHaveText([
    'Channel details',
    'Edit channel',
    'Leave channel',
    'Archive channel',
  ])
  await ana.getByRole('menuitem', { name: 'Archive channel' }).click()
  const confirm = ana.getByRole('alertdialog')
  await expect(confirm).toContainText('Archive Projects?')
  await confirm.getByRole('button', { name: 'Cancel' }).click()
  await expect(composer(ana)).toBeVisible() // not archived
  await ana.context().close()
  await bruno.context().close()
})

test('channel details: the Members button, participants, a thread takes the panel and gives it back', async ({
  page,
}) => {
  await login(page)
  await page.goto(projects())
  await page.getByRole('button', { name: 'Close side panel' }).click()
  await expect(details(page)).toHaveCount(0)
  const members = page.getByRole('button', { name: /Members \(\d+\)/ })
  const n = Number(((await members.getAttribute('aria-label')) ?? (await members.textContent()))!.match(/\d+/)![0])
  await members.click()
  await expect(details(page)).toBeVisible()
  await expect(details(page).locator('[data-participant]')).toHaveCount(n)
  await expect(details(page).getByRole('heading', { name: `Participants (${n})` })).toBeVisible()

  const root = msg(page, 'Release checklist?')
  await root.getByRole('button', { name: /repl/ }).click()
  await expect(page.locator('.thread-panel')).toBeVisible()
  await expect(details(page)).toHaveCount(0)
  await page.locator('.thread-panel').getByRole('button', { name: 'Close thread' }).click()
  await expect(details(page)).toBeVisible()

  // the header's toggle closes the panel also while a thread holds it open
  await root.getByRole('button', { name: /repl/ }).click()
  await expect(page.locator('.thread-panel')).toBeVisible()
  await page.getByRole('button', { name: 'Close side panel' }).click()
  await expect(page.locator('.side-panel')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Open side panel' })).toHaveAttribute('aria-expanded', 'false')
  await page.getByRole('button', { name: 'Open side panel' }).click()
  await expect(details(page)).toBeVisible()
})

test('pins: the banner reaches the other browser, stays dismissed until a newer pin', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  const bruno = await as(browser, BRUNO)
  for (const p of [ana, bruno]) await p.goto(projects())
  await send(ana, 'Deploy checklist for the release')
  const own = msg(ana, 'Deploy checklist for the release')
  await own.hover()
  await own.getByRole('button', { name: 'Pin', exact: true }).click()
  const banner = bruno.locator('.pinned-banner')
  await expect(banner).toContainText('Pinned by Ana Silva', { timeout: 2000 })
  await expect(banner).toContainText('Deploy checklist for the release')
  await expect(msg(bruno, 'Deploy checklist for the release').locator('.msg-pinned')).toBeVisible()
  await expect(details(bruno).getByRole('heading', { name: 'Pinned messages (1)' })).toBeVisible()

  await banner.getByRole('button', { name: 'Dismiss' }).click()
  await expect(banner).toHaveCount(0)
  await bruno.reload()
  await expect(msg(bruno, 'Deploy checklist for the release')).toBeVisible()
  await expect(bruno.locator('.pinned-banner')).toHaveCount(0)

  await send(ana, 'Rollback plan')
  const second = msg(ana, 'Rollback plan')
  await second.hover()
  await second.getByRole('button', { name: 'Pin', exact: true }).click()
  await expect(bruno.locator('.pinned-banner')).toContainText('Rollback plan', { timeout: 2000 })
  // unpin both from the details panel
  for (let i = 0; i < 2; i++) await details(ana).getByRole('button', { name: 'Unpin' }).first().click()
  await expect(details(ana).getByRole('heading', { name: 'Pinned messages (0)' })).toBeVisible()
  await expect(bruno.locator('.pinned-banner')).toHaveCount(0)
  await ana.context().close()
  await bruno.context().close()
})

test('search: highlighted results, the empty state, and a jump to an old message and back to the present', async ({
  browser,
}) => {
  const ana = await as(browser, OWNER)
  const bruno = await as(browser, BRUNO)
  const list = await api<{ items: { id: string; name: string }[] }>(ana, 'GET', '/users/me/teams')
  const team = list.items.find((t) => t.name === teamName)!.id
  const channels = await api<{ items: { id: string; name: string }[] }>(ana, 'GET', `/users/me/teams/${team}/channels`)
  const channel = channels.items.find((c) => c.name === 'projects')!.id
  await api(ana, 'POST', '/posts', {
    channel_id: channel,
    message: 'the zebra-quartz invoice',
    pending_post_id: 'e2e-needle',
  })
  // 32 newer messages by two authors (under the limit of 30 per user in 10 s): more than the 30 of half a window
  for (let i = 0; i < 16; i++) {
    for (const [who, page] of [
      ['a', ana],
      ['b', bruno],
    ] as const) {
      await api(page, 'POST', '/posts', {
        channel_id: channel,
        message: `filler ${who}${i}`,
        pending_post_id: `e2e-f-${who}${i}`,
      })
    }
  }
  await bruno.context().close()

  await ana.goto(`/teams/${teamName}/channels/general`)
  await ana.locator('.channel-actions').getByRole('button', { name: 'Search' }).click()
  const panel = ana.locator('.search-panel')
  const box = panel.getByRole('searchbox', { name: 'Search messages' })
  await expect(box).toBeFocused()
  await box.fill('no-such-word-anywhere')
  await box.press('Enter')
  await expect(panel.getByRole('status')).toContainText('No messages found for “no-such-word-anywhere”')
  await box.fill('a')
  await box.press('Enter')
  await expect(panel.getByRole('alert')).toContainText('at least 2')

  await box.fill('ZEBRA-quartz')
  await box.press('Enter')
  const result = panel.locator('[data-search-result]')
  await expect(result).toHaveCount(1)
  await expect(result).toContainText('#Projects')
  await expect(result.locator('mark')).toHaveText('zebra-quartz')

  await result.click()
  await expect(ana).toHaveURL(/\/channels\/projects$/)
  const needle = msg(ana, 'the zebra-quartz invoice')
  await expect(needle).toBeInViewport()
  await expect(needle).toHaveClass(/highlight/)
  await expect(msg(ana, 'filler b15')).toHaveCount(0) // the window does not reach the present
  const recent = ana.getByRole('button', { name: 'Jump to recent' })
  await expect(recent).toBeVisible()
  // scrolling down pages forwards until the newest message, then the window is attached again
  await expect(async () => {
    await timeline(ana).evaluate((el) => (el.scrollTop = el.scrollHeight))
    await expect(msg(ana, 'filler b15')).toBeVisible({ timeout: 500 })
  }).toPass({ timeout: 15_000 })
  await expect(recent).toHaveCount(0)

  // again (the search stays open in the side panel), this time back through the button
  await panel.locator('[data-search-result]').click()
  await expect(needle).toBeInViewport()
  await ana.getByRole('button', { name: 'Jump to recent' }).click()
  await expect(msg(ana, 'filler b15')).toBeVisible()
  await expect(ana.getByRole('button', { name: 'Jump to recent' })).toHaveCount(0)
  await send(ana, 'live again')
  await expect(msg(ana, 'live again')).toBeVisible()
  await ana.context().close()
})

// Meetings (meetings, meeting-room-ui and chat-ui specs): two browsers with Chromium's fake camera and microphone join
// a channel's LiveKit room, see each other, use the controls and the channel chat, show up in the details panel,
// the sidebar and the Meetings page through LiveKit's real webhooks, and a removed member's call ends.
// Runs after the chat suite (Bruno and Carla exist).
import { expect, test, type Page } from '@playwright/test'
import { api, as, BRUNO, CARLA, collectCspViolations, OWNER, timeline } from './helpers'

const origin = new URL(process.env.E2E_BASE_URL ?? 'http://127.0.0.1:18080').origin
test.use({
  launchOptions: {
    args: [
      '--use-fake-device-for-media-stream',
      '--use-fake-ui-for-media-stream',
      `--unsafely-treat-insecure-origin-as-secure=${origin}`,
    ],
  },
})
test.describe.configure({ mode: 'serial' })

const room = (page: Page) => page.getByRole('dialog', { name: /^Room of #Standup/ })
const tiles = (page: Page) => room(page).locator('.meeting-tile:not(.screen)')
const control = (page: Page, name: string) => room(page).locator(`[data-control="${name}"]`)
const details = (page: Page) => page.locator('.side-panel .channel-details')

let path = ''
let channelId = ''
let brunoId = ''

test('setup: a #standup channel with Ana and Bruno', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  await ana.goto('/conversations')
  await expect(ana).toHaveURL(/\/channels\//)
  const team = decodeURIComponent(new URL(ana.url()).pathname.split('/')[2]!)
  const teams = await api<{ items: { id: string; name: string }[] }>(ana, 'GET', '/users/me/teams')
  const teamId = teams.items.find((t) => t.name === team)!.id
  const ch = await api<{ id: string }>(ana, 'POST', '/channels', {
    team_id: teamId,
    display_name: 'Standup',
    type: 'O',
  })
  channelId = ch.id
  path = `/teams/${team}/channels/standup`
  const bruno = await as(browser, BRUNO)
  await api(bruno, 'POST', `/channels/${channelId}/members`, {})
  brunoId = (await api<{ user: { id: string } }>(bruno, 'GET', '/me')).user.id
  const list = await api<{ enabled: boolean }>(ana, 'GET', '/meetings')
  expect(list.enabled).toBe(true)
})

test('tokens only for members: Carla, outside the channel, is refused', async ({ browser }) => {
  const carla = await as(browser, CARLA)
  await carla.goto('/conversations')
  const status = await carla.evaluate(async (id) => {
    const me = await (await fetch('/api/v1/me')).json()
    const r = await fetch(`/api/v1/channels/${id}/meeting/token`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': me.csrf_token },
      body: '{}',
    })
    return r.status
  }, channelId)
  expect(status).toBe(403)
  // and a forged webhook is refused
  const hook = await carla.evaluate(
    async () => (await fetch('/api/v1/meetings/webhook', { method: 'POST', body: '{}' })).status,
  )
  expect(hook).toBe(401)
})

test('two browsers meet: pre-join, grid, presence everywhere, controls, chat, leave', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  const csp = await collectCspViolations(ana)
  const bruno = await as(browser, BRUNO)
  await ana.goto(path)
  await bruno.goto(path)

  // Ana starts the meeting: the pre-join screen previews her camera
  const start = ana.locator('[data-start-meeting]')
  await expect(start).toHaveText(/Start meeting/)
  await start.click()
  await expect(room(ana)).toBeVisible()
  await expect(room(ana).locator('.meeting-preview video')).toBeVisible()
  await expect(room(ana).getByRole('button', { name: 'Join now' })).toBeFocused()
  await room(ana).getByRole('button', { name: 'Join now' }).click()
  await expect(tiles(ana)).toHaveCount(1)
  await expect(room(ana).locator('[data-local]')).toContainText('Ana Silva (you)')
  await expect(control(ana, 'mic')).toBeFocused()

  // Bruno sees her in the room through LiveKit's webhook: header, details card, participants, sidebar, Meetings
  await expect(bruno.locator('[data-start-meeting]')).toHaveText(/Join meeting \(1\)/, { timeout: 15_000 })
  await bruno.getByRole('button', { name: /Members/ }).click()
  await expect(details(bruno).locator('[data-room-card]')).toContainText('1 in the room')
  await expect(details(bruno).locator('[data-participant]', { hasText: 'Ana Silva' })).toContainText('In the room')
  await expect(bruno.locator('[data-meeting-room]', { hasText: 'Standup' })).toContainText('1 in the room')

  // Bruno joins from the room card: both see two tiles, with Bruno's real video at Ana's
  await details(bruno).locator('[data-join-room]').click()
  await room(bruno).getByRole('button', { name: 'Join now' }).click()
  await expect(tiles(ana)).toHaveCount(2, { timeout: 15_000 })
  await expect(tiles(bruno)).toHaveCount(2, { timeout: 15_000 })
  const brunoAtAna = room(ana).locator(`[data-tile="${brunoId}"] video`)
  await expect
    .poll(() => brunoAtAna.evaluate((v: HTMLVideoElement) => v.videoWidth), { timeout: 15_000 })
    .toBeGreaterThan(0)

  // mute, raise a hand, react: Bruno sees each on Ana's tile
  const anaAtBruno = room(bruno).locator('.meeting-tile:not([data-local])')
  await control(ana, 'mic').click()
  await expect(control(ana, 'mic')).toHaveAttribute('aria-label', 'Turn on microphone')
  await expect(anaAtBruno.locator('.meeting-muted')).toBeVisible()
  await control(ana, 'hand').click()
  await expect(control(ana, 'hand')).toHaveAttribute('aria-pressed', 'true')
  await expect(anaAtBruno.locator('.meeting-hand')).toBeVisible()
  await control(ana, 'reactions').click()
  await room(ana).getByRole('menuitem', { name: 'React with 👍' }).click()
  await expect(anaAtBruno.locator('.meeting-reaction')).toHaveText('👍')

  // the channel's chat beside the call
  if (!(await room(ana).locator('.meeting-panel').isVisible())) await control(ana, 'chat').click()
  await room(ana).getByLabel('Send a message to everyone').fill('Starting the demo')
  await room(ana).getByLabel('Send a message to everyone').press('Enter')
  await expect(room(ana).locator('.meeting-messages')).toContainText('Starting the demo')
  await expect(timeline(bruno).locator('.msg', { hasText: 'Starting the demo' })).toHaveCount(1)

  // the Meetings page of a third tab of Bruno's lists the room with two people
  const page3 = await bruno.context().newPage()
  await page3.goto('/meetings')
  await expect(page3.locator('[data-meeting-row]', { hasText: 'Room of #Standup' })).toContainText('2 in the room')
  await page3.close()

  // keyboard: Tab stays inside the dialog
  await control(ana, 'leave').focus()
  for (let i = 0; i < 30; i++) await ana.keyboard.press('Tab')
  expect(await ana.evaluate(() => !!document.activeElement?.closest('.meeting-room'))).toBe(true)

  // Ana leaves: her call ends, Bruno is alone
  await control(ana, 'leave').click()
  await expect(room(ana)).toHaveCount(0)
  await expect(tiles(bruno)).toHaveCount(1, { timeout: 15_000 })
  expect(await csp()).toEqual([])
})

test('removed from the channel during a meeting: the call ends with a notice', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  const bruno = await as(browser, BRUNO)
  await bruno.goto(path)
  await bruno.locator('[data-start-meeting]').click()
  await room(bruno).getByRole('button', { name: 'Join now' }).click()
  await expect(tiles(bruno)).toHaveCount(1)
  await expect
    .poll(
      async () =>
        (await api<{ participants: unknown[] }>(ana, 'GET', `/channels/${channelId}/meeting`)).participants.length,
      {
        timeout: 15_000,
      },
    )
    .toBe(1)
  await api(ana, 'DELETE', `/channels/${channelId}/members/${brunoId}`)
  await expect(room(bruno)).toHaveCount(0, { timeout: 15_000 })
  await expect(bruno.locator('.toast', { hasText: 'You no longer have access to this channel.' }).first()).toBeVisible()
  await expect
    .poll(
      async () =>
        (await api<{ participants: unknown[] }>(ana, 'GET', `/channels/${channelId}/meeting`)).participants.length,
      {
        timeout: 15_000,
      },
    )
    .toBe(0)
})

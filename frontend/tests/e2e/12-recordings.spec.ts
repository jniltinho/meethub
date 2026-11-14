// Meeting recordings (add-meeting-recordings: meeting-room-ui and chat-ui specs): two browsers with Chromium's fake
// devices in a channel's room; the administrator starts a recording after a themed confirmation, everyone sees the
// mark and is told, the pre-join screen says so, the stop is announced, and the recording arrives in the channel as
// a message whose themed player plays from the keyboard and downloads; partial, expired and failed recordings say
// so. The e2e stack turns recordings on (compose.e2e.yml); the server composes with the image's own ffmpeg.
import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import { api, as, BRUNO, OWNER } from './helpers'

const origin = new URL(process.env.E2E_BASE_URL ?? 'http://127.0.0.1:18080').origin
test.use({
  channel: 'chromium',
  launchOptions: {
    args: [
      '--use-fake-device-for-media-stream',
      '--use-fake-ui-for-media-stream',
      '--autoplay-policy=no-user-gesture-required',
      `--unsafely-treat-insecure-origin-as-secure=${origin}`,
    ],
  },
})
test.describe.configure({ mode: 'serial' })

const room = (page: Page) => page.getByRole('dialog', { name: /^Room of #Retro/ })
const control = (page: Page, name: string) => room(page).locator(`[data-control="${name}"]`)
const toastWith = (page: Page, text: string) => page.locator('.toast', { hasText: text }).first()
const message = (page: Page) => page.locator('.channel-view [data-recording-message]').last()

let path = ''
let channelId = ''

async function join(page: Page) {
  await page.goto(path)
  await page.locator('[data-start-meeting]').click()
  await expect(room(page)).toBeVisible()
  await room(page).getByRole('button', { name: 'Join now' }).click()
  await expect(control(page, 'mic')).toBeVisible({ timeout: 15_000 })
}

async function axe(page: Page, include: string) {
  const r = await new AxeBuilder({ page }).include(include).withTags(['wcag2a', 'wcag2aa']).analyze()
  return r.violations
    .filter((v) => v.impact === 'serious' || v.impact === 'critical')
    .map((v) => `${v.id}: ${v.nodes[0]?.target}`)
}

test('setup: a #retro channel with Ana (its administrator) and Bruno', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  await ana.goto('/conversations')
  await expect(ana).toHaveURL(/\/channels\//)
  const team = decodeURIComponent(new URL(ana.url()).pathname.split('/')[2]!)
  const teams = await api<{ items: { id: string; name: string }[] }>(ana, 'GET', '/users/me/teams')
  const teamId = teams.items.find((t) => t.name === team)!.id
  channelId = (
    await api<{ id: string }>(ana, 'POST', '/channels', { team_id: teamId, display_name: 'Retro', type: 'O' })
  ).id
  path = `/teams/${team}/channels/retro`
  const bruno = await as(browser, BRUNO)
  const brunoId = (await api<{ user: { id: string } }>(bruno, 'GET', '/me')).user.id
  await api(ana, 'POST', `/channels/${channelId}/members`, { user_id: brunoId })
})

test('record a meeting: confirmation, the mark for everyone, the pre-join notice, the stop, the message', async ({
  browser,
}) => {
  test.setTimeout(240_000) // the server composes the recording after the meeting
  const ana = await as(browser, OWNER)
  const bruno = await as(browser, BRUNO)
  await join(ana)
  await join(bruno)

  // only who may record has the control
  await expect(control(ana, 'record')).toBeVisible()
  await expect(control(bruno, 'record')).toHaveCount(0)

  // a themed confirmation that says everyone will be told; cancelling records nothing
  await control(ana, 'record').click()
  const confirm = ana.locator('dialog[open]')
  await expect(confirm).toContainText('Everyone in the meeting will be told that it is being recorded')
  await confirm.getByRole('button', { name: 'Cancel' }).click()
  await expect(room(ana).locator('[data-recording]')).toHaveCount(0)
  await control(ana, 'record').click()
  await ana.locator('dialog[open]').getByRole('button', { name: 'Start recording' }).click()

  // both see "Recording" with its timer; Bruno is told who started it
  for (const page of [ana, bruno]) {
    await expect(room(page).locator('[data-recording]')).toContainText('Recording', { timeout: 15_000 })
  }
  await expect(toastWith(bruno, 'Ana Silva started recording')).toBeVisible()
  await expect(control(ana, 'record')).toHaveAttribute('aria-pressed', 'true')
  expect(await axe(ana, '.meeting-room'), 'recording room').toEqual([])

  // someone opening the pre-join screen is told before joining
  await control(bruno, 'leave').click()
  await expect(room(bruno)).toHaveCount(0)
  await bruno.locator('[data-start-meeting]').click()
  await expect(room(bruno).locator('[data-recorded-notice]')).toContainText('This meeting is being recorded')
  await room(bruno).getByRole('button', { name: 'Join now' }).click()
  await expect(control(bruno, 'mic')).toBeVisible({ timeout: 15_000 })

  await ana.waitForTimeout(4000) // some speech: Chromium's fake microphone beeps
  await control(ana, 'record').click()
  for (const page of [ana, bruno]) {
    await expect(room(page).locator('[data-recording]')).toHaveCount(0, { timeout: 20_000 })
  }
  await expect(toastWith(bruno, 'The recording stopped.')).toBeVisible()
  await control(ana, 'leave').click()
  await control(bruno, 'leave').click()

  // the recording arrives in the channel: a message by Ana with a themed player and a download
  await expect(message(bruno)).toBeVisible({ timeout: 90_000 })
  await expect(message(bruno)).toContainText('Meeting recording')
  await expect(message(bruno)).toContainText(/\d+ s|\d+ min/)
  const download = message(bruno).locator('[data-recording-download]')
  await expect(download).toHaveAttribute('href', /\/api\/v1\/files\//)
  const href = (await download.getAttribute('href'))!
  const res = await bruno.request.get(href)
  expect(res.status()).toBe(200)
  expect(res.headers()['content-type']).toContain('audio/mp4')

  // play and pause from the keyboard
  const play = message(bruno).locator('[data-recording-play]')
  await play.focus()
  await bruno.keyboard.press('Enter')
  await expect(play).toHaveAttribute('aria-label', 'Pause the recording', { timeout: 10_000 })
  await bruno.keyboard.press('Space')
  await expect(play).toHaveAttribute('aria-label', 'Play the recording')
  expect(await axe(bruno, '.channel-view [data-recording-message]'), 'recording message').toEqual([])

  // the channel's details list it, and it opens its message
  const list = await api<{ recordings: { status: string; post_id: string }[] }>(
    ana,
    'GET',
    `/channels/${channelId}/meeting/recordings`,
  )
  expect(list.recordings[0]?.status).toBe('ready')
  await ana.goto(path)
  const entry = ana.locator('.side-panel .channel-details [data-recordings] button').first()
  await expect(entry).toContainText('Ana Silva', { timeout: 15_000 })
  await entry.click()
  await expect(message(ana)).toBeInViewport()
})

test('partial, expired and failed recordings say so', async ({ browser }) => {
  const bruno = await as(browser, BRUNO)
  // the server's own states, shown on fabricated copies of the real message (the retention and a failed
  // composition take days or a broken ffmpeg to reach in a test)
  await bruno.route(`**/api/v1/channels/${channelId}/posts?*`, async (route) => {
    const res = await route.fetch()
    const page = await res.json()
    const real = page.items.find((p: { type: string }) => p.type === 'recording')
    const copy = (id: string, extra: object) => ({ ...real, id, ...extra, create_at: real.create_at + 1 })
    page.items.unshift(
      copy('00000000000000000000PARTIAL', { props: { ...real.props, partial: true } }),
      copy('00000000000000000000EXPIRED', { props: { ...real.props, expired: true }, metadata: {} }),
      copy('000000000000000000000FAILED', { type: 'recording_failed', metadata: {}, props: {} }),
    )
    await route.fulfill({ response: res, json: page })
  })
  await bruno.goto(path)
  const view = bruno.locator('.channel-view')
  await expect(view.locator('[data-recording-failed]')).toContainText(
    'The recording of this meeting could not be saved',
  )
  await expect(view.locator('[data-recording-failed] [data-recording-play]')).toHaveCount(0)
  await expect(view.locator('[data-recording-expired]')).toContainText('This recording expired')
  await expect(view.locator('[data-recording-message]', { hasText: 'Partial recording' }).first()).toBeVisible()
})

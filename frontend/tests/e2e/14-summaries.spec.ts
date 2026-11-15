// Meeting summaries (add-meeting-summaries: chat-ui and meeting-room-ui specs) against the fake AI service of the e2e
// stack (tests/e2e/ai/server.mjs): the setting in Administration with its connection test and the note when turning
// it off; under "manual" the notices before and while recording, "Summarize" for who may record and the summary in
// the recording's thread, labelled as written by AI, its hostile parts inert; under "automatic" the room's mark and
// a summary nobody asked for.
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

const room = (page: Page) => page.getByRole('dialog', { name: /^Room of #Minutes/ })
const control = (page: Page, name: string) => room(page).locator(`[data-control="${name}"]`)
const toastWith = (page: Page, text: string) => page.locator('.toast', { hasText: text }).first()
const message = (page: Page) => page.locator('.channel-view [data-recording-message]').last()

let path = ''

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

async function stats(page: Page) {
  return (await (await page.request.get('http://ai:8090/stats')).json()) as { chats: number; pixel: number }
}

test('setup: a #minutes channel with Ana (its administrator) and Bruno', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  await ana.goto('/conversations')
  await expect(ana).toHaveURL(/\/channels\//)
  const team = decodeURIComponent(new URL(ana.url()).pathname.split('/')[2]!)
  const teams = await api<{ items: { id: string; name: string }[] }>(ana, 'GET', '/users/me/teams')
  const teamId = teams.items.find((t) => t.name === team)!.id
  const channelId = (
    await api<{ id: string }>(ana, 'POST', '/channels', { team_id: teamId, display_name: 'Minutes', type: 'O' })
  ).id
  path = `/teams/${team}/channels/minutes`
  const bruno = await as(browser, BRUNO)
  const brunoId = (await api<{ user: { id: string } }>(bruno, 'GET', '/me')).user.id
  await api(ana, 'POST', `/channels/${channelId}/members`, { user_id: brunoId })
})

test('Administration: the setting, its connection test, and the note when turning it off', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  await ana.goto('/admin/summaries')
  await expect(ana.getByRole('heading', { name: 'Meeting summaries' })).toBeVisible()
  const setting = ana.locator('[data-summaries-setting]')
  await expect(setting.locator('[data-value="off"]')).toBeChecked()
  await ana.locator('[data-summaries-test]').click()
  await expect(ana.locator('[data-summaries-result]')).toContainText('The AI service answered', { timeout: 30_000 })
  expect(await axe(ana, 'main'), 'summaries page').toEqual([])

  await setting.locator('[data-value="manual"]').check()
  await expect(toastWith(ana, 'Meeting summaries setting saved.')).toBeVisible({ timeout: 30_000 })
  // turning it off: a themed confirmation that what was sent stays sent; cancelling keeps it on
  await setting.locator('[data-value="off"]').click()
  const confirm = ana.locator('dialog[open]')
  await expect(confirm).toContainText('cannot be recalled')
  await confirm.getByRole('button', { name: 'Cancel' }).click()
  await expect(setting.locator('[data-value="manual"]')).toBeChecked() // the radios show what is stored
  await ana.reload()
  await expect(ana.locator('[data-summaries-setting] [data-value="manual"]')).toBeChecked()

  // only manage_system: a member is refused by the server
  const bruno = await as(browser, BRUNO)
  await expect(api(bruno, 'PUT', '/admin/summaries', { meeting_summaries: 'automatic' })).rejects.toThrow(/403/)
})

test('manual: told before and during, "Summarize" for who may record, the summary in the thread', async ({
  browser,
}) => {
  test.setTimeout(300_000)
  const ana = await as(browser, OWNER)
  const bruno = await as(browser, BRUNO)
  await join(ana)
  await join(bruno)
  await control(ana, 'record').click()
  const confirm = ana.locator('dialog[open]')
  await expect(confirm).toContainText('An AI summary may be written from this recording.')
  await confirm.getByRole('button', { name: 'Start recording' }).click()
  await expect(room(bruno).locator('[data-recording]')).toContainText('Recording', { timeout: 15_000 })
  await expect(room(bruno).locator('[data-recording]')).not.toContainText('AI summary')
  await expect(toastWith(bruno, 'An AI summary may be written from this recording.')).toBeVisible()
  // the pre-join screen says it too
  await control(bruno, 'leave').click()
  await bruno.locator('[data-start-meeting]').click()
  await expect(room(bruno).locator('[data-recorded-notice]')).toContainText(
    'An AI summary may be written from the recording',
  )
  await room(bruno).getByRole('button', { name: 'Join now' }).click()
  await expect(control(bruno, 'mic')).toBeVisible({ timeout: 15_000 })
  await ana.waitForTimeout(5000) // Chromium's fake microphone beeps: something to transcribe
  await control(ana, 'record').click()
  await expect(room(ana).locator('[data-recording]')).toHaveCount(0, { timeout: 20_000 })
  await control(ana, 'leave').click()
  await control(bruno, 'leave').click()

  // the recording arrives; Ana may ask for its summary, Bruno (no recording role) may not
  await expect(message(ana)).toBeVisible({ timeout: 90_000 })
  const summarize = message(ana).locator('[data-summarize]')
  await expect(summarize).toHaveText('Summarize', { timeout: 15_000 })
  await expect(message(bruno)).toBeVisible({ timeout: 30_000 })
  await expect(message(bruno).locator('[data-summarize]')).toHaveCount(0)
  const before = (await stats(ana)).chats
  await summarize.click()
  await expect(message(ana).locator('[data-summary-link]')).toBeVisible({ timeout: 90_000 })
  expect((await stats(ana)).chats).toBeGreaterThan(before)
  expect(await axe(ana, '.channel-view [data-recording-message]'), 'recording message').toEqual([])

  // in the thread: the interface's label, the summary rendered, its script and image inert, the transcript attached
  await message(ana).locator('[data-summary-link]').click()
  const label = ana.locator('[data-ai-label]').last()
  await expect(label).toContainText('Written by AI from the recording')
  const text = ana.locator('[data-summary]').last()
  await expect(text).toContainText('ship the release on Friday')
  await expect(text).toContainText('<script>')
  await expect(text.locator('img, script')).toHaveCount(0)
  expect(await ana.evaluate(() => (window as unknown as { __pwned?: number }).__pwned)).toBeUndefined()
  await expect(ana.getByText(/^transcript-.*\.txt$/).last()).toBeVisible()
  await expect(ana.locator('[data-summary-requested]').last()).toContainText('Ana Silva asked for an AI summary')
  expect((await stats(ana)).pixel).toBe(0)
})

test('automatic: "Recording · AI summary" in the room and a summary nobody asked for', async ({ browser }) => {
  test.setTimeout(300_000)
  const ana = await as(browser, OWNER)
  const bruno = await as(browser, BRUNO)
  await api(ana, 'PUT', '/admin/summaries', { meeting_summaries: 'automatic' })
  await join(ana)
  await control(ana, 'record').click()
  const confirm = ana.locator('dialog[open]')
  await expect(confirm).toContainText('This meeting will be summarized by AI.')
  await confirm.getByRole('button', { name: 'Start recording' }).click()
  await expect(room(ana).locator('[data-recording]')).toContainText('Recording · AI summary', { timeout: 15_000 })
  expect(await axe(ana, '.meeting-room'), 'recording room').toEqual([])
  await bruno.goto(path)
  await bruno.locator('[data-start-meeting]').click()
  await expect(room(bruno).locator('[data-recorded-notice]')).toContainText(
    'This meeting is being recorded and will be summarized by AI',
  )
  await ana.waitForTimeout(5000)
  await control(ana, 'record').click()
  await expect(room(ana).locator('[data-recording]')).toHaveCount(0, { timeout: 20_000 })
  await control(ana, 'leave').click()
  await ana.goto(path)
  await expect(message(ana).locator('[data-summary-link]')).toBeVisible({ timeout: 150_000 })
  // leave the install as it was found
  await api(ana, 'PUT', '/admin/summaries', { meeting_summaries: 'off' })
})

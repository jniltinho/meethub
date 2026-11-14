// Meetings (meetings, meeting-room-ui and chat-ui specs; design D12, the AAA+ bar): browsers with Chromium's fake
// camera, microphone and screen join a channel's LiveKit room through the real webhooks. Covered: the pre-join screen
// (level meter, pickers, refused and missing devices, an unreachable meeting server), the call (tiles, marks,
// shortcuts, toasts, tooltips, reactions, chat, screen-share stage, leaving), presence in the details panel, sidebar
// and Meetings page, a grid of three, every screen size, contrast measured on the real DOM in the MeetHub themes, axe,
// reduced motion, and a removed member. Runs after the chat suite (Bruno and Carla exist).
import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Browser, type Page } from '@playwright/test'
import { api, as, BRUNO, CARLA, collectCspViolations, login, OWNER, timeline } from './helpers'

const origin = new URL(process.env.E2E_BASE_URL ?? 'http://127.0.0.1:18080').origin
// the new headless Chromium: the old headless shell ignores the insecure-origin flag (no camera on http://meethub)
test.use({
  channel: 'chromium',
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
const toastWith = (page: Page, text: string) => page.locator('.toast', { hasText: text }).first()

let path = ''
let channelId = ''
const ids: Record<string, string> = {}

async function openPrejoin(page: Page) {
  await page.goto(path)
  await page.locator('[data-start-meeting]').click()
  await expect(room(page)).toBeVisible()
}

async function joinCall(page: Page) {
  await openPrejoin(page)
  await room(page).getByRole('button', { name: 'Join now' }).click()
  await expect(control(page, 'mic')).toBeVisible({ timeout: 15_000 })
}

async function leaveCall(page: Page) {
  await control(page, 'leave').click()
  await expect(room(page)).toHaveCount(0)
}

async function axe(page: Page) {
  const r = await new AxeBuilder({ page }).include('.meeting-room').withTags(['wcag2a', 'wcag2aa']).analyze()
  return r.violations
    .filter((v) => v.impact === 'serious' || v.impact === 'critical')
    .map((v) => `${v.id}: ${v.nodes[0]?.target}`)
}

/**
 * contrast measures, in the real DOM, each selector's text (or ::after) color against its effective background (the
 * first opaque one up the tree, translucent layers composited), in the given theme, and returns the failures.
 */
async function contrast(page: Page, theme: string, checks: { sel: string; min: number; pseudo?: string }[]) {
  return page.evaluate(
    ({ theme, checks }) => {
      document.documentElement.setAttribute('data-theme', theme)
      const ctx = document.createElement('canvas').getContext('2d', { willReadFrequently: true })!
      const rgba = (c: string) => {
        ctx.clearRect(0, 0, 1, 1)
        ctx.fillStyle = '#000'
        ctx.fillStyle = c
        ctx.fillRect(0, 0, 1, 1)
        const [r, g, b, a] = ctx.getImageData(0, 0, 1, 1).data
        return [r!, g!, b!, a! / 255]
      }
      const over = (top: number[], under: number[]) =>
        [0, 1, 2].map((i) => top[i]! * top[3]! + under[i]! * (1 - top[3]!)).concat(1)
      const lum = (c: number[]) => {
        const ch = (v: number) => ((v /= 255) <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4)
        return 0.2126 * ch(c[0]!) + 0.7152 * ch(c[1]!) + 0.0722 * ch(c[2]!)
      }
      const fails: string[] = []
      for (const { sel, min, pseudo } of checks) {
        const el = document.querySelector<HTMLElement>(sel)
        if (!el) {
          fails.push(`${sel}: missing`)
          continue
        }
        const own = getComputedStyle(el, pseudo ?? null)
        const layers: number[][] = []
        const first = rgba(own.backgroundColor)
        if (first[3]! > 0) layers.push(first)
        for (let n: HTMLElement | null = pseudo ? el : el.parentElement; n; n = n.parentElement) {
          if (layers.length && layers[layers.length - 1]![3]! >= 1) break
          const c = rgba(getComputedStyle(n).backgroundColor)
          if (c[3]! > 0) layers.push(c)
        }
        let bg = [255, 255, 255, 1]
        for (const l of layers.reverse()) bg = over(l, bg)
        const fg = over(rgba(own.color), bg)
        const [a, b] = [lum(fg), lum(bg)].sort((x, y) => y - x)
        const ratio = (a! + 0.05) / (b! + 0.05)
        if (ratio < min - 0.01) fails.push(`${sel}${pseudo ?? ''}: ${ratio.toFixed(2)} < ${min}`)
      }
      return fails
    },
    { theme, checks },
  )
}

async function sized(browser: Browser, width: number, height: number) {
  const ctx = await browser.newContext({ viewport: { width, height } })
  const page = await ctx.newPage()
  await login(page)
  return page
}

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
  for (const [who, page] of [
    ['ana', ana],
    ['bruno', await as(browser, BRUNO)],
    ['carla', await as(browser, CARLA)],
  ] as const) {
    ids[who] = (await api<{ user: { id: string } }>(page, 'GET', '/me')).user.id
  }
  await api(ana, 'POST', `/channels/${channelId}/members`, { user_id: ids.bruno })
  expect((await api<{ enabled: boolean }>(ana, 'GET', '/meetings')).enabled).toBe(true)
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
  const hook = await carla.evaluate(
    async () => (await fetch('/api/v1/meetings/webhook', { method: 'POST', body: '{}' })).status,
  )
  expect(hook).toBe(401)
})

test('pre-join: refused and missing devices each say so; joining without devices works and says how to unmute', async ({
  browser,
}) => {
  const ana = await as(browser, OWNER)
  await ana.addInitScript(() => {
    navigator.mediaDevices.getUserMedia = () => Promise.reject(new DOMException('no', 'NotAllowedError'))
  })
  await openPrejoin(ana)
  await expect(room(ana).locator('[data-devices="denied"]')).toContainText('blocked the camera and microphone')
  await expect(room(ana).getByRole('button', { name: 'Try again' })).toBeVisible()
  await expect(room(ana).locator('[data-control="prejoin-mic"]')).toBeDisabled()
  await room(ana).getByRole('button', { name: 'Back' }).click()
  await expect(room(ana)).toHaveCount(0)

  const bruno = await as(browser, BRUNO)
  await bruno.addInitScript(() => {
    navigator.mediaDevices.getUserMedia = () => Promise.reject(new DOMException('none', 'NotFoundError'))
  })
  await openPrejoin(bruno)
  await expect(room(bruno).locator('[data-devices="none"]')).toContainText('No camera or microphone was found')
  await room(bruno).getByRole('button', { name: 'Join now' }).click()
  const mine = room(bruno).locator('[data-local]')
  await expect(mine.locator('[aria-label="Microphone off"]')).toBeVisible({ timeout: 15_000 })
  await expect(mine.locator('[aria-label="Camera off"]')).toBeVisible()
  await expect(toastWith(bruno, 'You are muted. Press Ctrl+D to unmute.')).toBeVisible()
  await leaveCall(bruno)
})

test('an unreachable meeting server keeps the pre-join screen with a themed alert; Join now retries', async ({
  browser,
}) => {
  const ana = await as(browser, OWNER)
  await ana.route('**/meeting/token', async (route) => {
    const res = await route.fetch()
    const body = await res.json()
    await route.fulfill({ response: res, json: { ...body, url: 'ws://127.0.0.1:9' } })
  })
  await openPrejoin(ana)
  await room(ana).getByRole('button', { name: 'Join now' }).click()
  await expect(room(ana).locator('[data-join-error]')).toContainText('The meeting server could not be reached', {
    timeout: 30_000,
  })
  await expect(room(ana).locator('.meeting-preview video')).toBeVisible() // the preview is still there
  await ana.unroute('**/meeting/token')
  await room(ana).getByRole('button', { name: 'Join now' }).click()
  await expect(control(ana, 'mic')).toBeVisible({ timeout: 15_000 })
  await leaveCall(ana)
})

test('two browsers meet: pre-join, presence everywhere, shortcuts, marks, toasts, share stage, chat, leave', async ({
  browser,
}) => {
  const ana = await as(browser, OWNER)
  const csp = await collectCspViolations(ana)
  const bruno = await as(browser, BRUNO)
  await ana.goto(path)
  await bruno.goto(path)

  // Ana's pre-join screen: camera preview, a moving microphone level, device pickers
  const start = ana.locator('[data-start-meeting]')
  await expect(start).toHaveText(/Start meeting/)
  await start.click()
  await expect(room(ana).locator('.meeting-preview video')).toBeVisible()
  await expect(room(ana).getByRole('button', { name: 'Join now' })).toBeFocused()
  await expect
    .poll(async () => Number(await room(ana).locator('[data-mic-level]').getAttribute('aria-valuenow')), {
      timeout: 10_000,
    })
    .toBeGreaterThan(0)
  const camPicker = room(ana).getByLabel('Camera', { exact: true })
  await expect.poll(() => camPicker.locator('option').count()).toBeGreaterThan(1) // the default and a camera
  const camId = await camPicker.locator('option').nth(1).getAttribute('value')
  await camPicker.selectOption({ index: 1 })
  // the preview now runs on the chosen camera, and the call will publish that one
  const deviceOf = (v: HTMLVideoElement) =>
    (v.srcObject as MediaStream | null)?.getVideoTracks()[0]?.getSettings().deviceId ?? ''
  await expect.poll(() => room(ana).locator('.meeting-preview video').evaluate(deviceOf)).toBe(camId)
  await room(ana).getByRole('button', { name: 'Join now' }).click()
  await expect(tiles(ana)).toHaveCount(1, { timeout: 15_000 })
  await expect(room(ana).locator('[data-local]')).toContainText('Ana Silva (you)')
  await expect(control(ana, 'mic')).toBeFocused()
  await expect(room(ana).locator('.meeting-alone')).toContainText('You are the only one here')
  await expect.poll(() => room(ana).locator('[data-local] video').evaluate(deviceOf), { timeout: 10_000 }).toBe(camId)
  expect(await axe(ana), 'alone in the call').toEqual([])

  // Bruno sees her through LiveKit's webhook: header, details card, participants, sidebar
  await expect(bruno.locator('[data-start-meeting]')).toHaveText(/Join meeting \(1\)/, { timeout: 15_000 })
  await expect(details(bruno).locator('[data-room-card]')).toContainText('1 in the room')
  await expect(details(bruno).locator('[data-participant]', { hasText: 'Ana Silva' })).toContainText('In the room')
  await expect(bruno.locator('[data-meeting-room]', { hasText: 'Standup' })).toContainText('1 in the room')

  // Bruno's pre-join screen has no serious accessibility violation; he joins from the room card
  await details(bruno).locator('[data-join-room]').click()
  await expect(room(bruno).getByRole('button', { name: 'Join now' })).toBeVisible()
  expect(await axe(bruno), 'pre-join').toEqual([])
  for (const theme of ['meethub-dark', 'meethub-light']) {
    expect(
      await contrast(bruno, theme, [
        { sel: '#meeting-title', min: 7 },
        { sel: '.meeting-prejoin-side p', min: 7 },
        { sel: '.meeting-pickers label', min: 7 },
        { sel: '[data-control="prejoin-mic"]', min: 3 },
      ]),
      `${theme} pre-join`,
    ).toEqual([])
  }
  await room(bruno).getByRole('button', { name: 'Join now' }).click()
  await expect(tiles(ana)).toHaveCount(2, { timeout: 15_000 })
  await expect(tiles(bruno)).toHaveCount(2, { timeout: 15_000 })
  await expect(toastWith(ana, 'Bruno Martins joined the meeting.')).toBeVisible()
  await expect(room(ana).locator('.meeting-alone')).toHaveCount(0)
  const brunoAtAna = room(ana).locator(`[data-tile="${ids.bruno}"]`)
  await expect
    .poll(() => brunoAtAna.locator('video').evaluate((v: HTMLVideoElement) => v.videoWidth), { timeout: 15_000 })
    .toBeGreaterThan(0)

  // Meet's shortcuts: Ctrl+D mutes, Ctrl+E turns the camera off; Bruno sees both marks and Ana's picture
  const anaAtBruno = room(bruno).locator(`[data-tile="${ids.ana}"]`)
  await ana.keyboard.press('Control+d')
  await expect(control(ana, 'mic')).toHaveAttribute('aria-label', 'Turn on microphone')
  await expect(toastWith(ana, 'Microphone off.')).toBeVisible()
  await expect(anaAtBruno.locator('[aria-label="Microphone off"]')).toBeVisible()
  await ana.keyboard.press('Control+e')
  await expect(control(ana, 'cam')).toHaveAttribute('aria-pressed', 'false')
  await expect(anaAtBruno.locator('[aria-label="Camera off"]')).toBeVisible()
  await expect(anaAtBruno.locator('video')).toHaveCount(0)
  // the tooltip names the shortcut
  await control(ana, 'mic').hover()
  expect(await control(ana, 'mic').evaluate((b) => getComputedStyle(b, '::after').content)).toContain('Ctrl+D')

  // a hand, a reaction (focus goes back to the reactions button)
  await control(ana, 'hand').click()
  await expect(anaAtBruno.locator('.meeting-hand')).toBeVisible()
  await control(ana, 'reactions').click()
  await room(ana).getByRole('menuitem', { name: 'React with 👍' }).click()
  await expect(control(ana, 'reactions')).toBeFocused()
  await expect(anaAtBruno.locator('.meeting-reaction')).toHaveText('👍')

  // every text and control of the call, measured on the real DOM: AAA text, 3:1 icons, in both MeetHub themes
  const callChecks = [
    { sel: `.meeting-tile[data-tile="${ids.ana}"] .meeting-name`, min: 7 },
    { sel: `.meeting-tile[data-tile="${ids.ana}"] .meeting-mark`, min: 7 },
    { sel: `.meeting-tile[data-tile="${ids.ana}"] .meeting-hand`, min: 7 },
    { sel: '.meeting-bar-info time', min: 7 },
    { sel: '.meeting-bar-info h1', min: 7 },
    { sel: '.meeting-panel-head', min: 7 },
    { sel: '.meeting-tabs [aria-selected="true"]', min: 7 },
    { sel: '[data-control="cam"]', min: 3 },
    { sel: '[data-control="leave"]', min: 3 },
    { sel: '[data-control="chat"]', min: 3 },
    { sel: '[data-control="mic"]', min: 7, pseudo: '::after' },
  ]
  await control(bruno, 'mic').hover()
  for (const theme of ['meethub-dark', 'meethub-light']) {
    expect(await contrast(bruno, theme, callChecks), `${theme} call`).toEqual([])
  }
  expect(await axe(bruno), 'call').toEqual([])

  // the channel's chat beside the call
  await room(ana).getByLabel('Send a message to everyone').fill('Starting the demo')
  await room(ana).getByLabel('Send a message to everyone').press('Enter')
  await expect(room(ana).locator('.meeting-messages')).toContainText('Starting the demo')
  await expect(timeline(bruno).locator('.msg', { hasText: 'Starting the demo' })).toHaveCount(1)
  await expect(room(bruno).locator('.meeting-messages')).toContainText('Starting the demo')
  for (const theme of ['meethub-dark', 'meethub-light']) {
    expect(
      await contrast(bruno, theme, [
        { sel: '.meeting-message-text', min: 7 },
        { sel: '.meeting-message-head time', min: 7 },
      ]),
      `${theme} chat`,
    ).toEqual([])
  }

  // the Meetings page lists the room with two people
  const page3 = await bruno.context().newPage()
  await page3.goto('/meetings')
  await expect(page3.locator('[data-meeting-row]', { hasText: 'Room of #Standup' })).toContainText('2 in the room')
  await page3.close()

  // Ana shares her screen: it takes Bruno's stage, the people go to the strip
  await control(ana, 'share').click()
  await expect(control(ana, 'share')).toHaveAttribute('aria-pressed', 'true', { timeout: 10_000 })
  await expect(room(bruno).locator('.meeting-share .meeting-tile.screen')).toBeVisible({ timeout: 15_000 })
  await expect(room(bruno).locator('.meeting-strip .meeting-tile')).toHaveCount(2)

  // keyboard: Tab stays inside the dialog
  await control(ana, 'leave').focus()
  for (let i = 0; i < 30; i++) await ana.keyboard.press('Tab')
  expect(await ana.evaluate(() => !!document.activeElement?.closest('.meeting-room'))).toBe(true)

  // leaving while presenting asks first; Bruno is told and is alone again
  await control(ana, 'leave').click()
  const ask = ana.getByRole('alertdialog', { name: 'Confirmation' })
  await expect(ask).toContainText('Stop presenting and leave the meeting?')
  await expect(ask).toBeVisible()
  await ask.getByRole('button', { name: 'Leave' }).click()
  await expect(room(ana)).toHaveCount(0)
  await expect(tiles(bruno)).toHaveCount(1, { timeout: 15_000 })
  await expect(toastWith(bruno, 'Ana Silva left the meeting.')).toBeVisible()
  await expect(room(bruno).locator('.meeting-share')).toHaveCount(0)
  await leaveCall(bruno)
  expect(await csp()).toEqual([])
})

test('a grid of three: two rows, the last one centered, every tile the same size', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  await api(ana, 'POST', `/channels/${channelId}/members`, { user_id: ids.carla })
  const others = [await as(browser, BRUNO), await as(browser, CARLA)]
  for (const p of [ana, ...others]) await joinCall(p)
  await expect(tiles(ana)).toHaveCount(3, { timeout: 15_000 })
  const boxes = await tiles(ana).evaluateAll((els) =>
    els.map((e) => {
      const r = e.getBoundingClientRect()
      return { x: r.x + r.width / 2, y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height) }
    }),
  )
  const grid = await room(ana)
    .locator('.meeting-grid')
    .evaluate((e) => {
      const r = e.getBoundingClientRect()
      return r.x + r.width / 2
    })
  expect(new Set(boxes.map((b) => b.w)).size, 'same width').toBe(1)
  expect(new Set(boxes.map((b) => b.h)).size, 'same height').toBe(1)
  expect(boxes[0]!.y).toBe(boxes[1]!.y)
  expect(boxes[2]!.y).toBeGreaterThan(boxes[0]!.y)
  expect(Math.abs(boxes[2]!.x - grid)).toBeLessThan(2) // the last row is centered
  for (const p of [ana, ...others]) await leaveCall(p)
})

test('Full HD, QHD, 4K and a phone: the pre-join screen and the call fit without overlap or page scroll', async ({
  browser,
}) => {
  for (const [width, height] of [
    [1920, 1080],
    [2560, 1440],
    [3840, 2160],
    [375, 812],
  ] as const) {
    const page = await sized(browser, width, height)
    await openPrejoin(page)
    await expect(room(page).getByRole('button', { name: 'Join now' })).toBeInViewport({ ratio: 1 })
    await room(page).getByRole('button', { name: 'Join now' }).click()
    await expect(control(page, 'mic')).toBeVisible({ timeout: 15_000 })
    if (width < 768) {
      await control(page, 'more').click()
      await room(page).getByRole('menuitem', { name: 'Chat' }).click()
    }
    await expect(room(page).locator('.meeting-panel')).toBeVisible()
    const geo = await page.evaluate(() => {
      type Box = { l: number; t: number; r: number; b: number }
      const box = (el: Element | null): Box => {
        const r = el!.getBoundingClientRect()
        return { l: r.left, t: r.top, r: r.right, b: r.bottom }
      }
      const inside = (b: Box) =>
        b.l >= -0.5 && b.t >= -0.5 && b.r <= window.innerWidth + 0.5 && b.b <= window.innerHeight + 0.5
      const meets = (a: Box, b: Box) => a.l < b.r - 0.5 && b.l < a.r - 0.5 && a.t < b.b - 0.5 && b.t < a.b - 0.5
      const boxes = [...document.querySelectorAll<HTMLElement>('.meeting-bar .meeting-ctl')]
        .filter((e) => e.offsetParent)
        .map(box)
      const overlaps = boxes.flatMap((a, i) => boxes.slice(i + 1).filter((b) => meets(a, b)))
      const phone = window.innerWidth < 768
      const q = (s: string) => box(document.querySelector(s))
      return {
        room: inside(q('.meeting-room')),
        bar: inside(q('.meeting-bar')),
        controls: boxes.every(inside),
        overlaps: overlaps.length,
        panel: inside(q('.meeting-panel')),
        // on wide screens the panel sits beside the grid; on a phone it covers it on purpose
        gridAndPanel: phone || !meets(q('.meeting-grid'), q('.meeting-panel')),
        stageAboveBar: q('.meeting-stage').b <= q('.meeting-bar').t + 0.5,
        scrolled: document.documentElement.scrollTop + document.body.scrollTop,
      }
    })
    expect(geo, `${width}x${height}`).toEqual({
      room: true,
      bar: true,
      controls: true,
      overlaps: 0,
      panel: true,
      gridAndPanel: true,
      stageAboveBar: true,
      scrolled: 0,
    })
    await leaveCall(page)
    await page.context().close()
  }
})

test('reduced motion: reactions appear without animation', async ({ browser }) => {
  const page = await sized(browser, 1280, 800)
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await joinCall(page)
  await control(page, 'reactions').click()
  await room(page).getByRole('menuitem', { name: 'React with 🎉' }).click()
  const reaction = room(page).locator('.meeting-reaction')
  await expect(reaction).toBeVisible()
  expect(await reaction.evaluate((e) => getComputedStyle(e).animationName)).toBe('none')
  await leaveCall(page)
  await page.context().close()
})

test('removed from the channel during a meeting: the call ends with a notice', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  const bruno = await as(browser, BRUNO)
  await joinCall(bruno)
  const count = async () =>
    (await api<{ participants: unknown[] }>(ana, 'GET', `/channels/${channelId}/meeting`)).participants.length
  await expect.poll(count, { timeout: 15_000 }).toBe(1)
  await api(ana, 'DELETE', `/channels/${channelId}/members/${ids.bruno}`)
  await expect(room(bruno)).toHaveCount(0, { timeout: 15_000 })
  await expect(toastWith(bruno, 'You no longer have access to this channel.')).toBeVisible()
  await expect.poll(count, { timeout: 15_000 }).toBe(0)
})

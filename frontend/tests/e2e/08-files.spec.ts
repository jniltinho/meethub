// File attachments (add-file-uploads): attaching with the button, by dropping and by pasting, files inside
// messages, the channel's files in the side panel and the Files page. Runs after the chat suites (users and
// #projects exist).
import { readFile } from 'node:fs/promises'
import { expect, test, type Page } from '@playwright/test'
import { api, as, BRUNO, collectCspViolations, composer, OWNER, png, timeline } from './helpers'

test.describe.configure({ mode: 'serial' })

let teamName = ''
const projects = () => `/teams/${teamName}/channels/projects`
const msg = (page: Page, text: string) => timeline(page).locator('.msg', { hasText: text })
const tray = (page: Page) => page.locator('.channel-view .composer-files [data-upload]')
const picker = (page: Page) => page.locator('.channel-view .composer input[type=file]')
const PNG = png(640, 480)

async function open(page: Page) {
  if (!teamName) {
    await page.goto('/conversations')
    await expect(page).toHaveURL(/\/channels\//)
    teamName = decodeURIComponent(new URL(page.url()).pathname.split('/')[2]!)
  }
  await page.goto(projects())
  await expect(composer(page)).toBeFocused()
}

test('attach with the button: progress tray, remove, send; the other browser gets the files', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  const csp = await collectCspViolations(ana)
  const bruno = await as(browser, BRUNO)
  await open(ana)
  await open(bruno)

  await picker(ana).setInputFiles([
    { name: 'release-plan.txt', mimeType: 'text/plain', buffer: Buffer.from('ship it on friday') },
    { name: 'discard.txt', mimeType: 'text/plain', buffer: Buffer.from('never sent') },
    { name: 'diagram.png', mimeType: 'image/png', buffer: PNG },
  ])
  await expect(tray(ana)).toHaveCount(3)
  await expect(ana.locator('.composer-files [data-upload].ready')).toHaveCount(3)
  await expect(tray(ana).nth(2).locator('img.upload-thumb')).toBeVisible() // an image previews in the tray
  await ana.getByRole('button', { name: 'Remove discard.txt' }).click()
  await expect(tray(ana)).toHaveCount(2)

  await composer(ana).fill('Plan and diagram attached')
  await composer(ana).press('Enter')
  await expect(tray(ana)).toHaveCount(0)
  for (const page of [ana, bruno]) {
    const m = msg(page, 'Plan and diagram attached')
    await expect(m.locator('.file-card', { hasText: 'release-plan.txt' })).toBeVisible({ timeout: 1000 })
    await expect(m.locator('.file-thumb img')).toBeVisible({ timeout: 1000 })
    await expect(m).not.toContainText('discard.txt')
  }
  // the server made the thumbnail (640 × 480 → 240 px high)
  const thumb = msg(bruno, 'Plan and diagram attached').locator('.file-thumb img')
  await expect.poll(() => thumb.evaluate((img: HTMLImageElement) => img.naturalHeight)).toBe(240)

  // a document downloads under its name, with its bytes
  const [download] = await Promise.all([
    bruno.waitForEvent('download'),
    msg(bruno, 'Plan and diagram attached').locator('.file-card').click(),
  ])
  expect(download.suggestedFilename()).toBe('release-plan.txt')
  expect((await readFile(await download.path())).toString()).toBe('ship it on friday')
  expect(await csp()).toEqual([])
})

test('files alone make a message; the limits answer with themed messages', async ({ page }) => {
  await page.route('**/api/v1/bootstrap', async (route) => {
    const res = await route.fetch()
    await route.fulfill({
      response: res,
      json: { ...(await res.json()), files: { max_size_bytes: 32, max_per_post: 2 } },
    })
  })
  await as2(page)
  await open(page)
  await picker(page).setInputFiles({ name: 'huge.bin', mimeType: 'application/octet-stream', buffer: Buffer.alloc(64) })
  await expect(page.locator('.toast', { hasText: 'huge.bin is larger than 32 B.' })).toBeVisible()
  await expect(tray(page)).toHaveCount(0)
  const small = (name: string) => ({ name, mimeType: 'text/plain', buffer: Buffer.from(name) })
  await picker(page).setInputFiles([small('one.txt'), small('two.txt'), small('three.txt')])
  await expect(page.locator('.toast', { hasText: 'at most 2 files' })).toBeVisible()
  await expect(tray(page)).toHaveCount(2)

  await expect(page.getByRole('button', { name: 'Send' })).toBeEnabled() // no text, two files
  await page.getByRole('button', { name: 'Send' }).click()
  const last = timeline(page).locator('.msg').last()
  await expect(last.locator('.file-card')).toHaveCount(2)
  await expect(last.locator('.msg-text')).toHaveCount(0)

  // Enter while a file uploads: the message leaves with the text it had then, the next draft stays
  let release = () => {}
  const held = new Promise<void>((resolve) => (release = resolve))
  await page.route('**/api/v1/files', async (route) => {
    await held
    await route.continue()
  })
  await picker(page).setInputFiles(small('late.txt'))
  await composer(page).fill('Sent with its upload')
  await composer(page).press('Enter')
  await expect(composer(page)).toHaveValue('')
  await composer(page).fill('the next draft')
  await expect(msg(page, 'Sent with its upload')).toHaveCount(0) // still waiting for the upload
  release()
  await expect(msg(page, 'Sent with its upload').locator('.file-card', { hasText: 'late.txt' })).toBeVisible()
  await expect(composer(page)).toHaveValue('the next draft')
  await expect(tray(page)).toHaveCount(0)
})

/** logs the test's own page in as the owner */
async function as2(page: Page) {
  await page.goto('/login')
  await page.getByLabel('E-mail').fill(OWNER.email)
  await page.getByLabel('Password', { exact: true }).fill(OWNER.password)
  await page.getByRole('button', { name: 'Log in' }).click()
  await expect(page).not.toHaveURL(/\/login/)
}

test('dropping a file on the conversation and pasting an image attach them', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  await open(ana)
  const dropped = await ana.evaluateHandle(() => {
    const dt = new DataTransfer()
    dt.items.add(new File(['dropped on the timeline'], 'dropped.txt', { type: 'text/plain' }))
    return dt
  })
  await ana.dispatchEvent('.channel-view', 'dragenter', { dataTransfer: dropped })
  await expect(ana.locator('.drop-overlay')).toContainText('Drop files to attach them')
  await ana.dispatchEvent('.channel-view', 'drop', { dataTransfer: dropped })
  await expect(ana.locator('.drop-overlay')).toHaveCount(0)
  await expect(tray(ana)).toContainText('dropped.txt')

  await composer(ana).focus()
  await ana.evaluate(
    (bytes) => {
      const dt = new DataTransfer()
      dt.items.add(new File([new Uint8Array(bytes)], 'pasted.png', { type: 'image/png' }))
      document
        .querySelector('.channel-view [data-composer]')!
        .dispatchEvent(new ClipboardEvent('paste', { clipboardData: dt, bubbles: true, cancelable: true }))
    },
    [...PNG],
  )
  await expect(tray(ana)).toHaveCount(2)
  await expect(ana.locator('.composer-files [data-upload].ready')).toHaveCount(2)
  await composer(ana).fill('Dropped and pasted')
  await composer(ana).press('Enter')
  const m = msg(ana, 'Dropped and pasted')
  await expect(m.locator('.file-card', { hasText: 'dropped.txt' })).toBeVisible()
  await expect(m.locator('.file-thumb')).toBeVisible()
})

test('a thumbnail opens enlarged and Escape returns the focus; a deleted message shows no files', async ({
  browser,
}) => {
  const ana = await as(browser, OWNER)
  await open(ana)
  const m = msg(ana, 'Dropped and pasted')
  const thumb = m.locator('.file-thumb')
  await thumb.click()
  const dialog = ana.locator('dialog.image-modal')
  await expect(dialog).toBeVisible()
  await expect(dialog).toContainText('pasted.png')
  const big = dialog.locator('img')
  await expect.poll(() => big.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(640)
  await expect(dialog.getByRole('link', { name: 'Download' })).toHaveAttribute('download', 'pasted.png')
  await ana.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(thumb).toBeFocused()

  const id = await m.getAttribute('data-post-id')
  await api(ana, 'DELETE', `/posts/${id}`)
  const gone = timeline(ana).locator(`.msg[data-post-id="${id}"]`)
  await expect(gone).toContainText('(message deleted)')
  await expect(gone.locator('.msg-files')).toHaveCount(0)
})

test('the Files action lists the channel files in the side panel and one opens its message', async ({ browser }) => {
  const bruno = await as(browser, BRUNO)
  await open(bruno)
  // the details panel shows the recent ones
  await expect(bruno.locator('.channel-details .details-files [data-file]').first()).toBeVisible()
  await bruno.locator('.channel-head').getByRole('button', { name: 'Files' }).click()
  const panel = bruno.locator('.side-panel .files-panel')
  await expect(panel.locator('[data-file]', { hasText: 'release-plan.txt' })).toBeVisible()
  await expect(panel.locator('[data-file]', { hasText: 'diagram.png' })).toBeVisible()
  await expect(panel).not.toContainText('pasted.png') // its message was deleted
  await panel
    .locator('[data-file]', { hasText: 'release-plan.txt' })
    .getByRole('button', { name: 'Go to message' })
    .first()
    .click()
  await expect(timeline(bruno).locator('.msg.highlight')).toContainText('Plan and diagram attached')
  await bruno.getByRole('button', { name: 'Close files' }).click()
  await expect(bruno.locator('.side-panel .channel-details')).toBeVisible()
})

test('the Files page: every file of my conversations, filtered by name, back to its message', async ({ browser }) => {
  const bruno = await as(browser, BRUNO)
  await bruno.goto('/files')
  await expect(bruno.getByRole('heading', { name: 'Files' })).toBeVisible()
  await expect(bruno.getByText('arrives in a later version')).toHaveCount(0)
  const rows = bruno.locator('.files-page [data-file]')
  await expect(rows.filter({ hasText: 'diagram.png' })).toBeVisible()
  await expect(rows.filter({ hasText: 'release-plan.txt' })).toContainText('#Projects')

  await bruno.getByRole('searchbox', { name: 'Filter by name' }).fill('RELEASE')
  await expect(rows).toHaveCount(1)
  await expect(rows.first()).toContainText('release-plan.txt')
  await bruno.getByRole('searchbox', { name: 'Filter by name' }).fill('no-such-file')
  await expect(bruno.getByText('No file matches that name.')).toBeVisible()

  await bruno.getByRole('searchbox', { name: 'Filter by name' }).fill('release')
  await rows.first().getByRole('button', { name: 'Go to message' }).first().click()
  await expect(bruno).toHaveURL(/\/channels\/projects$/)
  const landed = timeline(bruno).locator('.msg.highlight')
  await expect(landed).toContainText('Plan and diagram attached')

  // the open lists follow the channel: a deleted message's files leave them without a reload
  const id = await landed.getAttribute('data-post-id')
  await bruno.locator('.channel-head').getByRole('button', { name: 'Files' }).click()
  const listed = bruno.locator('.side-panel .files-panel [data-file]', { hasText: 'release-plan.txt' })
  await expect(listed).toBeVisible()
  const ana = await as(browser, OWNER)
  await api(ana, 'DELETE', `/posts/${id}`)
  await expect(listed).toHaveCount(0)
})

// Chat UI (chat-ui spec): sidebar, routes, channel view, composer, threads, actions, live updates between two
// browsers, recovery, safe rendering, keyboard and accessibility. Runs after the setup test (owner = Ana).
import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Browser, type Page } from '@playwright/test'
import { collectCspViolations, login, OWNER } from './helpers'

test.describe.configure({ mode: 'serial' })

const BRUNO = { name: 'Bruno Martins', email: 'bruno.chat@example.com', password: 'a long password!' }
const CARLA = { name: 'Carla Pereira', email: 'carla.chat@example.com', password: 'a long password!' }

/** api calls the API as the page's user (session cookie + CSRF token from /me) */
async function api<T = unknown>(page: Page, method: string, path: string, body?: unknown): Promise<T> {
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

async function as(browser: Browser, who: { email: string; password: string }): Promise<Page> {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await login(page, who.email, who.password)
  return page
}

const composer = (page: Page) => page.locator('.channel-view [data-composer]')
const timeline = (page: Page) => page.locator('.channel-view .timeline')

async function send(page: Page, text: string) {
  await composer(page).fill(text)
  await composer(page).press('Enter')
}

let teamName = ''

test('setup: two more users and a #projects channel', async ({ page }) => {
  await login(page)
  for (const u of [BRUNO, CARLA]) {
    await api(page, 'POST', '/admin/users', {
      email: u.email,
      display_name: u.name,
      role: 'member',
      password: u.password,
    }).catch(
      () => undefined, // already there on a rerun
    )
  }
  await page.goto('/conversations')
  await expect(page).toHaveURL(/\/channels\/general$/)
  teamName = decodeURIComponent(new URL(page.url()).pathname.split('/')[2]!)
})

test('sidebar, create a channel, deep link survives a reload', async ({ page }) => {
  const csp = await collectCspViolations(page)
  await login(page)
  await page.goto('/conversations')
  await expect(page.locator('[data-composer]')).toBeFocused()
  await page.getByRole('button', { name: 'Create a channel' }).click()
  const dlg = page.getByRole('dialog', { name: 'Create a channel' })
  await dlg.getByLabel('Name').fill('Projects')
  await dlg.getByRole('button', { name: 'Create' }).click()
  await expect(page).toHaveURL(/\/channels\/projects$/)
  await expect(page.getByRole('heading', { level: 1 })).toContainText('Projects')
  // screen readers hear the channel name; focus is in its composer
  await expect(page.locator('[aria-live="polite"]').last()).toHaveText('Projects, channel')
  await expect(page.locator('[data-composer]')).toBeFocused()
  await expect(page.getByRole('link', { name: 'Projects' })).toHaveAttribute('aria-current', 'page')
  await page.reload()
  await expect(page.getByRole('heading', { level: 1 })).toContainText('Projects')
  await expect(page.getByRole('link', { name: 'Conversations' })).toHaveClass(/on/)
  expect(await csp()).toEqual([])
})

test('two browsers: send, edit, react and delete reach the other within a second', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  const bruno = await as(browser, BRUNO)
  await bruno.goto(`/teams/${teamName}/channels/projects`)
  await bruno.getByRole('button', { name: 'Join channel' }).click()
  await ana.goto(`/teams/${teamName}/channels/projects`)
  await expect(composer(bruno)).toBeVisible()

  await send(bruno, 'Deploy at 17h')
  const msg = timeline(ana).locator('.msg', { hasText: 'Deploy at 17h' })
  await expect(msg).toBeVisible({ timeout: 1000 })

  // Up in an empty composer edits the last own message, in place
  await composer(bruno).press('ArrowUp')
  const editor = bruno.getByRole('textbox', { name: 'Edit message' })
  await editor.fill('Deploy at 18h')
  await editor.press('Enter')
  await expect(timeline(ana).locator('.msg', { hasText: 'Deploy at 18h' })).toContainText('(edited)', { timeout: 1000 })

  const own = timeline(bruno).locator('.msg', { hasText: 'Deploy at 18h' })
  await own.hover()
  await own.getByRole('button', { name: 'Add reaction' }).click()
  await bruno.getByRole('button', { name: 'rocket', exact: true }).click()
  await expect(timeline(ana).locator('.reaction', { hasText: '1' })).toBeVisible({ timeout: 1000 })

  await own.hover()
  await own.getByRole('button', { name: 'Delete' }).click()
  const confirm = bruno.getByRole('alertdialog')
  await expect(confirm).toContainText('Delete this message?')
  await confirm.getByRole('button', { name: 'Delete' }).click()
  await expect(timeline(ana).getByText('(message deleted)')).toBeVisible({ timeout: 1000 })
  await expect(timeline(ana).getByText('Deploy at 18h')).toHaveCount(0)
  await ana.context().close()
  await bruno.context().close()
})

test('thread in the side panel updates live', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  const bruno = await as(browser, BRUNO)
  for (const p of [ana, bruno]) await p.goto(`/teams/${teamName}/channels/projects`)
  await send(ana, 'Release checklist?')
  const root = timeline(ana).locator('.msg', { hasText: 'Release checklist?' })
  await root.hover()
  await root.getByRole('button', { name: 'Reply' }).click()
  const panel = ana.locator('.thread-panel')
  await expect(panel).toContainText('Release checklist?')

  const broot = timeline(bruno).locator('.msg', { hasText: 'Release checklist?' })
  await broot.hover()
  await broot.getByRole('button', { name: 'Reply' }).click()
  const reply = bruno.locator('.thread-panel [data-composer]')
  await reply.fill('In the docs folder')
  await reply.press('Enter')
  await expect(panel).toContainText('In the docs folder', { timeout: 1000 })
  await expect(root.getByRole('button', { name: /1 reply/ })).toBeVisible({ timeout: 1000 })
  await ana.context().close()
  await bruno.context().close()
})

test('offline: the message shows as failed with Retry and is stored once after reconnecting', async ({ page }) => {
  await login(page)
  await page.goto(`/teams/${teamName}/channels/projects`)
  await expect(composer(page)).toBeFocused()
  await page.context().setOffline(true)
  await send(page, 'Sent while offline')
  const failed = timeline(page).locator('.msg.failed', { hasText: 'Sent while offline' })
  await expect(failed).toContainText('Not sent.')
  await page.context().setOffline(false)
  await failed.getByRole('button', { name: 'Retry' }).click()
  await expect(timeline(page).locator('.msg.failed')).toHaveCount(0)
  await page.reload()
  await expect(timeline(page).locator('.msg', { hasText: 'Sent while offline' })).toHaveCount(1)
})

test('unread and mentions in the sidebar; Alt+Down opens the next channel with focus in its composer', async ({
  browser,
}) => {
  const ana = await as(browser, OWNER)
  await ana.goto(`/teams/${teamName}/channels/general`)
  const bruno = await as(browser, BRUNO)
  const anaUser = await api<{ user: { username: string } }>(ana, 'GET', '/me')
  await bruno.goto(`/teams/${teamName}/channels/projects`)
  await send(bruno, `@${anaUser.user.username} please review`)
  const item = ana.getByRole('link', { name: /Projects/ })
  await expect(item).toHaveClass(/unread/, { timeout: 2000 })
  await expect(item.locator('.chat-badge')).toHaveText('1')

  await composer(ana).press('Alt+ArrowDown')
  await expect(ana).toHaveURL(/\/channels\/projects$/)
  await expect(composer(ana)).toBeFocused()
  await expect(item).not.toHaveClass(/unread/)
  await ana.context().close()
  await bruno.context().close()
})

test('a private channel of others is "not found"; hostile messages stay inert', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  const team = await api<{ items: { id: string; name: string }[] }>(ana, 'GET', '/users/me/teams')
  const teamId = team.items.find((t) => t.name === teamName)!.id
  await api(ana, 'POST', '/channels', { team_id: teamId, display_name: 'Finance', type: 'P' }).catch(() => undefined)
  const carla = await as(browser, CARLA)
  await carla.goto(`/teams/${teamName}/channels/finance`)
  await expect(carla.getByRole('heading', { name: 'Channel not found' })).toBeVisible()

  // safe rendering: no script runs, nothing is fetched from evil.example, no CSP violation
  const csp = await collectCspViolations(ana)
  const evil: string[] = []
  ana.on('request', (r) => {
    if (r.url().includes('evil.example')) evil.push(r.url())
  })
  let dialogs = 0
  ana.on('dialog', async (d) => {
    dialogs++
    await d.dismiss()
  })
  await ana.goto(`/teams/${teamName}/channels/projects`)
  for (const payload of [
    '<img src=x onerror=alert(1)>',
    '[x](javascript:alert(1))',
    '[x](&#106;avascript:alert(1))',
    '![t](https://evil.example/pixel)',
  ]) {
    await send(ana, payload)
  }
  await expect(timeline(ana).locator('a[href="https://evil.example/pixel"]')).toBeVisible()
  for (const a of await timeline(ana).locator('a').all()) {
    await a.click({ trial: true }).catch(() => undefined)
  }
  expect(await timeline(ana).locator('.msg-text img').count()).toBe(0)
  expect(evil).toEqual([])
  expect(dialogs).toBe(0)
  expect(await csp()).toEqual([])
  await ana.context().close()
  await carla.context().close()
})

test('logging out clears the chat: the next user sees none of it', async ({ page }) => {
  await login(page)
  await page.goto(`/teams/${teamName}/channels/projects`)
  await expect(timeline(page)).toContainText('Release checklist?')
  await page.getByRole('button', { name: 'Log out' }).click()
  await login(page, CARLA.email, CARLA.password)
  await page.goto('/conversations')
  await expect(page.getByRole('link', { name: 'Projects' })).toHaveCount(0)
  await expect(page.getByText('Release checklist?')).toHaveCount(0)
})

test('chat screens have no serious accessibility violations', async ({ page }) => {
  await login(page)
  await page.goto(`/teams/${teamName}/channels/projects`)
  await expect(composer(page)).toBeFocused()
  const r = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()
  const serious = r.violations
    .filter((v) => v.impact === 'serious' || v.impact === 'critical')
    .map((v) => `${v.id}: ${v.nodes[0]?.target}`)
  expect(serious).toEqual([])
})

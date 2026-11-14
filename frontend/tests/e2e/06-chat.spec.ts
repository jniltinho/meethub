// Chat UI (chat-ui spec): sidebar, routes, channel view, composer, threads, actions, live updates between two
// browsers, recovery, safe rendering, keyboard and accessibility. Runs after the setup test (owner = Ana).
import { connect } from 'node:net'
import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Browser, type Page, type WebSocketRoute } from '@playwright/test'
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

/** startConversation opens a direct (one name) or group conversation from the sidebar's New message dialog */
async function startConversation(page: Page, ...names: string[]) {
  await page.getByRole('button', { name: 'New message' }).click()
  const dlg = page.getByRole('dialog', { name: 'New message' })
  for (const n of names) {
    await dlg.getByLabel('Find people').fill(n)
    await dlg.locator('li', { hasText: n }).getByRole('button', { name: 'Add' }).click()
  }
  await dlg.getByRole('button', { name: 'Start' }).click()
  await expect(page).toHaveURL(/\/messages\//)
  await expect(composer(page)).toBeFocused()
}

/** redis sends one command to the stack's Redis and returns its reply ('' when the server just hangs up) */
function redis(command: string): Promise<string> {
  const [host, port] = process.env.E2E_REDIS_ADDR!.split(':')
  return new Promise((resolve) => {
    let reply = ''
    const s = connect(Number(port), host!, () => s.write(command + '\r\n'))
    s.setTimeout(2000, () => s.destroy())
    s.on('data', (d) => {
      reply += d
      s.end()
    })
    s.on('error', () => undefined)
    s.on('close', () => resolve(reply))
  })
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

test('typing shows to the others in the channel', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  const bruno = await as(browser, BRUNO)
  for (const p of [ana, bruno]) await p.goto(`/teams/${teamName}/channels/projects`)
  await expect(composer(ana)).toBeFocused()
  // client and server both throttle typing to one notice every 3 s: keep typing until one gets through
  await expect(async () => {
    await composer(bruno).pressSequentially('x')
    await expect(ana.locator('.channel-view .typing')).toHaveText(`${BRUNO.name} is typing…`, { timeout: 1000 })
  }).toPass({ timeout: 15_000 })
  await ana.context().close()
  await bruno.context().close()
})

test('direct and group messages: presence, unread badge, delivery to every member', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  const bruno = await as(browser, BRUNO)
  const carla = await as(browser, CARLA)
  for (const p of [ana, bruno, carla]) await p.goto('/conversations')

  await startConversation(ana, BRUNO.name)
  const peer = ana.locator('a.chat-item', { hasText: BRUNO.name }).filter({ hasNotText: CARLA.name })
  await expect(peer.getByRole('img', { name: 'Online' })).toBeVisible()
  await send(ana, 'Lunch at noon?')
  const dm = bruno.locator('a.chat-item', { hasText: OWNER.name })
  await expect(dm.locator('.chat-badge')).toHaveText('1', { timeout: 2000 })
  await dm.click()
  await expect(timeline(bruno).locator('.msg', { hasText: 'Lunch at noon?' })).toBeVisible()
  await expect(dm.locator('.chat-badge')).toHaveCount(0)

  await startConversation(ana, BRUNO.name, CARLA.name)
  await send(ana, 'Release group is open')
  const gm = carla.locator('a.chat-item', { hasText: OWNER.name }).filter({ hasText: BRUNO.name })
  await expect(gm.locator('.chat-badge')).toHaveText('1', { timeout: 2000 })
  await gm.click()
  await expect(timeline(carla).locator('.msg', { hasText: 'Release group is open' })).toBeVisible()
  await send(carla, 'Count me in')
  await expect(timeline(ana).locator('.msg', { hasText: 'Count me in' })).toBeVisible({ timeout: 1000 })

  // the last tab of Bruno closes: Ana sees him offline
  await bruno.context().close()
  await expect(peer.getByRole('img', { name: 'Offline' })).toBeVisible({ timeout: 5000 })
  await ana.context().close()
  await carla.context().close()
})

test('a mention in another team shows on the team switcher', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  const bruno = await as(browser, BRUNO)
  const me = await api<{ user: { id: string; username: string } }>(bruno, 'GET', '/me')
  const ops = await api<{ id: string }>(ana, 'POST', '/teams', { display_name: 'Ops' })
  await api(ana, 'POST', `/teams/${ops.id}/members`, { user_id: me.user.id })
  await bruno.goto(`/teams/${teamName}/channels/general`)
  await expect(composer(bruno)).toBeFocused()

  const channels = await api<{ items: { id: string; name: string }[] }>(
    ana,
    'GET',
    `/users/me/teams/${ops.id}/channels`,
  )
  await api(ana, 'POST', '/posts', {
    channel_id: channels.items.find((c) => c.name === 'general')!.id,
    message: `@${me.user.username} the ops team needs you`,
    pending_post_id: 'e2e-ops-1',
  })
  const switcher = bruno.getByRole('button', { name: /Switch team/ })
  await expect(switcher.locator('.chat-badge')).toHaveText('1', { timeout: 2000 })
  await switcher.click()
  await bruno.getByRole('option', { name: /Ops/ }).getByRole('button').click()
  await expect(bruno).toHaveURL(/\/teams\/ops\/channels\/general$/)
  await expect(timeline(bruno)).toContainText('the ops team needs you')
  await expect(switcher.locator('.chat-badge')).toHaveCount(0)
  await ana.context().close()
  await bruno.context().close()
})

test('a cut socket catches up: nothing lost, nothing twice', async ({ browser }) => {
  const ana = await as(browser, OWNER)
  const bruno = await as(browser, BRUNO)
  // Bruno's sockets pass through the test, which cuts them and holds the reconnections back
  let blocked = false
  const open: WebSocketRoute[] = []
  await bruno.routeWebSocket(/\/api\/v1\/websocket/, (ws) => {
    if (blocked) return void ws.close()
    ws.connectToServer()
    open.push(ws)
  })
  for (const p of [ana, bruno]) await p.goto(`/teams/${teamName}/channels/projects`)
  const seen = (text: string) => timeline(bruno).locator('.msg', { hasText: text })
  await send(ana, 'before the cut')
  await expect(seen('before the cut')).toHaveCount(1)

  blocked = true
  for (const ws of open.splice(0)) await ws.close()
  const missed = ['missed one', 'missed two', 'missed three']
  for (const m of missed) await send(ana, m)
  await expect(timeline(ana).locator('.msg', { hasText: 'missed three' })).toBeVisible()
  await expect(seen('missed one')).toHaveCount(0)
  blocked = false

  for (const m of missed) await expect(seen(m)).toHaveCount(1, { timeout: 40_000 }) // reconnect backoff
  await send(ana, 'after the cut')
  await expect(seen('after the cut')).toHaveCount(1)
  for (const m of ['before the cut', ...missed]) await expect(seen(m)).toHaveCount(1)
  await ana.context().close()
  await bruno.context().close()
})

test('a Redis restart loses nothing and duplicates nothing', async ({ browser }) => {
  test.skip(!process.env.E2E_REDIS_ADDR, 'needs the stack of make test-e2e (E2E_REDIS_ADDR)')
  const ana = await as(browser, OWNER)
  const bruno = await as(browser, BRUNO)
  let resyncs = 0
  bruno.on('websocket', (ws) =>
    ws.on('framereceived', (f) => {
      if (String(f.payload).includes('"resync"')) resyncs++
    }),
  )
  for (const p of [ana, bruno]) await p.goto(`/teams/${teamName}/channels/projects`)
  const seen = (text: string) => timeline(bruno).locator('.msg', { hasText: text })
  await send(ana, 'before the restart')
  await expect(seen('before the restart')).toHaveCount(1)

  await redis('SHUTDOWN NOSAVE') // Compose restarts it (restart: unless-stopped)
  await send(ana, 'while Redis restarts')
  await expect(seen('while Redis restarts')).toHaveCount(1, { timeout: 15_000 })
  await expect.poll(() => redis('PING'), { timeout: 30_000 }).toContain('PONG')
  await expect.poll(async () => (await ana.request.get('/api/v1/ready')).status(), { timeout: 30_000 }).toBe(200)
  // the restored subscription asks every client to resync: reloading what it already has must not duplicate it
  await expect.poll(() => resyncs, { timeout: 30_000 }).toBeGreaterThan(0)
  await send(ana, 'after the restart')
  const all = ['before the restart', 'while Redis restarts', 'after the restart']
  for (const m of all) await expect(seen(m)).toHaveCount(1)
  await expect(timeline(ana).locator('.msg.failed')).toHaveCount(0)
  await bruno.reload()
  for (const m of all) await expect(seen(m)).toHaveCount(1)
  await ana.context().close()
  await bruno.context().close()
})

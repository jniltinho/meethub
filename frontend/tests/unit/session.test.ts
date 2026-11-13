import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useSession } from '@/stores/session'
import type { Profile } from '@/api/types'

const profile = {
  user: { id: 'u1' },
  organization: { id: 'o1' },
  role: 'owner',
  permissions: [],
  preferences: { theme: '', language: '' },
  csrf_token: 'c',
} as unknown as Profile

describe('logout', () => {
  beforeEach(() => setActivePinia(createPinia()))
  afterEach(() => vi.unstubAllGlobals())

  it('keeps the local session when the server could not revoke it', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')))
    const s = useSession()
    s.profile = profile
    await expect(s.logout()).rejects.toMatchObject({ code: 'network' })
    expect(s.loggedIn).toBe(true)
  })

  it('clears after the server revoked the session', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 204 })))
    const s = useSession()
    s.profile = profile
    await s.logout()
    expect(s.loggedIn).toBe(false)
  })

  it('clears when the server says the session already ended', async () => {
    const body = JSON.stringify({ error: { code: 'unauthenticated', message: 'x' } })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(body, { status: 401 })))
    const s = useSession()
    s.profile = profile
    await s.logout()
    expect(s.loggedIn).toBe(false)
  })
})

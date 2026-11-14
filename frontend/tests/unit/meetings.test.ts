import { nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChat } from '@/stores/chat'
import { useMeetings } from '@/stores/meetings'
import { useSession } from '@/stores/session'
import type { ServerEvent } from '@/chat/types'
import type { Profile } from '@/api/types'

const ev = (channel: string, revision: number, users: string[], team = 't1'): ServerEvent => ({
  event: 'meeting_updated',
  data: { channel_id: channel, revision, participants: users.map((u, i) => ({ user_id: u, joined_at: i })) },
  broadcast: { channel_id: channel, team_id: team },
  seq: 1,
})

let listed: () => unknown
let hold = false
let release: (() => void) | null

describe('meetings store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    listed = () => ({
      enabled: true,
      rooms: [{ channel_id: 'c1', team_id: 't1', revision: 3, participants: [{ user_id: 'u1', joined_at: 1 }] }],
    })
    hold = false
    release = null
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string) => {
        if (url === '/api/v1/meetings') {
          const body = JSON.stringify(listed())
          if (hold) await new Promise<void>((r) => (release = r))
          return new Response(body)
        }
        return new Response(JSON.stringify([]))
      }),
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('loads the rooms and follows meeting_updated through the chat socket, in revision order', async () => {
    const chat = useChat()
    const meetings = useMeetings()
    await meetings.load()
    expect(meetings.enabled).toBe(true)
    expect(meetings.people('c1').map((p) => p.user_id)).toEqual(['u1'])

    chat.apply(ev('c2', 5, ['u2', 'u3']))
    expect(meetings.people('c2')).toHaveLength(2)
    expect(meetings.rooms.c2?.team_id).toBe('t1')
    expect(meetings.active[0]?.channel_id).toBe('c2') // most people first

    // an older snapshot published late by another replica is ignored
    chat.apply(ev('c2', 4, ['u2']))
    expect(meetings.people('c2')).toHaveLength(2)
    // an empty snapshot removes the room, and an older one cannot bring it back
    chat.apply(ev('c1', 4, []))
    chat.apply(ev('c1', 3, ['u1']))
    expect(meetings.people('c1')).toEqual([])
    chat.apply({ event: 'typing', data: { channel_id: 'c2' }, broadcast: {}, seq: 2 })
    expect(Object.keys(meetings.rooms)).toEqual(['c2'])
  })

  it('a load answered after newer events keeps the events', async () => {
    const chat = useChat()
    const meetings = useMeetings()
    hold = true // hold the next answer
    const loading = meetings.load()
    await vi.waitFor(() => expect(release).not.toBeNull())
    chat.apply(ev('c1', 4, [])) // u1 left while the list was on its way
    release!()
    await loading
    expect(meetings.people('c1')).toEqual([])
  })

  it('forgets rooms of channels and teams the user loses', async () => {
    const chat = useChat()
    const meetings = useMeetings()
    useSession().profile = { user: { id: 'me' } } as Profile
    await meetings.load()
    chat.apply(ev('c2', 1, ['u2'], 't2'))
    chat.apply({ event: 'user_removed', data: { channel_id: 'c1', user_id: 'me' }, broadcast: {}, seq: 3 })
    expect(meetings.people('c1')).toEqual([])
    chat.apply({ event: 'leave_team', data: { team_id: 't2', user_id: 'me' }, broadcast: {}, seq: 4 })
    expect(meetings.rooms).toEqual({})
  })

  it('reloads on resync and forgets everything when the session ends', async () => {
    const chat = useChat()
    const meetings = useMeetings()
    const session = useSession()
    session.profile = { user: { id: 'me' } } as Profile
    await nextTick()
    void chat.resync().catch(() => undefined)
    await vi.waitFor(() => expect(meetings.enabled).toBe(true))
    meetings.openCall('c1')
    session.clear()
    await nextTick()
    expect(meetings.enabled).toBe(false)
    expect(meetings.rooms).toEqual({})
    expect(meetings.callChannel).toBe('')
  })
})

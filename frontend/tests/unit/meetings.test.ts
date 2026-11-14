import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChat } from '@/stores/chat'
import { useMeetings } from '@/stores/meetings'
import { useSession } from '@/stores/session'
import type { ServerEvent } from '@/chat/types'

const ev = (data: Record<string, unknown>, team = 't1'): ServerEvent => ({
  event: 'meeting_updated',
  data,
  broadcast: { channel_id: String(data.channel_id), team_id: team },
  seq: 1,
})

describe('meetings store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async (url: string) => {
        if (url === '/api/v1/meetings')
          return new Response(
            JSON.stringify({
              enabled: true,
              rooms: [{ channel_id: 'c1', team_id: 't1', participants: [{ user_id: 'u1', joined_at: 1 }] }],
            }),
          )
        return new Response(JSON.stringify([]))
      }),
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('loads the rooms and follows meeting_updated through the chat socket', async () => {
    const chat = useChat()
    const meetings = useMeetings()
    await meetings.load()
    expect(meetings.enabled).toBe(true)
    expect(meetings.people('c1').map((p) => p.user_id)).toEqual(['u1'])

    // the chat store forwards every event to the meetings hook
    chat.apply(
      ev({
        channel_id: 'c2',
        participants: [
          { user_id: 'u2', joined_at: 2 },
          { user_id: 'u3', joined_at: 3 },
        ],
      }),
    )
    expect(meetings.people('c2')).toHaveLength(2)
    expect(meetings.rooms.c2?.team_id).toBe('t1')
    expect(meetings.active[0]?.channel_id).toBe('c2') // most people first

    // an empty snapshot removes the room; other events are ignored
    chat.apply(ev({ channel_id: 'c1', participants: [] }))
    chat.apply({ event: 'typing', data: { channel_id: 'c2' }, broadcast: {}, seq: 2 })
    expect(meetings.people('c1')).toEqual([])
    expect(Object.keys(meetings.rooms)).toEqual(['c2'])
  })

  it('reloads on resync and forgets everything when the session ends', async () => {
    const chat = useChat()
    const meetings = useMeetings()
    void chat.resync().catch(() => undefined)
    await vi.waitFor(() => expect(meetings.enabled).toBe(true))
    meetings.openCall('c1')
    useSession().clear()
    expect(meetings.enabled).toBe(false)
    expect(meetings.rooms).toEqual({})
    expect(meetings.callChannel).toBe('')
  })
})

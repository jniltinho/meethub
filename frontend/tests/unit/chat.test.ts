import { computed } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { ChatSocket, BACKOFF_MAX, BACKOFF_MIN } from '@/realtime/socket'
import { useChat } from '@/stores/chat'
import { useSession } from '@/stores/session'
import type { Post, ServerEvent } from '@/chat/types'
import type { Profile } from '@/api/types'

// FakeSocket stands in for the browser WebSocket.
class FakeSocket {
  static all: FakeSocket[] = []
  onopen: (() => void) | null = null
  onmessage: ((m: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  sent: string[] = []
  closed = false
  constructor(public url: string) {
    FakeSocket.all.push(this)
  }
  send(s: string) {
    this.sent.push(s)
  }
  close() {
    this.closed = true
  }
  // test helpers
  open() {
    this.onopen?.()
    this.emit({ event: 'hello', data: {}, broadcast: {}, seq: 0 })
  }
  emit(m: unknown) {
    this.onmessage?.({ data: JSON.stringify(m) })
  }
  drop() {
    this.onclose?.()
  }
}

const factory = (url: string) => new FakeSocket(url) as unknown as WebSocket

function post(over: Partial<Post>): Post {
  return {
    id: 'p1',
    channel_id: 'c1',
    user_id: 'u2',
    root_id: '',
    message: 'hello',
    type: '',
    create_at: 1000,
    update_at: 1000,
    edit_at: 0,
    delete_at: 0,
    reply_count: 0,
    last_reply_at: 0,
    revision: 10,
    ...over,
  }
}

function ev(event: string, data: Record<string, unknown>, seq = 1): ServerEvent {
  return { event, data, broadcast: { channel_id: 'c1' }, seq }
}

describe('ChatSocket', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    FakeSocket.all = []
  })
  afterEach(() => vi.useRealTimers())

  it('asks for a resync on a seq gap, on resync and after reconnecting, never on the first connection', () => {
    const reasons: string[] = []
    const events: string[] = []
    const s = new ChatSocket({
      onEvent: (e) => events.push(`${e.event}#${e.seq}`),
      onResync: (r) => reasons.push(r),
      createSocket: factory,
      random: () => 0.5,
      isVisible: () => true,
    })
    s.start()
    const ws = FakeSocket.all[0]!
    ws.open()
    expect(s.status).toBe('open')
    ws.emit({ event: 'posted', data: {}, broadcast: {}, seq: 1 })
    ws.emit({ event: 'posted', data: {}, broadcast: {}, seq: 3 }) // seq 2 lost
    ws.emit({ event: 'resync', data: {}, broadcast: {}, seq: 4 })
    expect(events).toEqual(['posted#1', 'posted#3'])
    expect(reasons).toEqual(['gap', 'resync'])

    ws.drop()
    expect(s.status).toBe('reconnecting')
    vi.advanceTimersByTime(BACKOFF_MIN)
    const again = FakeSocket.all[1]!
    again.open()
    expect(reasons).toEqual(['gap', 'resync', 'reconnect'])
    vi.advanceTimersByTime(60000)
    expect(reasons.at(-1)).toBe('interval')
    s.stop()
    expect(again.closed).toBe(true)
    expect(s.status).toBe('closed')
  })

  it('backs off exponentially up to 30 s with jitter', () => {
    const s = new ChatSocket({ onEvent: () => {}, onResync: () => {}, random: () => 1 })
    expect(s.backoff(1)).toBe(BACKOFF_MIN)
    expect(s.backoff(3)).toBe(4 * BACKOFF_MIN)
    expect(s.backoff(20)).toBe(BACKOFF_MAX)
    const low = new ChatSocket({ onEvent: () => {}, onResync: () => {}, random: () => 0 })
    expect(low.backoff(20)).toBe(BACKOFF_MAX / 2)
  })

  it('answers actions by seq_reply and fails them when the socket drops', async () => {
    const s = new ChatSocket({ onEvent: () => {}, onResync: () => {}, createSocket: factory })
    s.start()
    const ws = FakeSocket.all[0]!
    ws.open()
    const ok = s.send('ping')
    const seq = JSON.parse(ws.sent[0]!).seq
    ws.emit({ status: 'OK', seq_reply: seq, data: { text: 'pong' } })
    await expect(ok).resolves.toEqual({ text: 'pong' })
    const lost = s.send('get_statuses', { user_ids: [] })
    ws.drop()
    await expect(lost).rejects.toThrow('disconnected')
    s.stop()
  })
})

describe('chat store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    useSession().profile = { user: { id: 'me' }, permissions: [] } as unknown as Profile
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async () => new Response('[]', { status: 200 })),
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  function withTimeline(chat: ReturnType<typeof useChat>) {
    chat.channels.c1 = {
      id: 'c1',
      team_id: 't1',
      type: 'O',
      name: 'projects',
      display_name: 'Projects',
      purpose: '',
      header: '',
      creator_id: 'u1',
      is_default: false,
      revision: 5,
      total_msg_count: 0,
      last_post_at: 0,
      archived_at: 0,
      created_at: 0,
      updated_at: 0,
      member_count: 2,
      msg_count: 0,
      mention_count: 0,
      unread: 0,
    }
    chat.timelines.c1 = { ids: [], cursor: '', complete: true, revision: 5 }
  }

  it('ignores duplicate and stale events (out of order)', () => {
    const chat = useChat()
    withTimeline(chat)
    chat.apply(ev('posted', { post: post({ revision: 10 }), mentions: ['me'] }))
    chat.apply(ev('posted', { post: post({ revision: 10 }), mentions: ['me'] })) // duplicate
    expect(chat.rootsOf('c1')).toHaveLength(1)
    expect(chat.channels.c1!.total_msg_count).toBe(1)
    expect(chat.channels.c1!.mention_count).toBe(1)

    chat.apply(ev('post_edited', { post: post({ message: 'v3', revision: 13 }) }))
    chat.apply(ev('post_edited', { post: post({ message: 'v2', revision: 12 }) })) // late
    expect(chat.posts.p1!.message).toBe('v3')

    chat.apply(ev('reaction_added', { reaction: { post_id: 'p1', user_id: 'u3', emoji_name: 'rocket' }, revision: 14 }))
    chat.apply(ev('reaction_added', { reaction: { post_id: 'p1', user_id: 'u3', emoji_name: 'rocket' }, revision: 14 }))
    expect(chat.posts.p1!.reactions).toEqual([{ emoji_name: 'rocket', count: 1, user_ids: ['u3'] }])

    chat.apply(ev('post_deleted', { post: post({ message: undefined, delete_at: 2000, revision: 15 }) }))
    chat.apply(ev('post_edited', { post: post({ message: 'zombie', revision: 13 }) })) // older than the deletion
    expect(chat.posts.p1!.delete_at).toBe(2000)
    expect(chat.posts.p1!.message).toBeUndefined()
    expect(chat.posts.p1!.reactions).toBeUndefined()
  })

  it('keeps the timeline in creation order whatever the arrival order', () => {
    const chat = useChat()
    withTimeline(chat)
    chat.applyPost(post({ id: 'b', create_at: 2000, revision: 12 }))
    chat.applyPost(post({ id: 'a', create_at: 1000, revision: 11 }))
    chat.applyPost(post({ id: 'c', create_at: 2000, revision: 13 }))
    expect(chat.rootsOf('c1').map((p) => p.id)).toEqual(['a', 'b', 'c'])
  })

  it('counts a reply on its root once, and a thread deletion tombstones every reply', () => {
    const chat = useChat()
    withTimeline(chat)
    chat.applyPost(post({ id: 'root', revision: 10 }))
    const r = post({ id: 'r1', root_id: 'root', create_at: 1500, revision: 11 })
    chat.apply(ev('posted', { post: r, mentions: [] }))
    chat.apply(ev('posted', { post: r, mentions: [] }))
    expect(chat.posts.root!.reply_count).toBe(1)
    expect(chat.rootsOf('c1').map((p) => p.id)).toEqual(['root'])
    chat.apply(ev('post_deleted', { post: post({ id: 'root', delete_at: 3000, revision: 12 }), delete_thread: true }))
    expect(chat.posts.r1!.delete_at).toBe(3000)
    expect(chat.posts.r1!.message).toBeUndefined()
  })

  it('replaces a pending message with the stored one, and keeps a failed one for retry with the same key', async () => {
    const chat = useChat()
    withTimeline(chat)
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')))
    await expect(chat.send('c1', 'hi', '', 'me:1:k')).rejects.toBeTruthy()
    const failed = chat.rootsOf('c1')
    expect(failed.map((p) => p.local)).toEqual(['failed'])

    const stored = post({ id: 'srv', user_id: 'me', message: 'hi', pending_post_id: 'me:1:k', revision: 20 })
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(stored), { status: 201 }))
    vi.stubGlobal('fetch', fetchMock)
    await chat.retry(failed[0]!.id)
    expect(JSON.parse(fetchMock.mock.calls[0]![1].body).pending_post_id).toBe('me:1:k')
    chat.apply(ev('posted', { post: stored, mentions: [] })) // the event of the same post arrives too
    expect(chat.rootsOf('c1').map((p) => [p.id, p.local])).toEqual([['srv', undefined]])
  })

  it('forgets a channel the user was removed from, and everything on session end', () => {
    const chat = useChat()
    withTimeline(chat)
    chat.openChannelId = 'c1'
    chat.applyPost(post({}))
    chat.apply(ev('user_removed', { channel_id: 'c1', user_id: 'me' }))
    expect(chat.channels.c1).toBeUndefined()
    expect(chat.posts.p1).toBeUndefined()
    expect(chat.lostChannel).toBe('c1')

    withTimeline(chat)
    chat.applyPost(post({ id: 'p9' }))
    useSession().clear()
    expect(chat.posts).toEqual({})
    expect(chat.channels).toEqual({})
  })

  it('shows typing for 5 seconds or until that user posts', () => {
    vi.useFakeTimers()
    const chat = useChat()
    withTimeline(chat)
    chat.start(factory)
    chat.apply({ event: 'typing', data: { user_id: 'u2', parent_id: '' }, broadcast: { channel_id: 'c1' }, seq: 1 })
    expect(chat.typingIn('c1')).toEqual(['u2'])
    chat.apply(ev('posted', { post: post({ id: 'x', revision: 30 }), mentions: [] }))
    expect(chat.typingIn('c1')).toEqual([])
    chat.apply({ event: 'typing', data: { user_id: 'u2', parent_id: '' }, broadcast: { channel_id: 'c1' }, seq: 2 })
    vi.advanceTimersByTime(6000)
    expect(chat.typingIn('c1')).toEqual([])
    chat.reset()
    vi.useRealTimers()
  })
})

describe('chat store thread loading', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    useSession().profile = { user: { id: 'me' }, permissions: [] } as unknown as Profile
  })
  afterEach(() => vi.unstubAllGlobals())

  it('shows replies already known from the channel history', async () => {
    const chat = useChat()
    const root = post({ id: 'root', revision: 10 })
    const reply = post({ id: 'r1', root_id: 'root', create_at: 1500, revision: 11 })
    chat.applyPost(root)
    chat.applyPost(reply)
    const replies = chat.threadOf.bind(chat)
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(async () => new Response(JSON.stringify({ items: [root, reply] }), { status: 200 })),
    )
    const shown = computed(() => replies('root').map((p) => p.id))
    expect(shown.value).toEqual([])
    await chat.openThread('root')
    expect(shown.value).toEqual(['r1'])
  })
})

// The parity change (add-chat-mockup-parity): pins, the detached timeline after a jump, and requests answered
// after a logout.
describe('chat store: pins, jumps and logout', () => {
  const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200 })
  beforeEach(() => {
    setActivePinia(createPinia())
    useSession().profile = { user: { id: 'me' }, permissions: [] } as unknown as Profile
  })
  afterEach(() => vi.unstubAllGlobals())

  it('a search answered after a logout never reaches the next user', async () => {
    const chat = useChat()
    let answer: (r: Response) => void = () => undefined
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(() => new Promise<Response>((resolve) => (answer = resolve))),
    )
    const pending = chat.runSearch('budget')
    chat.reset() // the session ended while the search was on its way
    answer(json({ posts: [post({ id: 's1', message: 'budget approved' })], has_more: false, channels: {} }))
    await pending
    expect(chat.search.posts).toEqual([])
    expect(chat.search.terms).toBe('')
  })

  it('a pin event during the load of the pinned list is kept, newest pin first', async () => {
    const chat = useChat()
    let answer: (r: Response) => void = () => undefined
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(() => new Promise<Response>((resolve) => (answer = resolve))),
    )
    const loading = chat.loadPinned('c1')
    chat.apply(ev('post_edited', { post: post({ id: 'late', revision: 30, is_pinned: true, pinned_at: 3000 }) }))
    answer(json({ items: [post({ id: 'old', revision: 20, is_pinned: true, pinned_at: 2000 })] }))
    await loading
    expect(chat.pinned.c1).toEqual(['late', 'old'])
    // unpinning through an event takes it out of the list
    chat.apply(ev('post_edited', { post: post({ id: 'old', revision: 31, is_pinned: false }) }, 2))
    expect(chat.pinned.c1).toEqual(['late'])
  })

  it('a thread deletion takes its pinned replies out of the pinned list', () => {
    const chat = useChat()
    chat.pinned.c1 = []
    chat.applyPost(post({ id: 'root', revision: 10 }))
    chat.applyPost(post({ id: 'reply', root_id: 'root', revision: 11, is_pinned: true, pinned_at: 500 }))
    expect(chat.pinned.c1).toEqual(['reply'])
    chat.apply(ev('post_deleted', { post: post({ id: 'root', revision: 12, delete_at: 900 }), delete_thread: true }))
    expect(chat.pinned.c1).toEqual([])
    expect(chat.posts.reply!.is_pinned).toBe(false)
  })

  it('a message that arrived while the timeline was detached is in it once it is attached again', async () => {
    const chat = useChat()
    chat.applyPost(post({ id: 'old', create_at: 100, revision: 1 }))
    chat.timelines.c1 = { ids: ['old'], cursor: '', complete: true, revision: 1, newer: '100:old' }
    // live, while the window does not reach the present: cached, not appended
    chat.apply(ev('posted', { post: post({ id: 'live', create_at: 300, revision: 3 }) }))
    expect(chat.timelines.c1!.ids).toEqual(['old'])
    // the last forward page (a snapshot from before the live message), then the catch-up with the same revision
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockImplementation(async (url: string) =>
          url.includes('after=')
            ? json({ items: [post({ id: 'mid', create_at: 200, revision: 2 })] })
            : json({ posts: [post({ id: 'live', create_at: 300, revision: 3 })], next_revision: 3, has_more: false }),
        ),
    )
    await chat.loadNewer('c1')
    await vi.waitFor(() => expect(chat.timelines.c1!.ids).toEqual(['old', 'mid', 'live']))
    expect(chat.timelines.c1!.newer).toBe('')
  })
})

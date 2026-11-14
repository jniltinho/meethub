// The chat store (design D9): entities normalized by id; every post keeps its `revision` and older updates are
// ignored, so duplicate, late or out-of-order events and API pages converge to the same state. The WebSocket feeds
// it live; the API fills gaps (resync). Logging out (session.clear) stops the socket and forgets everything.
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api } from '@/api/client'
import { ChatSocket, type SocketState } from '@/realtime/socket'
import { useSession } from '@/stores/session'
import type {
  Changes,
  ChannelInfo,
  ChannelWithState,
  Conversation,
  LocalPost,
  Page,
  Post,
  Presence,
  PublicUser,
  ReactionGroup,
  ServerEvent,
  Status,
  TeamWithState,
} from '@/chat/types'

/** a channel or conversation the user belongs to, with unread state */
export interface ChatChannel extends ChannelInfo {
  msg_count: number
  mention_count: number
  unread: number
  members?: PublicUser[]
}

interface Timeline {
  ids: string[] // all posts loaded (roots and replies), oldest first
  cursor: string // older page cursor; '' when complete
  complete: boolean
  revision: number // catch-up baseline
}

const TYPING_MS = 5000

export function byTime(a: Post, b: Post): number {
  return a.create_at - b.create_at || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0)
}

export function newPendingId(userId: string): string {
  const rnd = Math.random().toString(36).slice(2, 10)
  return `${userId}:${Date.now()}:${rnd}`
}

export const useChat = defineStore('chat', () => {
  const teams = ref<TeamWithState[]>([])
  const channels = ref<Record<string, ChatChannel>>({})
  const teamChannels = ref<Record<string, string[]>>({})
  const conversationIds = ref<string[]>([])
  const posts = ref<Record<string, LocalPost>>({})
  const timelines = ref<Record<string, Timeline>>({})
  const threads = ref<Record<string, string[]>>({})
  const users = ref<Record<string, PublicUser>>({})
  const statuses = ref<Record<string, Presence>>({})
  const typing = ref<Record<string, Record<string, { parent: string; until: number }>>>({})
  const socketState = ref<SocketState>('closed')
  const openChannelId = ref<string>('')
  /** set when the open channel disappears for the user (removed, archived team): views show "not found" */
  const lostChannel = ref<string>('')
  /** the team the sidebar shows (the open channel's, or the last one) */
  const currentTeamId = ref<string>('')
  /** the root of the thread shown in the side panel ('' when none) */
  const threadRoot = ref<string>('')

  let socket: ChatSocket | null = null
  let typingTimer: ReturnType<typeof setInterval> | null = null
  const pendingUsers = new Set<string>()
  let userTimer: ReturnType<typeof setTimeout> | null = null

  const session = useSession()
  const me = computed(() => session.profile?.user.id ?? '')

  // ── lifecycle ──────────────────────────────────────────────────────────────

  function start(factory?: (url: string) => WebSocket): void {
    if (socket) return
    socket = new ChatSocket({
      onEvent: apply,
      onResync: () => void resync(),
      onState: (s) => (socketState.value = s),
      createSocket: factory,
    })
    socket.start()
    typingTimer = setInterval(expireTyping, 1000)
  }

  function reset(): void {
    socket?.stop()
    socket = null
    if (typingTimer) clearInterval(typingTimer)
    typingTimer = null
    teams.value = []
    channels.value = {}
    teamChannels.value = {}
    conversationIds.value = []
    posts.value = {}
    timelines.value = {}
    threads.value = {}
    users.value = {}
    statuses.value = {}
    typing.value = {}
    socketState.value = 'closed'
    openChannelId.value = ''
    lostChannel.value = ''
    threadRoot.value = ''
    currentTeamId.value = ''
    pendingUsers.clear()
  }

  // a session ending (logout, expiry) must not leave one user's chat visible to the next
  session.$onAction(({ name, after }) => {
    if (name === 'clear') after(() => reset())
  })

  // ── users and statuses ─────────────────────────────────────────────────────

  function rememberUsers(list: PublicUser[] | null): void {
    for (const u of list ?? []) users.value[u.id] = u
  }

  /** needUser queues a profile lookup (batched through POST /users/ids). */
  function needUser(id: string): void {
    if (!id || users.value[id] || pendingUsers.has(id)) return
    pendingUsers.add(id)
    if (userTimer) return
    userTimer = setTimeout(async () => {
      userTimer = null
      const ids = [...pendingUsers].slice(0, 200)
      try {
        rememberUsers(await api<PublicUser[]>('POST', '/users/ids', ids))
        void loadStatuses(ids).catch(() => undefined)
      } catch {
        /* names stay unknown until the next resync */
      } finally {
        for (const id of ids) pendingUsers.delete(id)
      }
    }, 20)
  }

  async function loadStatuses(ids: string[]): Promise<void> {
    if (ids.length === 0) return
    const list = await api<Status[] | null>('POST', '/users/status/ids', ids.slice(0, 200))
    for (const s of list ?? []) statuses.value[s.user_id] = s.status
  }

  // ── teams and channels ─────────────────────────────────────────────────────

  async function loadTeams(): Promise<TeamWithState[]> {
    teams.value = (await api<Page<TeamWithState>>('GET', '/users/me/teams')).items
    return teams.value
  }

  function putChannel(c: ChannelWithState | Conversation | ChannelInfo): ChatChannel {
    const prev = channels.value[c.id]
    const next = { ...(prev ?? { member_count: 0, msg_count: 0, mention_count: 0, unread: 0 }), ...c } as ChatChannel
    if ('unread' in c && !('msg_count' in c)) next.msg_count = c.total_msg_count - c.unread
    channels.value[c.id] = next
    return next
  }

  async function loadChannels(teamId: string): Promise<void> {
    const ids: string[] = []
    let cursor = ''
    do {
      const page = await api<Page<ChannelWithState>>(
        'GET',
        `/users/me/teams/${teamId}/channels?per_page=200${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`,
      )
      for (const c of page.items) ids.push(putChannel(c).id)
      cursor = page.next_cursor ?? ''
    } while (cursor)
    teamChannels.value[teamId] = ids
  }

  async function loadConversations(): Promise<void> {
    const page = await api<Page<Conversation>>('GET', '/users/me/conversations?per_page=200')
    conversationIds.value = page.items.map((c) => {
      rememberUsers(c.members)
      return putChannel(c).id
    })
    void loadStatuses([...new Set(page.items.flatMap((c) => c.members.map((m) => m.id)))])
  }

  async function loadChannel(id: string): Promise<ChatChannel> {
    return putChannel(await api<ChannelInfo>('GET', `/channels/${id}`))
  }

  // ── posts ──────────────────────────────────────────────────────────────────

  /** applyPost stores a post unless a same-or-newer revision is already there; returns whether it changed. */
  function applyPost(p: Post): boolean {
    const cur = posts.value[p.id]
    if (cur && cur.revision >= p.revision) return false
    if (p.pending_post_id) {
      const local = posts.value['pending:' + p.pending_post_id]
      if (local) dropLocal(local)
    }
    const stored: LocalPost = { ...p }
    if (stored.delete_at) {
      delete stored.message
      delete stored.reactions
    }
    posts.value[p.id] = stored
    needUser(p.user_id)
    const tl = timelines.value[p.channel_id]
    if (tl && !tl.ids.includes(p.id)) insertSorted(tl.ids, p.id)
    if (p.root_id) {
      const th = threads.value[p.root_id]
      if (th && !th.includes(p.id)) insertSorted(th, p.id)
    }
    return true
  }

  function insertSorted(ids: string[], id: string): void {
    const p = posts.value[id]!
    let i = ids.length
    while (i > 0 && byTime(posts.value[ids[i - 1]!]!, p) > 0) i--
    ids.splice(i, 0, id)
  }

  function dropLocal(local: LocalPost): void {
    delete posts.value[local.id]
    for (const list of [
      timelines.value[local.channel_id]?.ids,
      local.root_id ? threads.value[local.root_id] : undefined,
    ]) {
      if (!list) continue
      const i = list.indexOf(local.id)
      if (i >= 0) list.splice(i, 1)
    }
  }

  async function openChannel(id: string): Promise<ChatChannel> {
    openChannelId.value = id
    lostChannel.value = ''
    const ch = await loadChannel(id)
    if (!timelines.value[id]) {
      const tl: Timeline = { ids: [], cursor: '', complete: false, revision: ch.revision }
      timelines.value[id] = tl
      await loadOlder(id)
    }
    void markRead(id)
    return ch
  }

  async function loadOlder(id: string): Promise<void> {
    const tl = timelines.value[id]
    if (!tl || tl.complete) return
    const page = await api<Page<Post>>(
      'GET',
      `/channels/${id}/posts?per_page=60${tl.cursor ? `&cursor=${encodeURIComponent(tl.cursor)}` : ''}`,
    )
    for (const p of page.items) {
      applyPost(p)
      if (!tl.ids.includes(p.id)) insertSorted(tl.ids, p.id)
    }
    tl.cursor = page.next_cursor ?? ''
    tl.complete = !tl.cursor
  }

  async function openThread(rootId: string): Promise<void> {
    threadRoot.value = rootId
    threads.value[rootId] = []
    const ids = threads.value[rootId] // the reactive array: inserts must go through it
    let cursor = ''
    do {
      const page = await api<Page<Post>>(
        'GET',
        `/posts/${rootId}/thread?per_page=200${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`,
      )
      for (const p of page.items) {
        applyPost(p)
        if (p.root_id && !ids.includes(p.id)) insertSorted(ids, p.id)
      }
      cursor = page.next_cursor ?? ''
    } while (cursor)
  }

  /** catchUp pages every change of a loaded channel since its baseline revision. */
  async function catchUp(id: string): Promise<void> {
    const tl = timelines.value[id]
    if (!tl) return
    for (;;) {
      const ch = await api<Changes>('GET', `/channels/${id}/posts?since_revision=${tl.revision}&per_page=200`)
      for (const p of ch.posts) applyPost(p)
      tl.revision = Math.max(tl.revision, ch.next_revision)
      if (!ch.has_more) break
    }
  }

  let resyncing: Promise<void> | null = null
  /** resync reloads what events may have missed: memberships, unread counts, loaded channels, statuses. */
  function resync(): Promise<void> {
    if (resyncing) return resyncing
    resyncing = (async () => {
      try {
        await loadTeams()
        await Promise.all(Object.keys(teamChannels.value).map((t) => loadChannels(t).catch(() => undefined)))
        await loadConversations()
        for (const id of Object.keys(timelines.value)) await catchUp(id).catch(() => undefined)
        await loadStatuses(Object.keys(users.value)).catch(() => undefined)
      } finally {
        resyncing = null
      }
    })()
    return resyncing
  }

  async function markRead(id: string): Promise<void> {
    const ch = channels.value[id]
    if (ch) {
      ch.msg_count = ch.total_msg_count
      ch.unread = 0
      ch.mention_count = 0
    }
    await api('POST', `/channels/${id}/view`).catch(() => undefined)
    void loadTeams().catch(() => undefined)
  }

  // ── sending ────────────────────────────────────────────────────────────────

  /** send shows the message at once as pending, then replaces it with the stored one (same pending_post_id). */
  async function send(
    channelId: string,
    message: string,
    rootId = '',
    pendingId = newPendingId(me.value),
  ): Promise<void> {
    const now = Date.now()
    const local: LocalPost = {
      id: 'pending:' + pendingId,
      channel_id: channelId,
      user_id: me.value,
      root_id: rootId,
      message,
      type: '',
      pending_post_id: pendingId,
      create_at: now,
      update_at: now,
      edit_at: 0,
      delete_at: 0,
      reply_count: 0,
      last_reply_at: 0,
      revision: 0,
      local: 'pending',
    }
    posts.value[local.id] = local
    const tl = timelines.value[channelId]
    if (tl) tl.ids.push(local.id)
    if (rootId && threads.value[rootId]) threads.value[rootId].push(local.id)
    try {
      const stored = await api<Post>('POST', '/posts', {
        channel_id: channelId,
        root_id: rootId,
        message,
        pending_post_id: pendingId,
      })
      applyPost(stored)
      const ch = channels.value[channelId]
      if (ch) ch.msg_count = ch.total_msg_count = Math.max(ch.total_msg_count, ch.msg_count + 1)
    } catch (e) {
      const cur = posts.value[local.id]
      if (cur) cur.local = 'failed'
      throw e
    }
  }

  /** retry sends a failed message again with the same key (stored at most once). */
  async function retry(localId: string): Promise<void> {
    const p = posts.value[localId]
    if (!p || p.local !== 'failed' || !p.pending_post_id) return
    dropLocal(p)
    await send(p.channel_id, p.message ?? '', p.root_id, p.pending_post_id)
  }

  async function edit(id: string, message: string): Promise<void> {
    applyPost(await api<Post>('PATCH', `/posts/${id}`, { message }))
  }

  async function remove(id: string): Promise<void> {
    await api('DELETE', `/posts/${id}`)
  }

  async function react(postId: string, emoji: string, add: boolean): Promise<void> {
    if (add) await api('POST', '/reactions', { post_id: postId, emoji_name: emoji })
    else await api('DELETE', `/users/me/posts/${postId}/reactions/${encodeURIComponent(emoji)}`)
  }

  function sendTyping(channelId: string, parentId = ''): void {
    socket?.send('user_typing', { channel_id: channelId, parent_id: parentId }).catch(() => undefined)
  }

  function sendActivity(): void {
    socket?.send('user_activity').catch(() => undefined)
  }

  // ── events ─────────────────────────────────────────────────────────────────

  function applyReaction(data: Record<string, unknown>, add: boolean): void {
    const r = data.reaction as { post_id: string; user_id: string; emoji_name: string }
    const p = posts.value[r.post_id]
    const revision = Number(data.revision ?? 0)
    if (!p || p.delete_at || p.revision >= revision) return
    p.revision = revision
    const groups: ReactionGroup[] = (p.reactions ?? []).map((g) => ({ ...g, user_ids: [...g.user_ids] }))
    let g = groups.find((x) => x.emoji_name === r.emoji_name)
    if (add) {
      if (!g) groups.push((g = { emoji_name: r.emoji_name, count: 0, user_ids: [] }))
      g.count++
      if (!g.user_ids.includes(r.user_id) && g.user_ids.length < 25) g.user_ids.push(r.user_id)
    } else if (g) {
      g.count--
      g.user_ids = g.user_ids.filter((u) => u !== r.user_id)
    }
    p.reactions = groups.filter((x) => x.count > 0)
  }

  function apply(ev: ServerEvent): void {
    const d = ev.data
    switch (ev.event) {
      case 'posted': {
        const p = d.post as Post
        const changed = applyPost(p)
        const ch = channels.value[p.channel_id]
        // a channel of a team whose channels were never loaded: only its team's badge in the switcher can change
        if (changed && !ch && p.user_id !== me.value) void loadTeams().catch(() => undefined)
        if (!changed || !ch) break
        ch.total_msg_count++
        ch.last_post_at = p.create_at
        if (p.root_id) {
          const root = posts.value[p.root_id]
          if (root && root.revision < p.revision) {
            root.reply_count++
            root.last_reply_at = p.create_at
            root.revision = p.revision
          }
        }
        clearTyping(p.channel_id, p.user_id)
        if (p.user_id === me.value) {
          ch.msg_count = ch.total_msg_count
        } else if (openChannelId.value === p.channel_id && document.visibilityState === 'visible') {
          void markRead(p.channel_id)
        } else {
          ch.unread = ch.total_msg_count - ch.msg_count
          const mentioned = ((d.mentions as string[] | undefined) ?? []).includes(me.value)
          if (mentioned) ch.mention_count++
          void loadTeams().catch(() => undefined)
        }
        break
      }
      case 'post_edited':
        applyPost(d.post as Post)
        break
      case 'post_deleted': {
        const p = d.post as Post
        applyPost(p)
        if (d.delete_thread) {
          for (const q of Object.values(posts.value)) {
            if (q.root_id === p.id && !q.delete_at) {
              q.delete_at = p.delete_at
              q.revision = Math.max(q.revision, p.revision)
              delete q.message
              delete q.reactions
            }
          }
        } else if (p.root_id) {
          const root = posts.value[p.root_id]
          if (root && root.reply_count > 0 && root.revision < p.revision) {
            root.reply_count--
            root.revision = p.revision
          }
        }
        break
      }
      case 'reaction_added':
      case 'reaction_removed':
        applyReaction(d, ev.event === 'reaction_added')
        break
      case 'typing': {
        const ch = ev.broadcast.channel_id ?? ''
        const user = String(d.user_id)
        if (!ch || user === me.value) break
        typing.value[ch] = {
          ...typing.value[ch],
          [user]: { parent: String(d.parent_id ?? ''), until: Date.now() + TYPING_MS },
        }
        needUser(user)
        break
      }
      case 'status_change':
        statuses.value[String(d.user_id)] = d.status as Presence
        break
      case 'channel_viewed': {
        const ch = channels.value[String(d.channel_id)]
        if (ch) {
          ch.msg_count = ch.total_msg_count
          ch.unread = 0
          ch.mention_count = 0
        }
        void loadTeams().catch(() => undefined)
        break
      }
      case 'channel_updated':
        putChannel(d.channel as ChannelInfo)
        break
      case 'channel_created':
      case 'channel_deleted':
      case 'channel_restored':
      case 'user_added':
      case 'channel_member_updated': {
        const team = String(d.team_id ?? (d.channel as ChannelInfo | undefined)?.team_id ?? '')
        if (team && teamChannels.value[team]) void loadChannels(team).catch(() => undefined)
        break
      }
      case 'user_removed': {
        const channelId = String(d.channel_id)
        if (d.user_id === me.value) forgetChannel(channelId)
        break
      }
      case 'direct_added':
      case 'group_added':
        void loadConversations().catch(() => undefined)
        break
      case 'added_to_team':
      case 'update_team':
      case 'restore_team':
      case 'user_role_updated':
        void loadTeams().catch(() => undefined)
        break
      case 'leave_team':
      case 'delete_team': {
        const team = String(d.team_id)
        if (ev.event === 'leave_team' && d.user_id !== me.value) break
        for (const id of teamChannels.value[team] ?? []) forgetChannel(id)
        delete teamChannels.value[team]
        void loadTeams().catch(() => undefined)
        break
      }
    }
  }

  function forgetChannel(id: string): void {
    for (const [team, ids] of Object.entries(teamChannels.value)) teamChannels.value[team] = ids.filter((x) => x !== id)
    conversationIds.value = conversationIds.value.filter((x) => x !== id)
    for (const pid of timelines.value[id]?.ids ?? []) delete posts.value[pid]
    delete timelines.value[id]
    delete channels.value[id]
    if (openChannelId.value === id) lostChannel.value = id
  }

  function clearTyping(channelId: string, userId: string): void {
    const ch = typing.value[channelId]
    if (ch && ch[userId]) {
      const { [userId]: _, ...rest } = ch
      typing.value[channelId] = rest
    }
  }

  function expireTyping(): void {
    const now = Date.now()
    for (const [ch, who] of Object.entries(typing.value)) {
      for (const [u, v] of Object.entries(who)) if (v.until <= now) clearTyping(ch, u)
    }
  }

  // ── selectors ──────────────────────────────────────────────────────────────

  /** roots of a channel in order (replies live in the thread panel) */
  function rootsOf(channelId: string): LocalPost[] {
    return (timelines.value[channelId]?.ids ?? [])
      .map((id) => posts.value[id])
      .filter((p): p is LocalPost => !!p && !p.root_id)
  }

  function threadOf(rootId: string): LocalPost[] {
    return (threads.value[rootId] ?? []).map((id) => posts.value[id]).filter((p): p is LocalPost => !!p)
  }

  function typingIn(channelId: string, parentId = ''): string[] {
    return Object.entries(typing.value[channelId] ?? {})
      .filter(([, v]) => v.parent === parentId)
      .map(([u]) => u)
  }

  /** conversationTitle names a direct or group conversation by its other members */
  function conversationTitle(c: ChatChannel): string {
    const others = (c.members ?? []).filter((m) => m.id !== me.value)
    if (others.length === 0) return displayName(me.value) || (c.members?.[0]?.display_name ?? '')
    return others.map((m) => m.display_name || m.username).join(', ')
  }

  /** the other person of a direct conversation ('' for groups) */
  function directPeer(c: ChatChannel): string {
    if (c.type !== 'D') return ''
    return (c.members ?? []).find((m) => m.id !== me.value)?.id ?? me.value
  }

  function displayName(id: string): string {
    const u = users.value[id]
    return u ? u.display_name || u.username : ''
  }

  return {
    teams,
    channels,
    teamChannels,
    conversationIds,
    posts,
    timelines,
    threads,
    users,
    statuses,
    typing,
    socketState,
    openChannelId,
    lostChannel,
    threadRoot,
    currentTeamId,
    start,
    reset,
    apply,
    applyPost,
    rememberUsers,
    needUser,
    loadStatuses,
    loadTeams,
    loadChannels,
    loadConversations,
    loadChannel,
    openChannel,
    loadOlder,
    openThread,
    catchUp,
    resync,
    markRead,
    send,
    retry,
    edit,
    remove,
    react,
    sendTyping,
    sendActivity,
    rootsOf,
    threadOf,
    typingIn,
    displayName,
    conversationTitle,
    directPeer,
  }
})

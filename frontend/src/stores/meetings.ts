// Meetings (meetings and meeting-room-ui specs): whether LiveKit is configured, who is in each room of the user's
// channels (GET /meetings, kept live by meeting_updated and reloaded on every resync), and which room's full-screen
// call is open. The LiveKit connection itself lives in components/meetings/call.ts.
import { defineStore } from 'pinia'
import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { ServerEvent } from '@/chat/types'
import { useChat } from '@/stores/chat'
import { useSession } from '@/stores/session'

export interface RoomPerson {
  user_id: string
  joined_at: number
}

export interface MeetingRoom {
  channel_id: string
  team_id: string
  revision: number
  participants: RoomPerson[]
}

export interface MeetingToken {
  url: string
  token: string
  expires_at: number
}

export const useMeetings = defineStore('meetings', () => {
  const chat = useChat()
  const session = useSession()
  const enabled = ref(false)
  /** rooms with people in them, by channel id */
  const rooms = ref<Record<string, MeetingRoom>>({})
  /** the highest revision seen per room (also of rooms that emptied): older snapshots are ignored */
  const revisions = new Map<string, number>()
  /** the channel whose call is open full screen ('' when none) */
  const callChannel = ref('')
  let epoch = 0
  let loads = 0
  /** events that arrive while a load is in flight, replayed over its answer */
  let pending: ServerEvent[] | null = null

  function put(room: MeetingRoom): void {
    const id = room.channel_id
    if ((revisions.get(id) ?? -1) > room.revision) return
    revisions.set(id, room.revision)
    if (room.participants.length === 0) delete rooms.value[id]
    else rooms.value[id] = room
    for (const p of room.participants) chat.needUser(p.user_id)
  }

  async function load(): Promise<void> {
    const e = epoch
    const n = ++loads
    pending ??= []
    try {
      const r = await api<{ enabled: boolean; rooms: MeetingRoom[] }>('GET', '/meetings')
      if (e !== epoch || n !== loads) return // reset, or a newer load answers
      enabled.value = r.enabled
      const listed = new Set(r.rooms.map((x) => x.channel_id))
      for (const id of Object.keys(rooms.value)) if (!listed.has(id)) delete rooms.value[id]
      for (const x of r.rooms) put(x)
      const queued = pending ?? []
      pending = null // replayed events must not queue again
      for (const ev of queued) apply(ev)
    } finally {
      if (n === loads) pending = null
    }
  }

  function forget(match: (room: MeetingRoom) => boolean): void {
    for (const room of Object.values(rooms.value)) if (match(room)) delete rooms.value[room.channel_id]
  }

  function apply(ev: ServerEvent): void {
    const d = ev.data
    const me = session.profile?.user.id
    switch (ev.event) {
      case 'meeting_updated': {
        const id = String(d.channel_id ?? '')
        if (!id) return
        if (pending) pending.push(ev)
        put({
          channel_id: id,
          team_id: ev.broadcast.team_id ?? rooms.value[id]?.team_id ?? chat.channels[id]?.team_id ?? '',
          revision: Number(d.revision ?? 0),
          participants: (d.participants as RoomPerson[] | undefined) ?? [],
        })
        break
      }
      // the user lost access: their copy of the room goes too (no more updates will come)
      case 'user_removed':
        if (d.user_id === me) forget((r) => r.channel_id === d.channel_id)
        break
      case 'channel_deleted':
        forget((r) => r.channel_id === d.channel_id)
        break
      case 'leave_team':
      case 'delete_team':
        if (ev.event === 'delete_team' || d.user_id === me) forget((r) => r.team_id === d.team_id)
        break
    }
  }

  function people(channelId: string): RoomPerson[] {
    return rooms.value[channelId]?.participants ?? []
  }

  /** active rooms, most people first */
  const active = computed(() =>
    Object.values(rooms.value).sort((a, b) => b.participants.length - a.participants.length),
  )

  function token(channelId: string): Promise<MeetingToken> {
    return api<MeetingToken>('POST', `/channels/${channelId}/meeting/token`)
  }

  function openCall(channelId: string): void {
    callChannel.value = channelId
  }

  function closeCall(): void {
    callChannel.value = ''
  }

  function reset(): void {
    epoch++
    pending = null
    enabled.value = false
    rooms.value = {}
    revisions.clear()
    callChannel.value = ''
  }

  chat.hooks.add({ event: apply, resync: () => void load().catch(() => undefined) })
  // a session ending (logout, expiry) or another user logging in must not keep the previous user's rooms
  watch(
    () => session.profile?.user.id,
    (id, prev) => {
      if (prev && id !== prev) reset()
    },
  )

  return { enabled, rooms, callChannel, active, load, apply, people, token, openCall, closeCall, reset }
})

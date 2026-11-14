// Meetings (meetings and meeting-room-ui specs): whether LiveKit is configured, who is in each room of the user's
// channels (GET /meetings, kept live by meeting_updated and reloaded on every resync), and which room's full-screen
// call is open. The LiveKit connection itself lives in components/meetings/call.ts.
import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
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
  /** the channel whose call is open full screen ('' when none) */
  const callChannel = ref('')
  let epoch = 0

  async function load(): Promise<void> {
    const e = epoch
    const r = await api<{ enabled: boolean; rooms: MeetingRoom[] }>('GET', '/meetings')
    if (e !== epoch) return
    enabled.value = r.enabled
    rooms.value = Object.fromEntries(r.rooms.map((x) => [x.channel_id, x]))
    for (const x of r.rooms) for (const p of x.participants) chat.needUser(p.user_id)
  }

  function apply(ev: ServerEvent): void {
    if (ev.event !== 'meeting_updated') return
    const id = String(ev.data.channel_id ?? '')
    const people = (ev.data.participants as RoomPerson[] | undefined) ?? []
    if (!id) return
    if (people.length === 0) {
      delete rooms.value[id]
      return
    }
    const team = ev.broadcast.team_id ?? rooms.value[id]?.team_id ?? chat.channels[id]?.team_id ?? ''
    rooms.value[id] = { channel_id: id, team_id: team, participants: people }
    for (const p of people) chat.needUser(p.user_id)
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
    enabled.value = false
    rooms.value = {}
    callChannel.value = ''
  }

  chat.hooks.add({ event: apply, resync: () => void load().catch(() => undefined) })
  session.$onAction(({ name, after }) => {
    if (name === 'clear') after(() => reset())
  })

  return { enabled, rooms, callChannel, active, load, apply, people, token, openCall, closeCall, reset }
})

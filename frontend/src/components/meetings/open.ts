// openMeeting shows a channel (its chat stays under the call) and opens its room's pre-join screen.
import type { Router } from 'vue-router'
import { channelPath } from '@/chat/location'
import { useChat } from '@/stores/chat'
import { useMeetings } from '@/stores/meetings'

export async function openMeeting(router: Router, channelId: string): Promise<void> {
  const chat = useChat()
  const ch = chat.channels[channelId]
  const team = ch && chat.teams.find((x) => x.id === ch.team_id)
  const path = ch && team ? channelPath(team.name, ch.name) : `/messages/${channelId}`
  if (chat.openChannelId !== channelId) await router.push(path)
  useMeetings().openCall(channelId)
}

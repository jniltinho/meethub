// Opening a channel at a given message (a search result, a pinned message): the store loads the window around it
// and the route follows when another channel is showing. A reply lives in its thread, so the channel opens at the
// thread's root and the reply is highlighted in the thread panel.
import type { Router } from 'vue-router'
import { channelPath } from '@/chat/location'
import type { useChat } from '@/stores/chat'

export async function jumpToPost(
  chat: ReturnType<typeof useChat>,
  router: Router,
  channelId: string,
  postId: string,
  rootId = '',
): Promise<void> {
  await chat.jumpTo(channelId, rootId || postId)
  const ch = chat.channels[channelId]
  if (!ch) return
  let path = `/messages/${ch.id}`
  if (ch.type === 'O' || ch.type === 'P') {
    if (chat.teams.length === 0) await chat.loadTeams()
    const team = chat.teams.find((x) => x.id === ch.team_id)
    if (!team) return
    path = channelPath(team.name, ch.name)
  }
  if (router.currentRoute.value.path !== path) await router.push(path)
  if (rootId) {
    await chat.openThread(rootId)
    chat.highlight = postId
  }
}

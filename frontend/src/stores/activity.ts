// Activity (activity spec): the caller's unseen count, shown on the Activity entry of the navigation, kept live by
// activity_updated and reloaded on every resync. The list itself is the Activity page's (views/ActivityView.vue).
import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { api } from '@/api/client'
import type { Page, Post, ServerEvent } from '@/chat/types'
import { useChat } from '@/stores/chat'
import { useSession } from '@/stores/session'

export interface ActivityItem {
  kind: 'mention' | 'reply'
  create_at: number
  post: Post
  channel: { id: string; team_id: string; type: string; name: string; display_name: string }
}

export interface ActivityPage extends Page<ActivityItem> {
  unseen: number
}

export const useActivity = defineStore('activity', () => {
  const chat = useChat()
  const session = useSession()
  const unseen = ref(0)
  let epoch = 0
  let loads = 0

  /** load reads the count (one item's page); an older answer never replaces a newer one or an event */
  async function load(): Promise<void> {
    const mine = ++loads
    const e = epoch
    const page = await api<ActivityPage>('GET', '/users/me/activity?per_page=1')
    if (mine === loads && e === epoch) unseen.value = page.unseen
  }

  /** seen marks everything seen (the server tells the other tabs) */
  async function seen(): Promise<void> {
    unseen.value = 0
    loads++ // a load in flight answers with the count from before
    await api('POST', '/users/me/activity/seen', {})
  }

  // events of concurrent changes may arrive out of order: the count is read again once they settle
  let settle: ReturnType<typeof setTimeout> | undefined
  function apply(ev: ServerEvent): void {
    if (ev.event !== 'activity_updated') return
    loads++
    unseen.value = Number(ev.data.unseen) || 0
    clearTimeout(settle)
    settle = setTimeout(() => void load().catch(() => undefined), 1500)
  }

  function reset(): void {
    epoch++
    clearTimeout(settle)
    unseen.value = 0
  }

  chat.hooks.add({ event: apply, resync: () => void load().catch(() => undefined) })
  watch(
    () => session.profile?.user.id,
    (id, prev) => {
      if (prev && id !== prev) reset()
    },
  )

  return { unseen, load, seen, apply, reset }
})

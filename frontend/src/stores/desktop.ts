// Desktop notifications: on every posted event the rule of chat/desktop.ts decides, and the browser shows one
// notification per conversation (its tag: a newer one replaces the older); choosing it opens the message.
import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { Router } from 'vue-router'
import { postExcerpt } from '@/chat/files'
import { desktopPreference, permission, shouldNotify, type Permission } from '@/chat/desktop'
import { jumpToPost } from '@/chat/jump'
import type { Post, ServerEvent } from '@/chat/types'
import { useChat } from '@/stores/chat'
import { useSession } from '@/stores/session'
import { t } from '@/i18n'

export const useDesktop = defineStore('desktop', () => {
  const chat = useChat()
  const session = useSession()
  const state = ref<Permission>(permission())
  let router: Router | null = null

  /** ask asks the browser for permission (from Settings only) */
  async function ask(): Promise<Permission> {
    if (state.value === 'unsupported') return state.value
    state.value = await Notification.requestPermission()
    return state.value
  }

  function apply(ev: ServerEvent): void {
    if (ev.event !== 'posted' || state.value !== 'granted' || !router) return
    const p = ev.data.post as Post
    const me = session.profile?.user.id ?? ''
    const type = String(ev.data.channel_type ?? '')
    const incoming = {
      mine: p.user_id === me,
      conversation: type === 'D' || type === 'G',
      mentioned: ((ev.data.mentions as string[] | undefined) ?? []).includes(me),
      channelId: p.channel_id,
    }
    const viewing = { hidden: document.visibilityState === 'hidden', openChannelId: chat.openChannelId }
    if (!shouldNotify(desktopPreference(session.profile?.preferences.desktop_notifications), incoming, viewing)) return
    const ch = chat.channels[p.channel_id]
    const author = chat.displayName(p.user_id) || t('Someone')
    const title = incoming.conversation
      ? author
      : t('{name} in {place}', { name: author, place: '#' + (ch?.display_name ?? String(ev.data.channel_name ?? '')) })
    const n = new Notification(title, { body: postExcerpt(p, 160), tag: p.channel_id })
    const to = router
    n.onclick = () => {
      window.focus()
      n.close()
      void jumpToPost(chat, to, p.channel_id, p.id, p.root_id).catch(() => undefined)
    }
  }

  /** start listens to the chat's events (the layout calls it with its router) */
  function start(r: Router): void {
    if (router) return
    router = r
    chat.hooks.add({ event: apply })
  }

  return { state, ask, start, apply }
})

<script setup lang="ts">
// A channel (/teams/:team/channels/:channel) or conversation (/messages/:conversation, /messages/@:username):
// header (favorite star, Search, Members, "more" menu), pinned banner, timeline, typing indicator and composer.
// Opening it marks it read, remembers it as the last location, focuses the composer and announces the channel to
// screen readers.
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Archive, EllipsisVertical, Hash, Lock, Search, SearchX, Star, Users, User } from '@lucide/vue'
import EditChannelDialog from '@/components/chat/EditChannelDialog.vue'
import MessageList from '@/components/chat/MessageList.vue'
import MessageComposer from '@/components/chat/MessageComposer.vue'
import PinnedBanner from '@/components/chat/PinnedBanner.vue'
import { api, ApiError } from '@/api/client'
import type { Channel, ChannelInfo, Page, PublicUser } from '@/chat/types'
import { renderInline } from '@/chat/render'
import { channelPath, rememberLocation, rememberTeamChannel } from '@/chat/location'
import { useChat, type ChatChannel } from '@/stores/chat'
import { useSession } from '@/stores/session'
import { announce } from '@/chat/announce'
import { askConfirm } from '@/feedback/dialogs'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'

const route = useRoute()
const router = useRouter()
const chat = useChat()
const session = useSession()
const state = ref<'loading' | 'ready' | 'not-found' | 'joinable'>('loading')
const joinable = ref<ChannelInfo | null>(null)
const channelId = ref('')
const list = ref<InstanceType<typeof MessageList> | null>(null)
const composer = ref<InstanceType<typeof MessageComposer> | null>(null)
const me = computed(() => session.profile?.user.id ?? '')

const channel = computed<ChatChannel | undefined>(() => chat.channels[channelId.value])
const isConversation = computed(() => channel.value?.type === 'D' || channel.value?.type === 'G')
const title = computed(() => {
  const c = channel.value
  if (!c) return ''
  return isConversation.value ? chat.conversationTitle(c) : c.display_name
})
const team = computed(() => chat.teams.find((x) => x.id === channel.value?.team_id))
const archived = computed(() => !!channel.value?.archived_at)
const canModerate = computed(
  () => channel.value?.role === 'channel_admin' || team.value?.role === 'team_admin' || session.can('manage_system'),
)
const roots = computed(() => chat.rootsOf(channelId.value))
const complete = computed(() => chat.timelines[channelId.value]?.complete ?? false)
const typers = computed(() => chat.typingIn(channelId.value).map((u) => chat.displayName(u) || '…'))
const typingText = computed(() => {
  const n = typers.value
  if (n.length === 0) return ''
  if (n.length === 1) return t('{name} is typing…', { name: n[0]! })
  if (n.length === 2) return t('{a} and {b} are typing…', { a: n[0]!, b: n[1]! })
  return t('Several people are typing…')
})
const placeholder = computed(() =>
  isConversation.value ? t('Message {name}', { name: title.value }) : t('Message #{name}', { name: title.value }),
)

async function resolve() {
  state.value = 'loading'
  joinable.value = null
  chat.threadRoot = ''
  try {
    let id = ''
    if (route.name === 'chat-direct') {
      const username = String(route.params.username)
      const page = await api<Page<PublicUser>>('GET', `/users?per_page=20&q=${encodeURIComponent(username)}`)
      const u = page.items.find((x) => x.username === username.toLowerCase())
      if (!u) return notFound()
      const ch = await api<Channel>('POST', '/channels/direct', { user_id: u.id })
      await router.replace(`/messages/${ch.id}`)
      return
    }
    if (route.name === 'chat-conversation') {
      id = String(route.params.conversation)
    } else {
      if (chat.teams.length === 0) await chat.loadTeams()
      const tm = chat.teams.find((x) => x.name === String(route.params.team))
      if (!tm) return notFound()
      chat.currentTeamId = tm.id
      if (!chat.teamChannels[tm.id]) await chat.loadChannels(tm.id)
      const name = String(route.params.channel)
      id = (chat.teamChannels[tm.id] ?? []).find((c) => chat.channels[c]?.name === name) ?? ''
      if (!id) {
        // a public channel the user has not joined yet: offer to join
        const open = await api<Page<ChannelInfo>>('GET', `/teams/${tm.id}/channels?per_page=200`)
        joinable.value = open.items.find((c) => c.name === name) ?? null
        state.value = joinable.value ? 'joinable' : 'not-found'
        return
      }
    }
    channelId.value = id
    const ch = await chat.openChannel(id)
    if (ch.type === 'D' || ch.type === 'G') {
      if (!chat.conversationIds.includes(id)) await chat.loadConversations()
    } else {
      chat.currentTeamId = ch.team_id
    }
    state.value = 'ready'
    remember(ch)
    void chat.loadPinned(id).catch(() => undefined) // the banner; the details panel shows an error if it fails
    await nextTick()
    composer.value?.focus()
    announce(t('{name}, channel', { name: title.value }))
  } catch (e) {
    if (e instanceof ApiError && (e.status === 404 || e.status === 403)) return notFound()
    toast.error(e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.'))
    state.value = 'not-found'
  }
}

function notFound() {
  channelId.value = ''
  chat.openChannelId = ''
  state.value = 'not-found'
}

function remember(ch: ChatChannel) {
  if (ch.type === 'D' || ch.type === 'G') rememberLocation(me.value, { kind: 'conversation', channel: ch.id })
  else if (team.value) {
    rememberLocation(me.value, { kind: 'channel', team: team.value.name, channel: ch.name })
    rememberTeamChannel(me.value, team.value.name, ch.name)
  }
}

async function join() {
  const c = joinable.value
  if (!c) return
  try {
    await api('POST', `/channels/${c.id}/members`, {})
    await chat.loadChannels(c.team_id)
    await resolve()
  } catch (e) {
    toast.error(e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.'))
  }
}

watch(() => route.fullPath, resolve, { immediate: true })

// the open channel disappeared for this user (removed, team left or archived)
watch(
  () => chat.lostChannel,
  (id) => {
    if (id && id === channelId.value) {
      toast.warning(t('You no longer have access to this channel.'))
      notFound()
    }
  },
)

// new messages of others are announced politely to screen readers
watch(
  () => roots.value.at(-1)?.id,
  (id, prev) => {
    const p = roots.value.at(-1)
    if (!id || !prev || id === prev || !p || p.user_id === me.value || p.local) return
    announce(t('New message from {name}', { name: chat.displayName(p.user_id) || t('Someone') }))
  },
)

// ── header actions ───────────────────────────────────────────────────────────
const menuOpen = ref(false)
const editing = ref(false)
const menu = ref<HTMLElement | null>(null)
const isAdmin = computed(() => canModerate.value && !isConversation.value)
const detached = computed(() => !!chat.timelines[channelId.value]?.newer)

function fail(e: unknown) {
  toast.error(e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.'))
}

async function toggleFavorite() {
  const c = channel.value
  if (!c) return
  try {
    await chat.setFavorite(c.id, !c.favorite)
  } catch (e) {
    fail(e)
  }
}

function showSearch() {
  chat.threadRoot = ''
  chat.searchOpen = true
}

/** the Members button opens the details panel at the participants */
function showMembers() {
  menuOpen.value = false
  chat.showDetails()
  setTimeout(() => document.getElementById('details-members')?.focus(), 80)
}

async function leave() {
  menuOpen.value = false
  const c = channel.value
  if (
    !c ||
    !(await askConfirm(t('Leave {name}? You can join again later.', { name: title.value }), { ok: t('Leave') }))
  )
    return
  try {
    await api('DELETE', `/channels/${c.id}/members/${me.value}`)
    toast.success(t('You left {name}.', { name: title.value }))
    await router.push('/conversations')
  } catch (e) {
    fail(e)
  }
}

async function archive() {
  menuOpen.value = false
  const c = channel.value
  const sure = await askConfirm(
    t('Archive {name}? It can be read but not changed until it is restored.', { name: title.value }),
    { ok: t('Archive'), danger: true },
  )
  if (!c || !sure) return
  try {
    await api('POST', `/channels/${c.id}/archive`)
    await chat.loadChannel(c.id)
    toast.success(t('Channel archived.'))
  } catch (e) {
    fail(e)
  }
}

function outside(e: Event) {
  if (menuOpen.value && menu.value && !menu.value.contains(e.target as Node)) menuOpen.value = false
}

async function openThread(id: string) {
  try {
    await chat.openThread(id)
  } catch (e) {
    toast.error(e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.'))
  }
}

function editLast() {
  list.value?.editLast(me.value)
}

// Alt+Up / Alt+Down: previous / next channel of the sidebar, focus in its composer
function onKey(e: KeyboardEvent) {
  if (!e.altKey || (e.key !== 'ArrowUp' && e.key !== 'ArrowDown')) return
  const links = [...document.querySelectorAll<HTMLAnchorElement>('[data-chat-channel]')]
  if (links.length === 0) return
  e.preventDefault()
  const cur = links.findIndex((a) => a.getAttribute('aria-current') === 'page')
  const next = links[(cur + (e.key === 'ArrowDown' ? 1 : -1) + links.length) % links.length]
  if (next) void router.push(next.getAttribute('href') ?? '/')
}
onMounted(() => {
  window.addEventListener('keydown', onKey)
  document.addEventListener('pointerdown', outside)
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKey)
  document.removeEventListener('pointerdown', outside)
  chat.searchOpen = false
  chat.openChannelId = ''
  chat.threadRoot = ''
})

const header = computed(() => (channel.value?.header ? renderInline(channel.value.header) : ''))
const purpose = computed(() => (channel.value?.purpose ? renderInline(channel.value.purpose) : ''))
</script>

<template>
  <section v-if="state === 'not-found'" class="empty-state">
    <SearchX :size="40" aria-hidden="true" />
    <h1 class="page-title">{{ t('Channel not found') }}</h1>
    <p>{{ t('It does not exist or you do not have access to it.') }}</p>
    <RouterLink to="/conversations" class="btn">{{ t('Back to conversations') }}</RouterLink>
  </section>

  <section v-else-if="state === 'joinable' && joinable" class="empty-state">
    <Hash :size="40" aria-hidden="true" />
    <h1 class="page-title">{{ joinable.display_name }}</h1>
    <p>{{ joinable.purpose || t('{n} members', { n: joinable.member_count }) }}</p>
    <button type="button" class="btn" @click="join">{{ t('Join channel') }}</button>
  </section>

  <section v-else-if="channel" class="channel-view" :aria-busy="state === 'loading'">
    <header class="channel-head">
      <div class="min-w-0 flex-1">
        <h1 class="channel-title">
          <User v-if="channel.type === 'D'" :size="18" aria-hidden="true" />
          <Users v-else-if="channel.type === 'G'" :size="18" aria-hidden="true" />
          <Lock v-else-if="channel.type === 'P'" :size="18" aria-hidden="true" />
          <Hash v-else :size="18" aria-hidden="true" />
          <span class="truncate">{{ title }}</span>
          <button
            type="button"
            class="icon-btn star"
            :class="{ on: channel.favorite }"
            :aria-pressed="!!channel.favorite"
            :aria-label="channel.favorite ? t('Remove from favorites') : t('Add to favorites')"
            :title="channel.favorite ? t('Remove from favorites') : t('Add to favorites')"
            @click="toggleFavorite"
          >
            <Star :size="16" aria-hidden="true" :fill="channel.favorite ? 'currentColor' : 'none'" />
          </button>
        </h1>
        <!-- eslint-disable-next-line vue/no-v-html -- sanitized by the fixed pipeline of chat/render.ts -->
        <p v-if="purpose" class="channel-purpose" v-html="purpose" />
      </div>
      <div class="channel-actions">
        <button type="button" class="btn-ghost" @click="showSearch">
          <Search :size="15" aria-hidden="true" /><span class="max-xl:sr-only">{{ t('Search') }}</span>
        </button>
        <button type="button" class="btn-ghost channel-members" @click="showMembers">
          <Users :size="15" aria-hidden="true" />
          <span class="max-xl:sr-only">{{ t('Members ({n})', { n: channel.member_count }) }}</span>
          <span class="xl:hidden" aria-hidden="true">{{ channel.member_count }}</span>
        </button>
        <div ref="menu" class="relative" @keydown.esc="menuOpen = false">
          <button
            type="button"
            class="icon-btn"
            :aria-label="t('More actions')"
            :title="t('More actions')"
            :aria-expanded="menuOpen"
            aria-haspopup="menu"
            @click="menuOpen = !menuOpen"
          >
            <EllipsisVertical :size="16" aria-hidden="true" />
          </button>
          <ul v-if="menuOpen" class="select-pop channel-menu" role="menu" :aria-label="t('More actions')">
            <li role="none">
              <button type="button" role="menuitem" @click="showMembers">{{ t('Channel details') }}</button>
            </li>
            <li v-if="isAdmin && !archived" role="none">
              <button type="button" role="menuitem" @click="((menuOpen = false), (editing = true))">
                {{ t('Edit channel') }}
              </button>
            </li>
            <li v-if="!isConversation && !channel.is_default" role="none">
              <button type="button" role="menuitem" @click="leave">{{ t('Leave channel') }}</button>
            </li>
            <li v-if="isAdmin && !archived && !channel.is_default" role="none">
              <button type="button" role="menuitem" @click="archive">{{ t('Archive channel') }}</button>
            </li>
          </ul>
        </div>
      </div>
    </header>
    <!-- eslint-disable-next-line vue/no-v-html -- sanitized by the fixed pipeline of chat/render.ts -->
    <div v-if="header" class="channel-header-text" v-html="header" />
    <p v-if="archived" class="channel-archived" role="status">
      <Archive :size="14" aria-hidden="true" />{{ t('This channel is archived: it can be read but not changed.') }}
    </p>

    <PinnedBanner :channel-id="channelId" />

    <MessageList
      ref="list"
      :key="channelId"
      :posts="roots"
      :complete="complete"
      :can-moderate="canModerate"
      :readonly="archived"
      :detached="detached"
      :highlight="chat.highlight"
      :label="t('Messages in {name}', { name: title })"
      @older="chat.loadOlder(channelId)"
      @newer="chat.loadNewer(channelId)"
      @recent="chat.jumpToRecent(channelId).catch(fail)"
      @seen="chat.highlight = ''"
      @reply="openThread"
    />

    <p class="typing" aria-live="off">{{ typingText }}</p>
    <MessageComposer
      v-if="!archived"
      ref="composer"
      :key="'c' + channelId"
      :channel-id="channelId"
      :placeholder="placeholder"
      @edit-last="editLast"
    />
    <EditChannelDialog v-if="editing" :channel="channel" @close="editing = false" />
  </section>

  <section v-else class="empty-state" aria-busy="true">
    <p>{{ t('Loading…') }}</p>
  </section>
</template>

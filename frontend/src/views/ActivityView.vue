<script setup lang="ts">
// The Activity page (chat-ui spec "Activity page"): the mentions of the user and the replies in their threads, newest
// first, loading more on scroll; each one opens its channel at the message (in its thread for a reply). Opening the
// page marks everything seen, which clears the badge of the navigation here and in the other tabs.
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { AtSign, MessageSquareReply } from '@lucide/vue'
import UserPicture from '@/components/chat/UserPicture.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import { api, ApiError } from '@/api/client'
import { postExcerpt } from '@/chat/files'
import { formatDay, formatTime } from '@/chat/format'
import { jumpToPost } from '@/chat/jump'
import { useActivity, type ActivityItem, type ActivityPage } from '@/stores/activity'
import { useChat } from '@/stores/chat'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'

const chat = useChat()
const activity = useActivity()
const router = useRouter()
const items = ref<ActivityItem[]>([])
const next = ref('')
const state = ref<'loading' | 'ready' | 'error'>('loading')
const end = ref<HTMLElement | null>(null)
let watcher: IntersectionObserver | undefined
let loadingMore = false

async function load(more = false) {
  if (more && (loadingMore || !next.value || state.value === 'loading')) return
  loadingMore = more
  try {
    const cursor = more ? `&cursor=${encodeURIComponent(next.value)}` : ''
    const page = await api<ActivityPage>('GET', `/users/me/activity?per_page=40${cursor}`)
    items.value = more ? [...items.value, ...page.items] : page.items
    next.value = page.next_cursor ?? ''
    state.value = 'ready'
    for (const it of page.items) chat.needUser(it.post.user_id)
  } catch {
    if (!more) state.value = 'error'
  } finally {
    if (more) loadingMore = false
  }
  // a short page may leave the end in view: observing again asks once more
  await nextTick()
  if (next.value && end.value && watcher) {
    watcher.unobserve(end.value)
    watcher.observe(end.value)
  }
}

onMounted(() => {
  if (typeof IntersectionObserver !== 'undefined') {
    watcher = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) void load(true)
    })
    if (end.value) watcher.observe(end.value)
  }
  void load()
  void activity.seen().catch(() => undefined)
})
onBeforeUnmount(() => watcher?.disconnect())

function place(it: ActivityItem): string {
  const c = it.channel
  return c.type === 'D' || c.type === 'G' ? c.display_name : '#' + c.display_name
}

async function open(it: ActivityItem) {
  try {
    if (!chat.channels[it.channel.id]) await chat.loadChannel(it.channel.id)
    await jumpToPost(chat, router, it.channel.id, it.post.id, it.post.root_id)
  } catch (e) {
    toast.error(e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.'))
  }
}
</script>

<template>
  <section class="activity-page">
    <h1 class="page-title">{{ t('Activity') }}</h1>
    <UiAlert v-if="state === 'error'" kind="error">{{ t('The activity could not be loaded.') }}</UiAlert>
    <p v-else-if="state === 'ready' && items.length === 0" class="empty-state">
      {{ t('Mentions of you and replies in your threads appear here.') }}
    </p>
    <ul class="activity-list" :aria-busy="state === 'loading'" :aria-label="t('Activity')">
      <li v-for="it in items" :key="it.post.id">
        <button type="button" class="activity-item" data-activity @click="open(it)">
          <UserPicture
            :user-id="it.post.user_id"
            :name="chat.displayName(it.post.user_id) || t('Someone')"
            :size="32"
          />
          <span class="activity-body">
            <span class="activity-head">
              <strong>{{ chat.displayName(it.post.user_id) || t('Someone') }}</strong>
              <span class="activity-kind">
                <AtSign v-if="it.kind === 'mention'" :size="13" aria-hidden="true" />
                <MessageSquareReply v-else :size="13" aria-hidden="true" />
                {{
                  it.kind === 'mention'
                    ? t('mentioned you in {place}', { place: place(it) })
                    : t('replied in a thread in {place}', { place: place(it) })
                }}
              </span>
              <time :datetime="new Date(it.create_at).toISOString()">
                {{ formatDay(it.create_at) }}, {{ formatTime(it.create_at) }}
              </time>
            </span>
            <span class="activity-text">{{ postExcerpt(it.post, 160) }}</span>
          </span>
        </button>
      </li>
    </ul>
    <button v-if="next" type="button" class="btn-ghost files-more" @click="load(true)">{{ t('Load more') }}</button>
    <div ref="end" aria-hidden="true" />
  </section>
</template>

<script setup lang="ts">
// Message search in the side panel (chat-ui spec): results with channel, author, picture, time and the text with
// the matched words highlighted, newest first, more on scroll; choosing one opens its channel at that message.
import { computed, nextTick, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Search, X } from '@lucide/vue'
import UserPicture from './UserPicture.vue'
import { ApiError } from '@/api/client'
import { jumpToPost } from '@/chat/jump'
import { highlightParts, searchTerms } from '@/chat/search'
import type { Post } from '@/chat/types'
import { useChat } from '@/stores/chat'
import { announce } from '@/chat/announce'
import { locale, t } from '@/i18n'

const chat = useChat()
const router = useRouter()
const query = ref(chat.search.terms)
const input = ref<HTMLInputElement | null>(null)
const error = ref('')
const terms = computed(() => searchTerms(chat.search.terms))

onMounted(async () => {
  await nextTick()
  input.value?.focus()
})

function explain(e: unknown): string {
  if (e instanceof ApiError && e.status === 429) return t('Too many searches. Wait a moment and try again.')
  if (e instanceof ApiError && e.status === 400) return t('That search cannot be run: {reason}', { reason: e.message })
  return t('Unexpected error.')
}

async function run(more = false) {
  const q = more ? chat.search.terms : query.value.trim()
  if (!q) return
  error.value = ''
  try {
    await chat.runSearch(q, more)
    if (!more) announce(t('{n} results', { n: chat.search.posts.length }))
  } catch (e) {
    error.value = explain(e)
  }
}

function onScroll(e: Event) {
  const el = e.target as HTMLElement
  if (chat.search.hasMore && !chat.search.loading && el.scrollHeight - el.scrollTop - el.clientHeight < 120)
    void run(true)
}

function place(p: Post): string {
  const c = chat.search.channels[p.channel_id]
  if (!c) return ''
  if (c.type === 'D') return t('Direct message')
  if (c.type === 'G') return t('Group message')
  return '#' + c.display_name
}

function when(ms: number): string {
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(ms)
}

async function open(p: Post) {
  try {
    await jumpToPost(chat, router, p.channel_id, p.id, p.root_id)
  } catch (e) {
    error.value = explain(e)
  }
}
</script>

<template>
  <div class="search-panel">
    <div class="side-panel-head">
      <Search :size="16" aria-hidden="true" />
      <span class="flex-1">{{ t('Search messages') }}</span>
      <button type="button" class="icon-btn" :aria-label="t('Close search')" @click="chat.searchOpen = false">
        <X :size="16" aria-hidden="true" />
      </button>
    </div>
    <form class="search-form" role="search" @submit.prevent="run()">
      <input
        ref="input"
        v-model="query"
        class="inp"
        type="search"
        maxlength="256"
        :aria-label="t('Search messages')"
        :placeholder="t('Words, &quot;a phrase&quot;, from:username, in:channel')"
      />
      <button type="submit" class="btn" :disabled="!query.trim() || chat.search.loading">{{ t('Search') }}</button>
    </form>
    <p v-if="error" class="field-error search-note" role="alert">{{ error }}</p>
    <p v-else-if="chat.search.loading && chat.search.posts.length === 0" class="search-note" role="status">
      {{ t('Searching…') }}
    </p>
    <p v-else-if="chat.search.done && chat.search.posts.length === 0" class="search-note" role="status">
      {{ t('No messages found for “{q}”. Try other words, or the from: and in: filters.', { q: chat.search.terms }) }}
    </p>
    <ul class="search-results" :aria-label="t('Results')" @scroll.passive="onScroll">
      <li v-for="p in chat.search.posts" :key="p.id">
        <button type="button" class="search-result" data-search-result @click="open(p)">
          <UserPicture :user-id="p.user_id" :name="chat.displayName(p.user_id) || '?'" :size="28" />
          <span class="min-w-0 flex-1">
            <span class="search-meta">
              <strong class="truncate">{{ chat.displayName(p.user_id) || t('Someone') }}</strong>
              <span class="truncate">{{ place(p) }}</span>
              <time :datetime="new Date(p.create_at).toISOString()">{{ when(p.create_at) }}</time>
            </span>
            <span class="search-text">
              <template v-for="(part, i) in highlightParts((p.message ?? '').slice(0, 400), terms)" :key="i">
                <mark v-if="part.hit">{{ part.text }}</mark>
                <template v-else>{{ part.text }}</template>
              </template>
            </span>
          </span>
        </button>
      </li>
    </ul>
  </div>
</template>

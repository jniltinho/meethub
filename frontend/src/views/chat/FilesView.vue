<script setup lang="ts">
// The Files page (web-shell spec): every file of the channels and conversations the user belongs to, newest
// first, filtered by name; each one opens its message or downloads.
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Search } from '@lucide/vue'
import FileRow from '@/components/chat/FileRow.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import { api } from '@/api/client'
import type { FileInfo, Page } from '@/chat/types'
import { useChat } from '@/stores/chat'
import { t } from '@/i18n'

const chat = useChat()
const q = ref('')
const files = ref<FileInfo[]>([])
const next = ref('')
const state = ref<'loading' | 'ready' | 'error'>('loading')
let run = 0
let timer: ReturnType<typeof setTimeout> | undefined
// more files load as the end of the list scrolls into view (the button stays for the keyboard)
const end = ref<HTMLElement | null>(null)
let watcher: IntersectionObserver | undefined
let loadingMore = false

async function load(more = false) {
  // no paging while the results are being replaced: the cursor belongs to the previous filter
  if (more && (loadingMore || !next.value || state.value === 'loading')) return
  loadingMore = more
  const mine = ++run // a slower, older answer must not replace a newer one
  if (!more) state.value = 'loading'
  try {
    const cursor = more && next.value ? `&cursor=${encodeURIComponent(next.value)}` : ''
    const page = await api<Page<FileInfo>>(
      'GET',
      `/users/me/files?per_page=40&q=${encodeURIComponent(q.value.trim())}${cursor}`,
    )
    if (mine !== run) return
    files.value = more ? [...files.value, ...page.items] : page.items
    next.value = page.next_cursor ?? ''
    state.value = 'ready'
    void namePlaces(page.items)
  } catch {
    if (mine === run) state.value = 'error'
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
// this page may be the first one opened: the channels of its files are fetched as needed (conversations come
// with their members, which name them)
const conversations = chat.loadConversations().catch(() => undefined)
async function namePlaces(items: FileInfo[]) {
  await conversations
  for (const id of new Set(items.map((f) => f.channel_id)))
    if (!chat.channels[id]) void chat.loadChannel(id).catch(() => undefined)
}
onMounted(() => {
  if (typeof IntersectionObserver !== 'undefined') {
    watcher = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) void load(true)
    })
    if (end.value) watcher.observe(end.value)
  }
  void load()
})
onBeforeUnmount(() => watcher?.disconnect())
watch(q, () => {
  state.value = 'loading' // from now on the list on screen is the previous filter's
  next.value = ''
  clearTimeout(timer)
  timer = setTimeout(() => void load(), 250)
})
// a message with files arrived, or one was deleted, in any of my channels
watch(
  () => chat.fileChanges[''],
  () => {
    if (state.value !== 'loading') void load()
  },
)

function place(f: FileInfo): string {
  const c = chat.channels[f.channel_id]
  if (!c) return ''
  return c.type === 'D' || c.type === 'G' ? chat.conversationTitle(c) : '#' + c.display_name
}
</script>

<template>
  <section class="files-page">
    <h1 class="page-title">{{ t('Files') }}</h1>
    <label class="files-filter">
      <Search :size="15" aria-hidden="true" />
      <input
        v-model="q"
        class="inp"
        type="search"
        :placeholder="t('Filter by name')"
        :aria-label="t('Filter by name')"
      />
    </label>
    <UiAlert v-if="state === 'error'" kind="error">{{ t('The files could not be loaded.') }}</UiAlert>
    <p v-else-if="state === 'ready' && files.length === 0" class="empty-state">
      {{ q.trim() ? t('No file matches that name.') : t('Files shared in your conversations appear here.') }}
    </p>
    <ul class="files-list" :aria-busy="state === 'loading'" :aria-label="t('Files')">
      <FileRow v-for="f in files" :key="f.id" :file="f" :place="place(f)" />
    </ul>
    <button v-if="next" type="button" class="btn-ghost files-more" @click="load(true)">{{ t('Load more') }}</button>
    <div ref="end" aria-hidden="true" />
  </section>
</template>

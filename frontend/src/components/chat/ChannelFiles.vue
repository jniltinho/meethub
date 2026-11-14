<script setup lang="ts">
// The open channel's files in the side panel (chat-ui spec): newest first, more on demand; each opens its message.
import { onMounted, ref, watch } from 'vue'
import { Paperclip, X } from '@lucide/vue'
import FileRow from './FileRow.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import { api } from '@/api/client'
import type { FileInfo, Page } from '@/chat/types'
import { useChat } from '@/stores/chat'
import { t } from '@/i18n'

const chat = useChat()
const id = chat.openChannelId
const files = ref<FileInfo[]>([])
const next = ref('')
const state = ref<'loading' | 'ready' | 'error'>('loading')

let run = 0
async function load(more = false) {
  const mine = ++run // an older answer must not replace a newer one
  try {
    const cursor = more && next.value ? `&cursor=${encodeURIComponent(next.value)}` : ''
    const page = await api<Page<FileInfo>>('GET', `/channels/${id}/files?per_page=30${cursor}`)
    if (mine !== run) return
    files.value = more ? [...files.value, ...page.items] : page.items
    next.value = page.next_cursor ?? ''
    state.value = 'ready'
  } catch {
    if (mine === run) state.value = 'error'
  }
}
onMounted(load)
// a message with files arrived, or one was deleted
watch(
  () => chat.fileChanges[id],
  () => void load(),
)
</script>

<template>
  <div class="files-panel">
    <div class="side-panel-head">
      <Paperclip :size="16" aria-hidden="true" />
      <span class="flex-1">{{ t('Files') }}</span>
      <button type="button" class="icon-btn" :aria-label="t('Close files')" @click="chat.filesOpen = false">
        <X :size="16" aria-hidden="true" />
      </button>
    </div>
    <div class="files-list" :aria-busy="state === 'loading'">
      <UiAlert v-if="state === 'error'" kind="error">{{ t('The files could not be loaded.') }}</UiAlert>
      <p v-else-if="state === 'ready' && files.length === 0" class="sidebar-empty">
        {{ t('No files in this conversation yet.') }}
      </p>
      <ul>
        <FileRow v-for="f in files" :key="f.id" :file="f" />
      </ul>
      <button v-if="next" type="button" class="btn-ghost files-more" @click="load(true)">
        {{ t('Load more') }}
      </button>
    </div>
  </div>
</template>

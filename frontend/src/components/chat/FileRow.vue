<script setup lang="ts">
// One file of a list (the channel's files, the Files page): what it is, who shared it and when, with its message
// and its download one click away.
import { computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { Download, MessageSquare } from '@lucide/vue'
import FileIcon from './FileIcon.vue'
import { ApiError } from '@/api/client'
import { fileUrl, jumpToFile } from '@/chat/files'
import { formatSize } from '@/chat/format'
import type { FileInfo } from '@/chat/types'
import { useChat } from '@/stores/chat'
import { toast } from '@/feedback/toasts'
import { errorMessage, locale, t } from '@/i18n'

const props = defineProps<{ file: FileInfo; place?: string }>()
const chat = useChat()
const router = useRouter()
const when = computed(() =>
  new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(props.file.create_at),
)
const who = computed(() => chat.displayName(props.file.user_id) || t('Someone'))
onMounted(() => chat.needUser(props.file.user_id))

async function goTo() {
  try {
    await jumpToFile(chat, router, props.file)
  } catch (e) {
    toast.error(e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.'))
  }
}
</script>

<template>
  <li class="file-row" data-file>
    <img
      v-if="file.has_preview_image"
      class="file-row-thumb"
      :src="fileUrl(file.id, 'thumbnail')"
      alt=""
      loading="lazy"
      width="36"
      height="36"
    />
    <FileIcon v-else :name="file.name" :size="24" />
    <button type="button" class="file-text" :title="t('Go to message')" @click="goTo">
      <span class="file-name">{{ file.name }}</span>
      <span class="file-meta">
        <template v-if="place">{{ place }} · </template>{{ who }} · {{ when }} · {{ formatSize(file.size) }}
      </span>
    </button>
    <button type="button" class="icon-btn" :aria-label="t('Go to message')" :title="t('Go to message')" @click="goTo">
      <MessageSquare :size="15" aria-hidden="true" />
    </button>
    <a
      class="icon-btn"
      :href="fileUrl(file.id)"
      :download="file.name"
      :aria-label="t('Download {name}', { name: file.name })"
      :title="t('Download {name}', { name: file.name })"
    >
      <Download :size="15" aria-hidden="true" />
    </a>
  </li>
</template>

<script setup lang="ts">
// /pl/:post opens a message where it lives (the links of notification e-mails): its channel at the message, or its
// thread for a reply. The thread answers with its root first, which names the channel.
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import UiAlert from '@/components/ui/UiAlert.vue'
import { api } from '@/api/client'
import { jumpToPost } from '@/chat/jump'
import type { Page, Post } from '@/chat/types'
import { useChat } from '@/stores/chat'
import { t } from '@/i18n'

const route = useRoute()
const router = useRouter()
const chat = useChat()
const failed = ref(false)

onMounted(async () => {
  const id = String(route.params.post)
  try {
    const page = await api<Page<Post>>('GET', `/posts/${encodeURIComponent(id)}/thread?per_page=1`)
    const root = page.items[0]
    if (!root) throw new Error('no message')
    if (!chat.channels[root.channel_id]) await chat.loadChannel(root.channel_id)
    await jumpToPost(chat, router, root.channel_id, id, root.id === id ? '' : root.id)
  } catch {
    failed.value = true
  }
})
</script>

<template>
  <section class="files-page">
    <h1 class="page-title">{{ t('Message') }}</h1>
    <UiAlert v-if="failed" kind="error">{{ t('This message does not exist or you cannot read it.') }}</UiAlert>
  </section>
</template>

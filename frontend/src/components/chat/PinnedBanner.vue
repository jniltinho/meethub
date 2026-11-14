<script setup lang="ts">
// The banner above the timeline with the most recently pinned message (chat-ui spec): who pinned it, the start of
// the message, a jump to it and a dismissal kept in this browser until another message is pinned.
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Pin, X } from '@lucide/vue'
import { jumpToPost } from '@/chat/jump'
import { useChat } from '@/stores/chat'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const props = defineProps<{ channelId: string }>()
const chat = useChat()
const router = useRouter()
const KEY = 'meethub.chat.pins.dismissed'

function read(): Record<string, number> {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) ?? '{}') as unknown
    return v && typeof v === 'object' ? (v as Record<string, number>) : {}
  } catch {
    return {}
  }
}
const dismissed = ref(read())
const post = computed(() => chat.posts[chat.pinned[props.channelId]?.[0] ?? ''])
const show = computed(() => !!post.value && (post.value.pinned_at ?? 0) > (dismissed.value[props.channelId] ?? 0))
const by = computed(() => chat.displayName(post.value?.pinned_by ?? '') || t('Someone'))
const excerpt = computed(() => (post.value?.message ?? '').split('\n')[0]!.slice(0, 160))

function dismiss() {
  dismissed.value = { ...dismissed.value, [props.channelId]: post.value?.pinned_at ?? 0 }
  try {
    localStorage.setItem(KEY, JSON.stringify(dismissed.value))
  } catch {
    /* storage unavailable: dismissed until the page reloads */
  }
}

async function jump() {
  if (!post.value) return
  try {
    await jumpToPost(chat, router, props.channelId, post.value.id, post.value.root_id)
  } catch {
    toast.error(t('Unexpected error.'))
  }
}
</script>

<template>
  <div v-if="show && post" class="pinned-banner" role="region" :aria-label="t('Pinned message')">
    <Pin :size="15" aria-hidden="true" />
    <button type="button" class="pinned-banner-text" :title="t('Go to the pinned message')" @click="jump">
      <strong>{{ t('Pinned by {name}', { name: by }) }}</strong>
      <span class="truncate">{{ excerpt }}</span>
    </button>
    <button type="button" class="icon-btn" :aria-label="t('Dismiss')" :title="t('Dismiss')" @click="dismiss">
      <X :size="15" aria-hidden="true" />
    </button>
  </div>
</template>

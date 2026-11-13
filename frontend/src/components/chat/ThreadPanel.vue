<script setup lang="ts">
// The thread in the side panel (320 px; full screen on phones): root, replies and their own composer, live.
import { computed, nextTick, onMounted, ref } from 'vue'
import { X } from '@lucide/vue'
import MessageList from './MessageList.vue'
import MessageComposer from './MessageComposer.vue'
import { useChat } from '@/stores/chat'
import { useSession } from '@/stores/session'
import { t } from '@/i18n'

const chat = useChat()
const session = useSession()
const list = ref<InstanceType<typeof MessageList> | null>(null)
const composer = ref<InstanceType<typeof MessageComposer> | null>(null)
const root = computed(() => chat.posts[chat.threadRoot])
const channel = computed(() => (root.value ? chat.channels[root.value.channel_id] : undefined))
const posts = computed(() => (root.value ? [root.value, ...chat.threadOf(root.value.id)] : []))
const typers = computed(() =>
  root.value ? chat.typingIn(root.value.channel_id, root.value.id).map((u) => chat.displayName(u) || '…') : [],
)
const archived = computed(() => !!channel.value?.archived_at)
const canModerate = computed(() => channel.value?.role === 'channel_admin' || session.can('manage_system'))

onMounted(async () => {
  await nextTick()
  composer.value?.focus()
})
</script>

<template>
  <div class="thread-panel">
    <div class="side-panel-head">
      <h2 class="flex-1">{{ t('Thread') }}</h2>
      <button type="button" class="icon-btn" :aria-label="t('Close thread')" @click="chat.threadRoot = ''">
        <X :size="16" aria-hidden="true" />
      </button>
    </div>
    <template v-if="root">
      <MessageList
        ref="list"
        :key="root.id"
        :posts="posts"
        complete
        in-thread
        :readonly="archived"
        :can-moderate="canModerate"
        :label="t('Thread messages')"
      />
      <p class="typing" aria-live="off">
        {{ typers.length ? t('{name} is typing…', { name: typers.join(', ') }) : '' }}
      </p>
      <MessageComposer
        v-if="!root.delete_at && !archived"
        ref="composer"
        :channel-id="root.channel_id"
        :root-id="root.id"
        :placeholder="t('Reply…')"
        @edit-last="list?.editLast(session.profile?.user.id ?? '')"
      />
    </template>
    <p v-else class="sidebar-empty">{{ t('Loading…') }}</p>
  </div>
</template>

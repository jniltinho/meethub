<script setup lang="ts">
// The side panel of a call: the channel's messages, live (sending posts to the channel), and the people in the room.
import { computed, nextTick, ref, watch } from 'vue'
import { MessageSquare, SendHorizontal, X } from '@lucide/vue'
import UserPicture from '@/components/chat/UserPicture.vue'
import { formatTime } from '@/chat/format'
import { useChat } from '@/stores/chat'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'
import { tiles } from './call'

const props = defineProps<{ channelId: string; title: string; tab: 'chat' | 'people' }>()
const emit = defineEmits<{ close: []; tab: [tab: 'chat' | 'people'] }>()
const chat = useChat()
const text = ref('')
const list = ref<HTMLElement | null>(null)
const messages = computed(() => chat.rootsOf(props.channelId).slice(-100))
const people = computed(() => tiles.value.filter((x) => !x.screen))

watch(
  () => messages.value.at(-1)?.id,
  async () => {
    await nextTick()
    list.value?.scrollTo({ top: list.value.scrollHeight })
  },
  { immediate: true },
)

async function send() {
  const m = text.value.trim()
  if (!m) return
  text.value = ''
  try {
    await chat.send(props.channelId, m)
  } catch {
    toast.error(t('The message could not be sent.'))
  }
}
</script>

<template>
  <aside class="meeting-panel" :aria-label="t('Messages of {name}', { name: title })">
    <div class="meeting-panel-head">
      <MessageSquare :size="16" aria-hidden="true" />
      <h2 class="min-w-0 flex-1 truncate">{{ t('Messages of {name}', { name: title }) }}</h2>
      <button
        type="button"
        class="icon-btn"
        :aria-label="t('Close panel')"
        :title="t('Close panel')"
        @click="emit('close')"
      >
        <X :size="16" aria-hidden="true" />
      </button>
    </div>
    <div class="meeting-tabs" role="tablist">
      <button
        type="button"
        role="tab"
        :aria-selected="tab === 'chat'"
        :class="tab === 'chat' ? 'btn' : 'btn-ghost'"
        @click="emit('tab', 'chat')"
      >
        {{ t('Chat') }}
      </button>
      <button
        type="button"
        role="tab"
        :aria-selected="tab === 'people'"
        :class="tab === 'people' ? 'btn' : 'btn-ghost'"
        @click="emit('tab', 'people')"
      >
        {{ t('People ({n})', { n: people.length }) }}
      </button>
    </div>
    <template v-if="tab === 'chat'">
      <ol ref="list" class="meeting-messages" aria-live="polite">
        <li v-if="messages.length === 0" class="text-fg-muted">{{ t('No messages yet.') }}</li>
        <li v-for="p in messages" :key="p.id">
          <UserPicture :user-id="p.user_id" :name="chat.displayName(p.user_id)" :size="24" />
          <div class="min-w-0 flex-1">
            <div class="meeting-message-head">
              <strong class="truncate">{{ chat.displayName(p.user_id) || t('Someone') }}</strong>
              <time :datetime="new Date(p.create_at).toISOString()">{{ formatTime(p.create_at) }}</time>
            </div>
            <p class="meeting-message-text">{{ p.delete_at ? t('(message deleted)') : p.message }}</p>
          </div>
        </li>
      </ol>
      <form class="meeting-send" @submit.prevent="send">
        <input
          v-model="text"
          class="inp flex-1"
          maxlength="4000"
          :placeholder="t('Send a message to everyone')"
          :aria-label="t('Send a message to everyone')"
        />
        <button type="submit" class="icon-btn" :aria-label="t('Send')" :title="t('Send')">
          <SendHorizontal :size="16" aria-hidden="true" />
        </button>
      </form>
    </template>
    <ul v-else class="meeting-people">
      <li v-for="p in people" :key="p.key">
        <UserPicture :user-id="p.identity" :name="chat.displayName(p.identity)" :size="28" />
        <span class="min-w-0 flex-1 truncate">
          {{ p.local ? t('{name} (you)', { name: chat.displayName(p.identity) }) : chat.displayName(p.identity) }}
        </span>
        <span v-if="p.hand" class="tag meeting-hand-tag">{{ t('Hand raised') }}</span>
        <span v-if="p.muted" class="sr-only">{{ t('Microphone off') }}</span>
      </li>
    </ul>
  </aside>
</template>

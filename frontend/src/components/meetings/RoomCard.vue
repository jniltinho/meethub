<script setup lang="ts">
// The details panel's room card (chat-ui spec): the channel's persistent room, who is in it and "Join room".
import { computed } from 'vue'
import { Video } from '@lucide/vue'
import UserPicture from '@/components/chat/UserPicture.vue'
import { useChat } from '@/stores/chat'
import { useMeetings } from '@/stores/meetings'
import { t } from '@/i18n'

const props = defineProps<{ channelId: string; name: string }>()
const chat = useChat()
const meetings = useMeetings()
const people = computed(() => meetings.people(props.channelId))
</script>

<template>
  <section class="room-card-section" :aria-label="t('Meeting room')">
    <h2><Video :size="15" aria-hidden="true" /> {{ t('Meeting room') }}</h2>
    <p class="text-fg-muted">{{ t('Persistent room of {name}.', { name }) }}</p>
    <div class="room-card" data-room-card>
      <div class="room-card-head">
        <span class="room-card-icon" aria-hidden="true"><Video :size="18" /></span>
        <div class="min-w-0">
          <strong class="block truncate">{{ t('Room of {name}', { name }) }}</strong>
          <span v-if="people.length" class="room-card-status">
            {{ t('{n} in the room', { n: people.length }) }}
          </span>
          <span v-else class="room-card-status">{{ t('Available now') }}</span>
        </div>
      </div>
      <ul v-if="people.length" class="room-card-faces" :aria-label="t('In the room')">
        <li v-for="p in people.slice(0, 8)" :key="p.user_id" :title="chat.displayName(p.user_id)">
          <UserPicture :user-id="p.user_id" :name="chat.displayName(p.user_id)" :size="24" round />
          <span class="sr-only">{{ chat.displayName(p.user_id) }}</span>
        </li>
      </ul>
      <button type="button" class="btn w-full justify-center" data-join-room @click="meetings.openCall(channelId)">
        {{ t('Join room') }}
      </button>
    </div>
  </section>
</template>

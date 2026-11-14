<script setup lang="ts">
// The sidebar's "Meeting rooms" (chat-ui spec): the rooms with people in them among the current team's channels and
// the user's conversations; selecting one opens its pre-join screen.
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { Video } from '@lucide/vue'
import { useChat } from '@/stores/chat'
import { useMeetings } from '@/stores/meetings'
import { t } from '@/i18n'
import { openMeeting } from './open'

const props = defineProps<{ teamId: string }>()
const chat = useChat()
const meetings = useMeetings()
const router = useRouter()
const rooms = computed(() =>
  meetings.active
    .filter((r) => r.team_id === props.teamId || r.team_id === '')
    .map((r) => {
      const c = chat.channels[r.channel_id]
      const name = !c ? '' : c.type === 'D' || c.type === 'G' ? chat.conversationTitle(c) : c.display_name
      return { id: r.channel_id, name, n: r.participants.length }
    })
    .filter((r) => r.name),
)
</script>

<template>
  <template v-if="meetings.enabled && rooms.length">
    <div class="chat-section">
      <h2>{{ t('Meeting rooms') }}</h2>
    </div>
    <button
      v-for="r in rooms"
      :key="r.id"
      type="button"
      class="chat-item meeting-room-item"
      data-meeting-room
      @click="openMeeting(router, r.id)"
    >
      <Video :size="14" aria-hidden="true" />
      <span class="flex-1 truncate text-left">{{ r.name }}</span>
      <span class="room-count">{{ t('{n} in the room', { n: r.n }) }}</span>
    </button>
  </template>
</template>

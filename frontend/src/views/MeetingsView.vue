<script setup lang="ts">
// Meetings (web-shell spec): the rooms with people in them among the user's channels and conversations, each with
// its channel, who is in it and "Join". Every channel has a room: a new meeting starts from its header.
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Video } from '@lucide/vue'
import UserPicture from '@/components/chat/UserPicture.vue'
import { openMeeting } from '@/components/meetings/open'
import { ApiError } from '@/api/client'
import { useChat } from '@/stores/chat'
import { useMeetings } from '@/stores/meetings'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'

const chat = useChat()
const meetings = useMeetings()
const router = useRouter()
const loaded = ref(false)

const rooms = computed(() =>
  meetings.active.map((r) => {
    const c = chat.channels[r.channel_id]
    const conv = c?.type === 'D' || c?.type === 'G'
    const team = chat.teams.find((x) => x.id === r.team_id)
    return {
      id: r.channel_id,
      name: !c ? '…' : conv ? chat.conversationTitle(c) : '#' + c.display_name,
      where: conv ? t('Direct messages') : (team?.display_name ?? ''),
      people: r.participants,
    }
  }),
)

// rooms of channels this page has not seen yet (another team), whenever they appear
const asked = new Set<string>()
watch(
  () => meetings.active.map((r) => r.channel_id),
  (ids) => {
    for (const id of ids.filter((x) => !chat.channels[x] && !asked.has(x))) {
      asked.add(id)
      void chat.loadChannel(id).catch(() => undefined)
    }
  },
  { immediate: true },
)

onMounted(async () => {
  try {
    await meetings.load()
    if (chat.teams.length === 0) await chat.loadTeams()
  } catch (e) {
    toast.error(e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.'))
  } finally {
    loaded.value = true
  }
})

function join(id: string) {
  openMeeting(router, id).catch(() => toast.error(t('Unexpected error.')))
}
</script>

<template>
  <section class="mx-auto grid max-w-3xl gap-4">
    <h1 class="page-title">{{ t('Meetings') }}</h1>
    <p v-if="loaded && !meetings.enabled" class="text-fg-muted">
      {{ t('Meetings are not configured on this server.') }}
    </p>
    <div v-else class="card">
      <div class="card-h">{{ t('Meetings in progress') }}</div>
      <ul class="chat-pick-list p-2">
        <li v-for="r in rooms" :key="r.id" data-meeting-row>
          <Video :size="16" aria-hidden="true" />
          <div class="min-w-0 flex-1">
            <div class="truncate font-semibold">{{ t('Room of {name}', { name: r.name }) }}</div>
            <div class="truncate text-xs text-fg-muted">
              {{ r.where ? r.where + ' · ' : '' }}{{ t('{n} in the room', { n: r.people.length }) }}
            </div>
          </div>
          <span class="meeting-faces" aria-hidden="true">
            <UserPicture
              v-for="p in r.people.slice(0, 5)"
              :key="p.user_id"
              :user-id="p.user_id"
              :name="chat.displayName(p.user_id)"
              :size="24"
              round
            />
          </span>
          <button type="button" class="btn" @click="join(r.id)">{{ t('Join') }}</button>
        </li>
        <li v-if="loaded && rooms.length === 0" class="text-fg-muted">
          {{ t('No meeting in progress. Start one from a channel: every channel has its own room.') }}
        </li>
      </ul>
    </div>
  </section>
</template>

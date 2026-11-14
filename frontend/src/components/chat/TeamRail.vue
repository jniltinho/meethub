<script setup lang="ts">
// The team tiles (chat-ui spec, "Team rail"): one per team of the user, with its initials, the current one marked,
// a count for mentions or a dot for unread messages in the others. A tile opens the team where the user left it.
// It is the 64 px rail on desktop and a row inside the drawer on phones.
import { onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { Plus } from '@lucide/vue'
import { useChat } from '@/stores/chat'
import type { TeamWithState } from '@/chat/types'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const chat = useChat()
const router = useRouter()

onMounted(() => {
  if (chat.teams.length === 0) void chat.loadTeams().catch(() => undefined)
})

/** up to three letters: the capitals of the first words, or the start of a single word */
function tile(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean)
  if (words.length > 1)
    return words
      .slice(0, 3)
      .map((w) => w[0])
      .join('')
      .toUpperCase()
  return (words[0] ?? '?').slice(0, 2).toUpperCase()
}

function label(tm: TeamWithState): string {
  if (tm.id === chat.currentTeamId) return tm.display_name
  if (tm.mention_count) return t('{name}, {n} mentions', { name: tm.display_name, n: tm.mention_count })
  if (tm.has_unread) return t('{name}, unread messages', { name: tm.display_name })
  return tm.display_name
}

async function open(tm: TeamWithState) {
  try {
    const path = await chat.teamHome(tm.id)
    chat.currentTeamId = tm.id
    await router.push(path || '/teams')
  } catch {
    toast.error(t('Unexpected error.'))
  }
}
</script>

<template>
  <ul class="team-tiles" :aria-label="t('Teams')">
    <li v-for="tm in chat.teams" :key="tm.id">
      <button
        type="button"
        class="team-tile"
        :class="{ on: tm.id === chat.currentTeamId }"
        :aria-current="tm.id === chat.currentTeamId ? 'true' : undefined"
        :aria-label="label(tm)"
        :title="tm.display_name"
        :data-team="tm.name"
        @click="open(tm)"
      >
        <span aria-hidden="true">{{ tile(tm.display_name) }}</span>
        <template v-if="tm.id !== chat.currentTeamId">
          <span v-if="tm.mention_count" class="chat-badge" aria-hidden="true">{{ tm.mention_count }}</span>
          <span v-else-if="tm.has_unread" class="unread-dot" aria-hidden="true" />
        </template>
      </button>
    </li>
    <li>
      <RouterLink
        to="/teams"
        class="team-tile add"
        :aria-label="t('Browse or create teams')"
        :title="t('Browse or create teams')"
      >
        <Plus :size="18" aria-hidden="true" />
      </RouterLink>
    </li>
  </ul>
</template>

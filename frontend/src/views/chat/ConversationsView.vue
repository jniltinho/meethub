<script setup lang="ts">
// Conversations: reopen the last channel or conversation that is still readable, else the default channel of the
// user's first team; without teams, an empty state offering to browse teams (the sidebar keeps the conversations).
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { MessagesSquare } from '@lucide/vue'
import { api } from '@/api/client'
import type { ChannelInfo } from '@/chat/types'
import { channelPath, lastLocation } from '@/chat/location'
import { useChat } from '@/stores/chat'
import { useSession } from '@/stores/session'
import { t } from '@/i18n'

const chat = useChat()
const session = useSession()
const router = useRouter()
const noTeams = ref(false)

async function readable(id: string): Promise<boolean> {
  try {
    await api<ChannelInfo>('GET', `/channels/${id}`)
    return true
  } catch {
    return false
  }
}

onMounted(async () => {
  const teams = await chat.loadTeams().catch(() => [])
  const last = lastLocation(session.profile?.user.id ?? '')
  if (last?.kind === 'conversation' && (await readable(last.channel))) {
    await router.replace(`/messages/${last.channel}`)
    return
  }
  if (last?.kind === 'channel' && last.team) {
    const tm = teams.find((x) => x.name === last.team)
    if (tm) {
      await chat.loadChannels(tm.id).catch(() => undefined)
      const id = (chat.teamChannels[tm.id] ?? []).find((c) => chat.channels[c]?.name === last.channel)
      if (id && !chat.channels[id]?.archived_at) {
        await router.replace(channelPath(tm.name, last.channel))
        return
      }
    }
  }
  const first = teams[0]
  if (!first) {
    noTeams.value = true
    return
  }
  await chat.loadChannels(first.id).catch(() => undefined)
  const def = (chat.teamChannels[first.id] ?? []).map((c) => chat.channels[c]).find((c) => c?.is_default)
  if (def) await router.replace(channelPath(first.name, def.name))
  else noTeams.value = true
})
</script>

<template>
  <section v-if="noTeams" class="empty-state">
    <MessagesSquare :size="40" aria-hidden="true" />
    <h1 class="page-title">{{ t('Conversations') }}</h1>
    <p>{{ t('You are not in a team yet. Your direct messages are in the sidebar.') }}</p>
    <RouterLink to="/teams" class="btn">{{ t('Browse teams') }}</RouterLink>
  </section>
  <section v-else class="empty-state" aria-busy="true">
    <h1 class="sr-only">{{ t('Conversations') }}</h1>
    <p>{{ t('Loading…') }}</p>
  </section>
</template>

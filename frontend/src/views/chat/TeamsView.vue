<script setup lang="ts">
// Teams: the user's teams and the open teams they may join.
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Users } from '@lucide/vue'
import { api, ApiError } from '@/api/client'
import type { Page, Team } from '@/chat/types'
import { channelPath } from '@/chat/location'
import { useChat } from '@/stores/chat'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'

const chat = useChat()
const router = useRouter()
const joinable = ref<Team[]>([])
const loaded = ref(false)

async function load() {
  try {
    await chat.loadTeams()
    joinable.value = (await api<Page<Team>>('GET', '/teams?per_page=200')).items
  } catch (e) {
    toast.error(e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.'))
  } finally {
    loaded.value = true
  }
}
onMounted(load)

async function open(id: string, name: string) {
  chat.currentTeamId = id
  await chat.loadChannels(id)
  const def = (chat.teamChannels[id] ?? []).map((c) => chat.channels[c]).find((c) => c?.is_default)
  if (def) await router.push(channelPath(name, def.name))
}

async function join(tm: Team) {
  try {
    await api('POST', `/teams/${tm.id}/members`, {})
    toast.success(t('You joined {name}.', { name: tm.display_name }))
    await load()
    await open(tm.id, tm.name)
  } catch (e) {
    toast.error(e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.'))
  }
}
</script>

<template>
  <section class="mx-auto grid max-w-3xl gap-4">
    <h1 class="page-title">{{ t('Teams') }}</h1>
    <div class="card">
      <div class="card-h">{{ t('Your teams') }}</div>
      <ul class="chat-pick-list p-2">
        <li v-for="tm in chat.teams" :key="tm.id">
          <Users :size="16" aria-hidden="true" />
          <div class="min-w-0 flex-1">
            <div class="truncate font-semibold">{{ tm.display_name }}</div>
            <div v-if="tm.description" class="truncate text-xs text-fg-muted">{{ tm.description }}</div>
          </div>
          <span v-if="tm.mention_count" class="chat-badge" :aria-label="t('{n} mentions', { n: tm.mention_count })">{{
            tm.mention_count
          }}</span>
          <button type="button" class="btn-ghost" @click="open(tm.id, tm.name)">{{ t('Open') }}</button>
        </li>
        <li v-if="loaded && chat.teams.length === 0" class="text-fg-muted">{{ t('You are not in a team yet.') }}</li>
      </ul>
    </div>
    <div class="card">
      <div class="card-h">{{ t('Teams you can join') }}</div>
      <ul class="chat-pick-list p-2">
        <li v-for="tm in joinable" :key="tm.id">
          <Users :size="16" aria-hidden="true" />
          <div class="min-w-0 flex-1">
            <div class="truncate font-semibold">{{ tm.display_name }}</div>
            <div v-if="tm.description" class="truncate text-xs text-fg-muted">{{ tm.description }}</div>
          </div>
          <button type="button" class="btn" @click="join(tm)">{{ t('Join') }}</button>
        </li>
        <li v-if="loaded && joinable.length === 0" class="text-fg-muted">{{ t('No other open teams.') }}</li>
      </ul>
    </div>
  </section>
</template>

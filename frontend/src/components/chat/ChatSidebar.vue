<script setup lang="ts">
// The chat sidebar (chat-ui spec): team switcher with badges for the other teams, the team's channels (unread in
// bold, mention badges) and the user's direct and group conversations with presence.
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ChevronDown, Compass, Hash, Lock, Plus, Users } from '@lucide/vue'
import PresenceDot from './PresenceDot.vue'
import CreateChannelDialog from './CreateChannelDialog.vue'
import BrowseChannelsDialog from './BrowseChannelsDialog.vue'
import NewMessageDialog from './NewMessageDialog.vue'
import { channelPath } from '@/chat/location'
import { useChat, type ChatChannel } from '@/stores/chat'
import { useSession } from '@/stores/session'
import { t } from '@/i18n'

const chat = useChat()
const session = useSession()
const route = useRoute()
const router = useRouter()
const teamMenu = ref(false)
const dialog = ref<'' | 'create' | 'browse' | 'message'>('')

const team = computed(() => chat.teams.find((t) => t.id === chat.currentTeamId) ?? chat.teams[0])
const otherTeams = computed(() => chat.teams.filter((t) => t.id !== team.value?.id))
const otherTeamsMentions = computed(() => otherTeams.value.reduce((n, t) => n + t.mention_count, 0))
const otherTeamsUnread = computed(() => otherTeams.value.some((t) => t.has_unread))

const channels = computed(() =>
  (team.value ? (chat.teamChannels[team.value.id] ?? []) : [])
    .map((id) => chat.channels[id])
    .filter((c): c is ChatChannel => !!c && !c.archived_at)
    .sort((a, b) => a.display_name.localeCompare(b.display_name)),
)
const conversations = computed(() =>
  chat.conversationIds.map((id) => chat.channels[id]).filter((c): c is ChatChannel => !!c),
)
const unread = (c: ChatChannel) => c.total_msg_count - c.msg_count > 0
const activeId = computed(() => chat.openChannelId)

onMounted(async () => {
  await chat.loadTeams().catch(() => undefined)
  await chat.loadConversations().catch(() => undefined)
})
watch(
  () => team.value?.id,
  (id) => {
    if (id && !chat.teamChannels[id]) void chat.loadChannels(id).catch(() => undefined)
  },
  { immediate: true },
)
watch(
  () => route.fullPath,
  () => (teamMenu.value = false),
)

async function switchTeam(id: string) {
  teamMenu.value = false
  const tm = chat.teams.find((x) => x.id === id)
  if (!tm) return
  chat.currentTeamId = id
  await chat.loadChannels(id)
  const def = (chat.teamChannels[id] ?? []).map((c) => chat.channels[c]).find((c) => c?.is_default)
  if (def) await router.push(channelPath(tm.name, def.name))
}

const me = computed(() => session.profile?.user.id ?? '')
</script>

<template>
  <div class="chat-sidebar">
    <div class="relative px-3 pt-2">
      <div class="sidebar-label">{{ t('Team') }}</div>
      <button
        type="button"
        class="team-switch"
        :aria-expanded="teamMenu"
        aria-haspopup="listbox"
        :aria-label="t('Switch team, current: {name}', { name: team?.display_name ?? '' })"
        @click="teamMenu = !teamMenu"
      >
        <Users :size="16" aria-hidden="true" />
        <span class="flex-1 truncate text-left">{{ team?.display_name ?? t('No team') }}</span>
        <span
          v-if="otherTeamsMentions"
          class="chat-badge"
          :aria-label="t('{n} mentions in other teams', { n: otherTeamsMentions })"
        >
          {{ otherTeamsMentions }}
        </span>
        <span v-else-if="otherTeamsUnread" class="unread-dot" :aria-label="t('Unread messages in other teams')" />
        <ChevronDown :size="16" aria-hidden="true" />
      </button>
      <ul v-if="teamMenu" class="select-pop team-menu" role="listbox" :aria-label="t('Teams')">
        <li v-for="tm in chat.teams" :key="tm.id" role="option" :aria-selected="tm.id === team?.id">
          <button
            type="button"
            class="team-option"
            :class="{ 'font-semibold': tm.has_unread }"
            @click="switchTeam(tm.id)"
          >
            <span class="flex-1 truncate">{{ tm.display_name }}</span>
            <span v-if="tm.mention_count" class="chat-badge">{{ tm.mention_count }}</span>
          </button>
        </li>
        <li>
          <RouterLink to="/teams" class="team-option">{{ t('Browse teams') }}</RouterLink>
        </li>
      </ul>
    </div>

    <nav class="chat-nav" :aria-label="t('Channels and conversations')">
      <div class="chat-section">
        <h2>{{ t('Channels') }}</h2>
        <template v-if="team">
          <button
            type="button"
            class="icon-btn"
            :aria-label="t('Browse channels')"
            :title="t('Browse channels')"
            @click="dialog = 'browse'"
          >
            <Compass :size="15" aria-hidden="true" />
          </button>
          <button
            type="button"
            class="icon-btn"
            :aria-label="t('Create a channel')"
            :title="t('Create a channel')"
            @click="dialog = 'create'"
          >
            <Plus :size="15" aria-hidden="true" />
          </button>
        </template>
      </div>
      <p v-if="!team" class="sidebar-empty">
        {{ t('You are not in a team yet.') }} <RouterLink to="/teams" class="act">{{ t('Browse teams') }}</RouterLink>
      </p>
      <RouterLink
        v-for="c in channels"
        :key="c.id"
        :to="channelPath(team!.name, c.name)"
        class="chat-item"
        :class="{ on: c.id === activeId, unread: unread(c) }"
        :aria-current="c.id === activeId ? 'page' : undefined"
        data-chat-channel
      >
        <Lock v-if="c.type === 'P'" :size="14" aria-hidden="true" />
        <Hash v-else :size="14" aria-hidden="true" />
        <span class="flex-1 truncate">{{ c.display_name }}</span>
        <span v-if="c.mention_count" class="chat-badge" :aria-label="t('{n} mentions', { n: c.mention_count })">{{
          c.mention_count
        }}</span>
        <span v-else-if="unread(c)" class="sr-only">{{ t('unread') }}</span>
      </RouterLink>

      <div class="chat-section">
        <h2>{{ t('Direct messages') }}</h2>
        <button
          type="button"
          class="icon-btn"
          :aria-label="t('New message')"
          :title="t('New message')"
          @click="dialog = 'message'"
        >
          <Plus :size="15" aria-hidden="true" />
        </button>
      </div>
      <RouterLink
        v-for="c in conversations"
        :key="c.id"
        :to="`/messages/${c.id}`"
        class="chat-item"
        :class="{ on: c.id === activeId, unread: unread(c) }"
        :aria-current="c.id === activeId ? 'page' : undefined"
        data-chat-channel
      >
        <PresenceDot v-if="c.type === 'D'" :status="chat.statuses[chat.directPeer(c)]" />
        <span v-else class="group-count" aria-hidden="true">{{ (c.members?.length ?? 1) - 1 }}</span>
        <span class="flex-1 truncate">
          {{ chat.conversationTitle(c) }}<template v-if="chat.directPeer(c) === me"> {{ t('(you)') }}</template>
        </span>
        <span v-if="c.mention_count" class="chat-badge" :aria-label="t('{n} mentions', { n: c.mention_count })">{{
          c.mention_count
        }}</span>
      </RouterLink>
      <p v-if="conversations.length === 0" class="sidebar-empty">{{ t('No conversations yet.') }}</p>
    </nav>

    <CreateChannelDialog
      v-if="dialog === 'create' && team"
      :team-id="team.id"
      :team-name="team.name"
      @close="dialog = ''"
    />
    <BrowseChannelsDialog
      v-if="dialog === 'browse' && team"
      :team-id="team.id"
      :team-name="team.name"
      @close="dialog = ''"
    />
    <NewMessageDialog v-if="dialog === 'message'" @close="dialog = ''" />
  </div>
</template>

<script setup lang="ts">
// The open channel's details in the side panel (chat-ui spec): about, participants with picture, presence and
// channel role (online first), and the pinned messages. A thread or the search takes the panel while open.
import { computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { Info, Pin, PinOff, Users } from '@lucide/vue'
import PresenceDot from './PresenceDot.vue'
import UserPicture from './UserPicture.vue'
import { ApiError } from '@/api/client'
import { formatTime } from '@/chat/format'
import { jumpToPost } from '@/chat/jump'
import { renderInline } from '@/chat/render'
import type { LocalPost } from '@/chat/types'
import { useChat } from '@/stores/chat'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'

const chat = useChat()
const router = useRouter()
const id = chat.openChannelId
const channel = computed(() => chat.channels[id])
const conversation = computed(() => channel.value?.type === 'D' || channel.value?.type === 'G')
const title = computed(() => {
  const c = channel.value
  return !c ? '' : conversation.value ? chat.conversationTitle(c) : c.display_name
})
const rank = (uid: string) => ({ online: 0, away: 1, dnd: 2, offline: 3 })[chat.statuses[uid] ?? 'offline']
const people = computed(() =>
  [...(chat.members[id] ?? [])].sort(
    (a, b) => rank(a.id) - rank(b.id) || (a.display_name || a.username).localeCompare(b.display_name || b.username),
  ),
)
const pins = computed(() => (chat.pinned[id] ?? []).map((p) => chat.posts[p]).filter((p): p is LocalPost => !!p))
const purpose = computed(() => (channel.value?.purpose ? renderInline(channel.value.purpose) : ''))
const header = computed(() => (channel.value?.header ? renderInline(channel.value.header) : ''))

function fail(e: unknown) {
  toast.error(e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.'))
}
onMounted(() => {
  void chat.loadMembers(id).catch(fail)
  if (!chat.pinned[id]) void chat.loadPinned(id).catch(fail)
})
</script>

<template>
  <div v-if="channel" class="channel-details">
    <div class="side-panel-head"><Info :size="16" aria-hidden="true" /> {{ t('Channel details') }}</div>
    <section :aria-label="t('About')">
      <h2>{{ title }}</h2>
      <!-- eslint-disable-next-line vue/no-v-html -- sanitized by the fixed pipeline of chat/render.ts -->
      <p v-if="purpose" v-html="purpose" />
      <p v-else-if="!conversation" class="text-fg-muted">{{ t('No purpose yet.') }}</p>
      <!-- eslint-disable-next-line vue/no-v-html -- sanitized by the fixed pipeline of chat/render.ts -->
      <p v-if="header" class="text-fg-muted" v-html="header" />
    </section>

    <section>
      <h2 id="details-members" tabindex="-1">
        <Users :size="15" aria-hidden="true" /> {{ t('Participants ({n})', { n: people.length }) }}
      </h2>
      <ul class="details-people">
        <li v-for="m in people" :key="m.id" data-participant>
          <UserPicture :user-id="m.id" :name="m.display_name || m.username" :size="24" />
          <PresenceDot :status="chat.statuses[m.id]" />
          <span class="min-w-0 flex-1 truncate">{{ m.display_name || m.username }}</span>
          <span v-if="m.role === 'channel_admin'" class="tag">{{ t('Channel administrator') }}</span>
        </li>
      </ul>
    </section>

    <section>
      <h2><Pin :size="15" aria-hidden="true" /> {{ t('Pinned messages ({n})', { n: pins.length }) }}</h2>
      <p v-if="pins.length === 0" class="text-fg-muted">{{ t('No pinned messages.') }}</p>
      <ul class="details-pins">
        <li v-for="p in pins" :key="p.id">
          <button
            type="button"
            class="details-pin"
            :title="t('Go to message')"
            @click="jumpToPost(chat, router, id, p.id, p.root_id).catch(fail)"
          >
            <strong>{{ chat.displayName(p.user_id) || t('Someone') }}</strong>
            <time :datetime="new Date(p.create_at).toISOString()">{{ formatTime(p.create_at) }}</time>
            <span>{{ (p.message ?? '').slice(0, 140) }}</span>
          </button>
          <button
            v-if="!channel.archived_at"
            type="button"
            class="icon-btn"
            :aria-label="t('Unpin')"
            :title="t('Unpin')"
            @click="chat.setPinned(p.id, false).catch(fail)"
          >
            <PinOff :size="14" aria-hidden="true" />
          </button>
        </li>
      </ul>
    </section>
  </div>
</template>

<script setup lang="ts">
// Start a direct (one person) or group (2 to 7 more people) conversation.
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { X } from '@lucide/vue'
import ChatModal from './ChatModal.vue'
import { api, ApiError } from '@/api/client'
import type { Channel, Page, PublicUser } from '@/chat/types'
import { useChat } from '@/stores/chat'
import { useSession } from '@/stores/session'
import { errorMessage, t } from '@/i18n'

const emit = defineEmits<{ close: [] }>()
const chat = useChat()
const session = useSession()
const router = useRouter()
const q = ref('')
const results = ref<PublicUser[]>([])
const picked = ref<PublicUser[]>([])
const error = ref('')
const me = computed(() => session.profile?.user.id ?? '')
let timer: ReturnType<typeof setTimeout> | null = null

watch(
  q,
  (v) => {
    if (timer) clearTimeout(timer)
    timer = setTimeout(async () => {
      try {
        const page = await api<Page<PublicUser>>('GET', `/users?per_page=20&q=${encodeURIComponent(v.trim())}`)
        chat.rememberUsers(page.items)
        results.value = page.items.filter((u) => u.id !== me.value && !picked.value.some((p) => p.id === u.id))
      } catch {
        results.value = []
      }
    }, 150)
  },
  { immediate: true },
)

function pick(u: PublicUser) {
  if (picked.value.length >= 7) return
  picked.value.push(u)
  results.value = results.value.filter((x) => x.id !== u.id)
  q.value = ''
}

async function start() {
  error.value = ''
  try {
    const ids = picked.value.map((u) => u.id)
    const ch =
      ids.length === 1
        ? await api<Channel>('POST', '/channels/direct', { user_id: ids[0] })
        : await api<Channel>('POST', '/channels/group', { user_ids: ids })
    await chat.loadConversations()
    emit('close')
    await router.push(`/messages/${ch.id}`)
  } catch (e) {
    error.value = e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')
  }
}
</script>

<template>
  <ChatModal :title="t('New message')" @close="emit('close')">
    <form @submit.prevent="start">
      <div class="pnl-body grid gap-3">
        <div v-if="picked.length" class="flex flex-wrap gap-1" :aria-label="t('Selected people')">
          <span v-for="u in picked" :key="u.id" class="chat-chip">
            {{ u.display_name || u.username }}
            <button
              type="button"
              class="icon-btn h-5! w-5!"
              :aria-label="t('Remove {name}', { name: u.display_name || u.username })"
              @click="picked = picked.filter((p) => p.id !== u.id)"
            >
              <X :size="12" aria-hidden="true" />
            </button>
          </span>
        </div>
        <input
          v-model="q"
          class="inp"
          type="search"
          autofocus
          :aria-label="t('Find people')"
          :placeholder="t('Find people by name or username')"
        />
        <ul class="chat-pick-list max-h-64 overflow-auto">
          <li v-for="u in results" :key="u.id">
            <span class="chat-avatar" aria-hidden="true">{{ (u.display_name || u.username).slice(0, 1) }}</span>
            <div class="min-w-0 flex-1">
              <div class="truncate">{{ u.display_name }}</div>
              <div class="truncate text-xs text-fg-muted">@{{ u.username }}</div>
            </div>
            <button type="button" class="btn-ghost" :disabled="picked.length >= 7" @click="pick(u)">
              {{ t('Add') }}
            </button>
          </li>
        </ul>
        <p class="text-xs text-fg-muted">{{ t('One person for a direct message, up to 7 for a group.') }}</p>
        <p v-if="error" class="field-error" role="alert">{{ error }}</p>
      </div>
      <div class="pnl-foot">
        <button type="submit" class="btn" :disabled="picked.length === 0">{{ t('Start') }}</button>
        <button type="button" class="btn-ghost" @click="emit('close')">{{ t('Cancel') }}</button>
      </div>
    </form>
  </ChatModal>
</template>

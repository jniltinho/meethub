<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Hash } from '@lucide/vue'
import ChatModal from './ChatModal.vue'
import { api, ApiError } from '@/api/client'
import type { ChannelInfo, Page } from '@/chat/types'
import { channelPath } from '@/chat/location'
import { useChat } from '@/stores/chat'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'

const props = defineProps<{ teamId: string; teamName: string }>()
const emit = defineEmits<{ close: [] }>()
const chat = useChat()
const router = useRouter()
const list = ref<ChannelInfo[]>([])
const loaded = ref(false)
const q = ref('')
const shown = computed(() => {
  const s = q.value.trim().toLowerCase()
  return s ? list.value.filter((c) => c.display_name.toLowerCase().includes(s) || c.name.includes(s)) : list.value
})

onMounted(async () => {
  try {
    list.value = (await api<Page<ChannelInfo>>('GET', `/teams/${props.teamId}/channels?per_page=200`)).items
  } catch (e) {
    toast.error(e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.'))
  } finally {
    loaded.value = true
  }
})

async function join(c: ChannelInfo) {
  try {
    await api('POST', `/channels/${c.id}/members`, {})
    await chat.loadChannels(props.teamId)
    emit('close')
    await router.push(channelPath(props.teamName, c.name))
  } catch (e) {
    toast.error(e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.'))
  }
}
</script>

<template>
  <ChatModal :title="t('Browse channels')" @close="emit('close')">
    <div class="pnl-body grid gap-3">
      <input
        v-model="q"
        class="inp"
        type="search"
        :aria-label="t('Search channels')"
        :placeholder="t('Search channels')"
      />
      <p v-if="loaded && shown.length === 0" class="text-fg-muted">{{ t('No other channels to join.') }}</p>
      <ul class="chat-pick-list">
        <li v-for="c in shown" :key="c.id">
          <Hash :size="16" aria-hidden="true" />
          <div class="min-w-0 flex-1">
            <div class="truncate font-semibold">{{ c.display_name }}</div>
            <div class="truncate text-xs text-fg-muted">
              {{ c.purpose || t('{n} members', { n: c.member_count }) }}
            </div>
          </div>
          <button type="button" class="btn-ghost" @click="join(c)">{{ t('Join') }}</button>
        </li>
      </ul>
    </div>
  </ChatModal>
</template>

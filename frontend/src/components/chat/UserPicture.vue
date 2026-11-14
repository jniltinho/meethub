<script setup lang="ts">
// A person's profile picture, with their initials as the fallback (no picture, unknown user, or a failed load).
// The picture's version is part of the URL, so a new upload is a new address and the cache never shows an old one.
import { computed, ref } from 'vue'
import { initials } from '@/chat/format'
import { useChat } from '@/stores/chat'
import { useSession } from '@/stores/session'

const props = defineProps<{ userId: string; name: string; size?: number; round?: boolean }>()
const chat = useChat()
const session = useSession()
const version = computed(() => {
  const own = session.profile?.user
  return chat.users[props.userId]?.last_picture_update ?? (own?.id === props.userId ? own.last_picture_update : 0) ?? 0
})
const failed = ref(0) // the version that could not be loaded
const px = computed(() => props.size ?? 36)
</script>

<template>
  <span
    class="chat-avatar"
    :class="{ round }"
    :style="{ width: px + 'px', height: px + 'px', fontSize: Math.max(9, Math.round(px * 0.36)) + 'px' }"
    aria-hidden="true"
  >
    <img
      v-if="version && failed !== version"
      :src="`/api/v1/users/${userId}/image?v=${version}`"
      alt=""
      :width="px"
      :height="px"
      loading="lazy"
      @error="failed = version"
    />
    <template v-else>{{ initials(name) }}</template>
  </span>
</template>

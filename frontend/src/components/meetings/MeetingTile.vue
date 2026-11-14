<script setup lang="ts">
// One tile of the meeting grid: the camera or shared screen, else the person's picture; their name, a muted
// microphone, a raised hand, a reaction for a few seconds, and an outline while they speak.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Hand, MicOff } from '@lucide/vue'
import type { Track } from 'livekit-client'
import UserPicture from '@/components/chat/UserPicture.vue'
import { useChat } from '@/stores/chat'
import { t } from '@/i18n'
import type { Tile } from './call'

const props = defineProps<{ tile: Tile }>()
const chat = useChat()
const video = ref<HTMLVideoElement | null>(null)
const name = computed(() => chat.displayName(props.tile.identity) || t('Someone'))
const label = computed(() => {
  if (props.tile.screen) return t('{name} (screen)', { name: name.value })
  return props.tile.local ? t('{name} (you)', { name: name.value }) : name.value
})

let attached: Track | undefined
watch(
  [() => props.tile.track, video],
  ([track, el]) => {
    if (attached && (attached !== track || !el)) attached.detach()
    attached = undefined
    if (track && el) {
      track.attach(el)
      attached = track
    }
  },
  { immediate: true, flush: 'post' },
)
onBeforeUnmount(() => attached?.detach())
</script>

<template>
  <figure
    class="meeting-tile"
    :class="{ speaking: tile.speaking, screen: tile.screen }"
    :data-tile="tile.identity"
    :data-local="tile.local || undefined"
  >
    <video v-if="tile.track" ref="video" autoplay playsinline muted :class="{ mirror: tile.local && !tile.screen }" />
    <UserPicture v-else :user-id="tile.identity" :name="name" :size="96" round />
    <span v-if="tile.reaction" class="meeting-reaction" aria-hidden="true">{{ tile.reaction }}</span>
    <span v-if="tile.hand" class="meeting-hand" :title="t('Hand raised')">
      <Hand :size="14" aria-hidden="true" /><span class="sr-only">{{ t('Hand raised') }}</span>
    </span>
    <span v-if="tile.muted && !tile.screen" class="meeting-muted" :title="t('Microphone off')">
      <MicOff :size="14" aria-hidden="true" /><span class="sr-only">{{ t('Microphone off') }}</span>
    </span>
    <figcaption class="meeting-name">{{ label }}</figcaption>
  </figure>
</template>

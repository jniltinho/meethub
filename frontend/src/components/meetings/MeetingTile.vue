<script setup lang="ts">
// One tile of the meeting grid: the camera or shared screen, else the person's picture; their name with a speaking
// mark, muted-microphone and camera-off marks, a raised hand, a reaction for a few seconds, and an outline while they
// speak. Every mark is absolutely placed and the outline is an outline, so no state moves anything.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { AudioLines, Hand, MicOff, VideoOff } from '@lucide/vue'
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
    <span v-if="tile.hand" class="meeting-hand"> <Hand :size="14" aria-hidden="true" />{{ t('Hand raised') }} </span>
    <span v-if="!tile.screen && (tile.muted || tile.camOff)" class="meeting-marks">
      <span v-if="tile.muted" class="meeting-mark meeting-muted" role="img" :aria-label="t('Microphone off')">
        <MicOff :size="14" aria-hidden="true" />
      </span>
      <span v-if="tile.camOff" class="meeting-mark meeting-camoff" role="img" :aria-label="t('Camera off')">
        <VideoOff :size="14" aria-hidden="true" />
      </span>
    </span>
    <figcaption class="meeting-name">
      <AudioLines v-if="tile.speaking && !tile.screen" :size="14" class="meeting-speaking-icon" aria-hidden="true" />
      <span class="truncate">{{ label }}</span>
      <span v-if="tile.speaking && !tile.screen" class="sr-only">{{ t('speaking') }}</span>
    </figcaption>
  </figure>
</template>

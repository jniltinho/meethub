<script setup lang="ts">
// A meeting's recording in its message (chat-ui spec, add-meeting-recordings): a themed player — play and pause
// from the keyboard, the position, the duration — and a download; a partial, silent, expired or failed recording
// says so instead.
import { computed, ref } from 'vue'
import { CircleDot, Download, Pause, Play, TriangleAlert } from '@lucide/vue'
import { fileUrl } from '@/chat/files'
import type { Post } from '@/chat/types'
import { t } from '@/i18n'

const props = defineProps<{ post: Post }>()
const p = computed(() => (props.post.props ?? {}) as Record<string, unknown>)
const file = computed(() => props.post.metadata?.files?.[0])
const failed = computed(() => props.post.type === 'recording_failed')
const expired = computed(() => p.value.expired === true || !file.value)
const video = computed(() => p.value.mode === 'video')
const media = ref<HTMLMediaElement | null>(null)
const playing = ref(false)
const pos = ref(0)
const dur = ref(Number(p.value.duration_ms ?? 0) / 1000)

/** length as people say it: "42 min", "1 h 5 min", "35 s" */
function spoken(seconds: number): string {
  const s = Math.round(seconds)
  if (s < 60) return t('{n} s', { n: s })
  const m = Math.round(s / 60)
  if (m < 60) return t('{n} min', { n: m })
  return t('{h} h {m} min', { h: Math.floor(m / 60), m: m % 60 })
}
function clock(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds))
  const h = Math.floor(s / 3600)
  const mm = String(Math.floor((s % 3600) / 60)).padStart(h ? 2 : 1, '0')
  const ss = String(s % 60).padStart(2, '0')
  return h ? `${h}:${mm}:${ss}` : `${mm}:${ss}`
}
function toggle() {
  const m = media.value
  if (!m) return
  if (m.paused) void m.play().catch(() => undefined)
  else m.pause()
}
function seek(e: Event) {
  const m = media.value
  if (m) m.currentTime = Number((e.target as HTMLInputElement).value)
}
function loaded() {
  if (media.value && Number.isFinite(media.value.duration)) dur.value = media.value.duration
}
</script>

<template>
  <p v-if="failed" class="rec-msg-failed" data-recording-failed>
    <TriangleAlert :size="15" aria-hidden="true" />{{ t('The recording of this meeting could not be saved') }}
  </p>
  <div v-else class="rec-msg" data-recording-message>
    <p class="rec-msg-title">
      <CircleDot :size="15" aria-hidden="true" />
      <span>{{ t('Meeting recording') }}</span>
      <span class="rec-msg-time">{{ spoken(Number(p.duration_ms ?? 0) / 1000) }}</span>
    </p>
    <p v-if="expired" class="rec-msg-note" data-recording-expired>
      {{ t('This recording expired: its file was removed.') }}
    </p>
    <template v-else>
      <p v-if="p.partial" class="rec-msg-note">
        {{ t('Partial recording: part of the meeting could not be captured.') }}
      </p>
      <p v-if="p.silent" class="rec-msg-note">{{ t('Nobody spoke during this recording.') }}</p>
      <video
        v-if="video"
        ref="media"
        :src="fileUrl(file!.id)"
        preload="metadata"
        playsinline
        @click="toggle"
        @loadedmetadata="loaded"
        @timeupdate="pos = media?.currentTime ?? 0"
        @play="playing = true"
        @pause="playing = false"
        @ended="playing = false"
      />
      <audio
        v-else
        ref="media"
        :src="fileUrl(file!.id)"
        preload="metadata"
        @loadedmetadata="loaded"
        @timeupdate="pos = media?.currentTime ?? 0"
        @play="playing = true"
        @pause="playing = false"
        @ended="playing = false"
      />
      <div class="rec-msg-controls">
        <button
          type="button"
          class="icon-btn"
          data-recording-play
          :aria-label="playing ? t('Pause the recording') : t('Play the recording')"
          :title="playing ? t('Pause the recording') : t('Play the recording')"
          @click="toggle"
        >
          <Pause v-if="playing" :size="16" aria-hidden="true" />
          <Play v-else :size="16" aria-hidden="true" />
        </button>
        <input
          type="range"
          min="0"
          :max="Math.max(1, Math.floor(dur))"
          step="1"
          :value="Math.floor(pos)"
          :aria-label="t('Position in the recording')"
          :aria-valuetext="clock(pos) + ' / ' + clock(dur)"
          @input="seek"
        />
        <span class="rec-msg-time">{{ clock(pos) }} / {{ clock(dur) }}</span>
        <a
          class="icon-btn"
          :href="fileUrl(file!.id)"
          :download="file!.name"
          data-recording-download
          :aria-label="t('Download the recording')"
          :title="t('Download the recording')"
        >
          <Download :size="16" aria-hidden="true" />
        </a>
      </div>
    </template>
  </div>
</template>

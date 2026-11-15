<script setup lang="ts">
// A meeting's recording in its message (chat-ui spec, add-meeting-recordings): a themed player — play and pause
// from the keyboard, the position, the duration — and a download; a partial, silent, expired or failed recording
// says so instead. Its AI summary (add-meeting-summaries): the state, a link to the reply in the thread, and for
// who may record "Summarize" or "Try again" — never for a recording without summary sources.
import { computed, ref, watch } from 'vue'
import { CircleDot, Download, Pause, Play, Sparkles, TriangleAlert } from '@lucide/vue'
import { fileUrl } from '@/chat/files'
import type { Post } from '@/chat/types'
import { api, ApiError } from '@/api/client'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'

const props = defineProps<{ post: Post }>()
const emit = defineEmits<{ thread: [] }>()
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
interface SummaryState {
  status: string
  source: string
  error?: string
  post_id?: string
}
const summary = computed(() => (p.value.summary ?? null) as SummaryState | null)
const writing = computed(() => ['queued', 'transcribing', 'summarizing'].includes(summary.value?.status ?? ''))
// whether the viewer may ask now is the server's to say (who may record, the setting, the sources' age)
const canRequest = ref(false)
const asking = ref(false)
watch(
  () => [summary.value?.status, summary.value?.source],
  () => {
    canRequest.value = false
    const s = summary.value
    if (!s || s.source !== 'available' || !['none', 'failed', 'cancelled'].includes(s.status)) return
    api<{ can_request: boolean }>('GET', `/recordings/${String(p.value.recording_id)}/summary`)
      .then((v) => (canRequest.value = v.can_request))
      .catch(() => undefined)
  },
  { immediate: true },
)
async function summarize() {
  if (asking.value) return
  asking.value = true
  try {
    await api('POST', `/recordings/${String(p.value.recording_id)}/summary`)
    canRequest.value = false
  } catch (e) {
    toast.error(e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.'))
  } finally {
    asking.value = false
  }
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
    <div v-if="summary && (summary.status !== 'none' || canRequest)" class="rec-msg-summary" data-summary-state>
      <Sparkles :size="14" aria-hidden="true" />
      <span v-if="writing" role="status">{{ t('Writing the summary…') }}</span>
      <button
        v-else-if="summary.status === 'ready'"
        type="button"
        class="act"
        data-summary-link
        @click="emit('thread')"
      >
        {{ t('AI summary in the thread') }}
      </button>
      <span v-else-if="summary.status === 'failed'" role="status">{{ t('The summary could not be written') }}</span>
      <span v-else-if="summary.status === 'cancelled'" role="status">{{ t('The summary was cancelled') }}</span>
      <button v-if="canRequest" type="button" class="act" data-summarize :disabled="asking" @click="summarize">
        {{ summary.status === 'none' ? t('Summarize') : t('Try again') }}
      </button>
    </div>
  </div>
</template>

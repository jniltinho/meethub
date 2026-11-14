<script setup lang="ts">
// The pre-join screen (meeting-room-ui spec): camera preview, the microphone's live level, camera and microphone
// pickers, the toggles, who is already in the room, and "Join now". A refused device and a missing one each get
// their own themed message; a refused join (token or meeting server) shows here with the same button as the retry.
import { computed, onMounted, ref, watch } from 'vue'
import { Mic, MicOff, Video, VideoOff } from '@lucide/vue'
import UserPicture from '@/components/chat/UserPicture.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import {
  call,
  chooseDevice,
  type DeviceState,
  devices,
  micLevel,
  preview,
  previewVideo,
  setCam,
  setMic,
  startPreview,
} from './call'
import { useChat } from '@/stores/chat'
import { useMeetings } from '@/stores/meetings'
import { useSession } from '@/stores/session'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const props = defineProps<{ channelId: string; title: string; busy: boolean; error: string; shortcut: string }>()
const emit = defineEmits<{ join: []; back: [] }>()
const chat = useChat()
const meetings = useMeetings()
const session = useSession()
const me = computed(() => session.profile?.user.id ?? '')
const inRoom = computed(() => meetings.people(props.channelId).filter((p) => p.user_id !== me.value))
const state = ref<DeviceState>('checking')
const videoEl = ref<HTMLVideoElement | null>(null)

async function check() {
  state.value = 'checking'
  state.value = await startPreview()
}
onMounted(check)

watch(
  [() => previewVideo(), videoEl, () => preview.value],
  ([track, el]) => {
    if (track && el) track.attach(el)
  },
  { flush: 'post' },
)

const hasAudio = computed(() => preview.value.some((x) => x.kind === 'audio'))
const hasVideo = computed(() => preview.value.some((x) => x.kind === 'video'))

async function toggle(kind: 'mic' | 'cam') {
  try {
    if (kind === 'mic') await setMic(!call.mic)
    else await setCam(!call.cam)
  } catch {
    toast.error(kind === 'mic' ? t('The microphone could not be changed.') : t('The camera could not be changed.'))
  }
}

async function pick(kind: 'audio' | 'video', e: Event) {
  try {
    await chooseDevice(kind, (e.target as HTMLSelectElement).value)
  } catch {
    toast.error(t('That device could not be opened.'))
  }
}
</script>

<template>
  <section class="meeting-prejoin" :aria-busy="busy || state === 'checking'">
    <div class="meeting-preview-col">
      <div class="meeting-preview">
        <video v-if="previewVideo()" ref="videoEl" autoplay playsinline muted class="mirror" />
        <div v-else class="meeting-preview-off">
          <UserPicture :user-id="me" :name="session.profile?.user.display_name ?? ''" :size="96" round />
          <span>{{ state === 'checking' ? t('Opening the camera…') : t('Camera off') }}</span>
        </div>
        <div class="meeting-preview-controls">
          <button
            type="button"
            class="meeting-ctl"
            :class="{ off: !call.mic }"
            :aria-pressed="call.mic"
            :disabled="!hasAudio"
            :aria-label="call.mic ? t('Turn off microphone') : t('Turn on microphone')"
            :data-tip="(call.mic ? t('Turn off microphone') : t('Turn on microphone')) + ' (' + shortcut + '+D)'"
            data-control="prejoin-mic"
            @click="toggle('mic')"
          >
            <Mic v-if="call.mic" :size="20" aria-hidden="true" />
            <MicOff v-else :size="20" aria-hidden="true" />
          </button>
          <button
            type="button"
            class="meeting-ctl"
            :class="{ off: !call.cam }"
            :aria-pressed="call.cam"
            :disabled="!hasVideo"
            :aria-label="call.cam ? t('Turn off camera') : t('Turn on camera')"
            :data-tip="(call.cam ? t('Turn off camera') : t('Turn on camera')) + ' (' + shortcut + '+E)'"
            data-control="prejoin-cam"
            @click="toggle('cam')"
          >
            <Video v-if="call.cam" :size="20" aria-hidden="true" />
            <VideoOff v-else :size="20" aria-hidden="true" />
          </button>
        </div>
      </div>
      <div v-if="hasAudio" class="meeting-level">
        <Mic :size="14" aria-hidden="true" />
        <span
          class="meeting-level-bar"
          role="meter"
          :aria-label="t('Microphone level')"
          aria-valuemin="0"
          aria-valuemax="100"
          :aria-valuenow="Math.round(micLevel * 100)"
          data-mic-level
        >
          <span :style="{ transform: `scaleX(${micLevel})` }" />
        </span>
      </div>
      <div v-if="devices.audio.length || devices.video.length" class="meeting-pickers">
        <div v-if="devices.audio.length">
          <label for="meeting-mic-picker">{{ t('Microphone') }}</label>
          <select id="meeting-mic-picker" class="inp" :value="call.micId" @change="pick('audio', $event)">
            <option value="">{{ t('Default') }}</option>
            <option v-for="d in devices.audio" :key="d.deviceId" :value="d.deviceId">
              {{ d.label || t('Microphone') }}
            </option>
          </select>
        </div>
        <div v-if="devices.video.length">
          <label for="meeting-cam-picker">{{ t('Camera') }}</label>
          <select id="meeting-cam-picker" class="inp" :value="call.camId" @change="pick('video', $event)">
            <option value="">{{ t('Default') }}</option>
            <option v-for="d in devices.video" :key="d.deviceId" :value="d.deviceId">
              {{ d.label || t('Camera') }}
            </option>
          </select>
        </div>
      </div>
    </div>

    <div class="meeting-prejoin-side">
      <h1 id="meeting-title" class="page-title">{{ title }}</h1>
      <p v-if="inRoom.length" class="meeting-who">
        <span class="meeting-faces" aria-hidden="true">
          <UserPicture
            v-for="p in inRoom.slice(0, 5)"
            :key="p.user_id"
            :user-id="p.user_id"
            :name="chat.displayName(p.user_id)"
            :size="24"
            round
          />
        </span>
        {{ t('{n} in the room', { n: inRoom.length }) }}
      </p>
      <p v-else class="meeting-muted-text">{{ t('Nobody is in the room yet.') }}</p>
      <UiAlert v-if="state === 'denied'" kind="warning" data-devices="denied">
        {{
          t(
            'The browser blocked the camera and microphone. Allow them in the address bar, then try again. You can also join without them.',
          )
        }}
      </UiAlert>
      <UiAlert v-else-if="state === 'none'" kind="info" data-devices="none">
        {{ t('No camera or microphone was found. You can still join to listen and chat.') }}
      </UiAlert>
      <button
        v-if="state === 'denied' || state === 'none'"
        type="button"
        class="btn-ghost justify-self-start"
        @click="check"
      >
        {{ t('Try again') }}
      </button>
      <UiAlert v-if="error" kind="error" data-join-error>{{ error }}</UiAlert>
      <div class="flex flex-wrap gap-2">
        <button type="button" class="btn" data-autofocus :disabled="busy" @click="emit('join')">
          {{ busy ? t('Connecting…') : t('Join now') }}
        </button>
        <button type="button" class="btn-ghost" @click="emit('back')">{{ t('Back') }}</button>
      </div>
    </div>
  </section>
</template>

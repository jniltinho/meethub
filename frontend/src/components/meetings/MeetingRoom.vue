<script setup lang="ts">
// The full-screen meeting room (meeting-room-ui spec, design D11 of the layout): a pre-join screen with the camera
// preview, then the grid, the round controls centered at the bottom and the channel's messages and people in a panel
// on the right. A modal dialog: focus stays inside; leaving returns focus where it was.
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  EllipsisVertical,
  Hand,
  Maximize,
  MessageSquare,
  Mic,
  MicOff,
  MonitorUp,
  MonitorX,
  PhoneOff,
  Smile,
  Users,
  Video,
  VideoOff,
} from '@lucide/vue'
import UserPicture from '@/components/chat/UserPicture.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import MeetingPanel from './MeetingPanel.vue'
import MeetingTile from './MeetingTile.vue'
import {
  call,
  type EndReason,
  join,
  leave,
  preview,
  previewVideo,
  react,
  REACTIONS,
  setCam,
  setHand,
  setMic,
  setSharing,
  startPreview,
  stopPreview,
  tiles,
} from './call'
import { ApiError } from '@/api/client'
import { announce } from '@/chat/announce'
import { formatTime } from '@/chat/format'
import { useChat } from '@/stores/chat'
import { useMeetings } from '@/stores/meetings'
import { useSession } from '@/stores/session'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'

const meetings = useMeetings()
const chat = useChat()
const session = useSession()
const id = meetings.callChannel
const channel = computed(() => chat.channels[id])
const name = computed(() => {
  const c = channel.value
  if (!c) return ''
  return c.type === 'D' || c.type === 'G' ? chat.conversationTitle(c) : '#' + c.display_name
})
const title = computed(() => t('Room of {name}', { name: name.value }))
const me = computed(() => session.profile?.user.id ?? '')
const inRoom = computed(() => meetings.people(id).filter((p) => p.user_id !== me.value))

const root = ref<HTMLElement | null>(null)
const audio = ref<HTMLElement | null>(null)
const previewEl = ref<HTMLVideoElement | null>(null)
const devices = ref<'checking' | 'ok' | 'none'>('checking')
const busy = ref(false)
const panel = ref<'' | 'chat' | 'people'>(window.innerWidth >= 1024 ? 'chat' : '')
const menu = ref<'' | 'reactions' | 'more'>('')
const now = ref(Date.now())
const clock = setInterval(() => (now.value = Date.now()), 15000)
const opener = document.activeElement as HTMLElement | null

const cols = computed(() => Math.max(1, Math.ceil(Math.sqrt(tiles.value.length))))

watch(
  [() => previewVideo(), previewEl, () => preview.value],
  ([track, el]) => {
    if (track && el) track.attach(el)
  },
  { flush: 'post' },
)

onMounted(async () => {
  await nextTick()
  root.value?.querySelector<HTMLElement>('[data-autofocus]')?.focus()
  devices.value = (await startPreview()) ? 'ok' : 'none'
})

onBeforeUnmount(() => {
  clearInterval(clock)
  stopPreview()
  void leave()
  opener?.focus?.()
})

function fail(e: unknown, fallback: string) {
  toast.error(e instanceof ApiError ? errorMessage(e.code, e.message) : fallback)
}

function ended(why: EndReason) {
  if (why === 'removed') toast.warning(t('You no longer have access to this channel.'))
  else if (why === 'elsewhere') toast.info(t('You joined this meeting from another tab or device.'))
  else if (why === 'lost') toast.error(t('The connection to the meeting was lost.'))
  announce(t('You left the meeting.'))
  meetings.closeCall()
}

async function joinNow() {
  if (busy.value) return
  busy.value = true
  try {
    const tok = await meetings.token(id)
    await join(tok.url, tok.token, audio.value!, ended)
    announce(t('You joined {name}.', { name: title.value }))
    await nextTick()
    root.value?.querySelector<HTMLElement>('[data-control="mic"]')?.focus()
  } catch (e) {
    fail(e, t('Could not join the meeting.'))
  } finally {
    busy.value = false
  }
}

async function hangUp() {
  if (call.phase === 'idle') {
    stopPreview()
    meetings.closeCall()
    return
  }
  await leave()
}

async function run(fn: () => Promise<void>, fallback: string) {
  try {
    await fn()
  } catch (e) {
    fail(e, fallback)
  }
}

const toggleMic = () => run(() => setMic(!call.mic), t('The microphone could not be changed.'))
const toggleCam = () => run(() => setCam(!call.cam), t('The camera could not be changed.'))
const toggleShare = () => run(() => setSharing(!call.sharing), t('The screen could not be shared.'))
const toggleHand = () => run(() => setHand(!call.hand), t('Could not raise your hand.'))
function sendReaction(e: string) {
  menu.value = ''
  void run(() => react(e), t('The reaction could not be sent.'))
}
function togglePanel(p: 'chat' | 'people') {
  menu.value = ''
  panel.value = panel.value === p ? '' : p
}
function fullscreen() {
  menu.value = ''
  if (document.fullscreenElement) void document.exitFullscreen().catch(() => undefined)
  else void root.value?.requestFullscreen?.().catch(() => undefined)
}

// the channel disappeared for this user (removed, archived, team left): the call ends with it
watch(channel, (c) => {
  if (!c) void hangUp()
})

// people joining and leaving, announced politely
watch(
  () => tiles.value.filter((x) => !x.screen && !x.local).map((x) => x.identity),
  (cur, prev) => {
    if (call.phase !== 'joined' || !prev) return
    for (const u of cur.filter((x) => !prev.includes(x))) announce(t('{name} joined.', { name: chat.displayName(u) }))
    for (const u of prev.filter((x) => !cur.includes(x))) announce(t('{name} left.', { name: chat.displayName(u) }))
  },
)

// a modal dialog: Tab cycles inside, Escape closes an open menu but never leaves the call
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    menu.value = ''
    return
  }
  if (e.key !== 'Tab' || !root.value) return
  const all = [
    ...root.value.querySelectorAll<HTMLElement>('button:not(:disabled), input, [tabindex]:not([tabindex="-1"])'),
  ].filter((el) => el.offsetParent !== null)
  if (all.length === 0) return
  const first = all[0]!
  const last = all[all.length - 1]!
  if (e.shiftKey && document.activeElement === first) {
    e.preventDefault()
    last.focus()
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault()
    first.focus()
  }
}
</script>

<template>
  <div
    ref="root"
    class="meeting-room"
    role="dialog"
    aria-modal="true"
    aria-labelledby="meeting-title"
    :data-phase="call.phase"
    @keydown="onKey"
  >
    <div ref="audio" class="sr-only" aria-hidden="true" />

    <!-- pre-join -->
    <section v-if="call.phase === 'idle'" class="meeting-prejoin">
      <div class="meeting-preview">
        <video v-if="previewVideo()" ref="previewEl" autoplay playsinline muted class="mirror" />
        <UserPicture v-else :user-id="me" :name="session.profile?.user.display_name ?? ''" :size="96" round />
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
        <p v-else class="text-fg-muted">{{ t('Nobody is in the room yet.') }}</p>
        <UiAlert v-if="devices === 'none'" kind="warning">
          {{ t('The camera and microphone are unavailable. You can still join without them.') }}
        </UiAlert>
        <div class="meeting-prejoin-toggles">
          <button
            type="button"
            class="meeting-ctl"
            :class="{ off: !call.mic }"
            :aria-pressed="call.mic"
            :disabled="devices !== 'ok'"
            :aria-label="call.mic ? t('Turn off microphone') : t('Turn on microphone')"
            :title="call.mic ? t('Turn off microphone') : t('Turn on microphone')"
            @click="toggleMic"
          >
            <Mic v-if="call.mic" :size="20" aria-hidden="true" />
            <MicOff v-else :size="20" aria-hidden="true" />
          </button>
          <button
            type="button"
            class="meeting-ctl"
            :class="{ off: !call.cam }"
            :aria-pressed="call.cam"
            :disabled="devices !== 'ok'"
            :aria-label="call.cam ? t('Turn off camera') : t('Turn on camera')"
            :title="call.cam ? t('Turn off camera') : t('Turn on camera')"
            @click="toggleCam"
          >
            <Video v-if="call.cam" :size="20" aria-hidden="true" />
            <VideoOff v-else :size="20" aria-hidden="true" />
          </button>
        </div>
        <div class="flex flex-wrap gap-2">
          <button type="button" class="btn" data-autofocus :disabled="busy" @click="joinNow">
            {{ busy ? t('Joining…') : t('Join now') }}
          </button>
          <button type="button" class="btn-ghost" @click="hangUp">{{ t('Back') }}</button>
        </div>
      </div>
    </section>

    <!-- the call -->
    <template v-else>
      <div class="meeting-stage" :class="{ 'with-panel': panel }">
        <div class="meeting-grid" :style="{ '--cols': cols }" :aria-label="t('Participants')" role="list">
          <div v-for="tile in tiles" :key="tile.key" role="listitem" :class="{ 'meeting-spot': tile.screen }">
            <MeetingTile :tile="tile" />
          </div>
          <p v-if="call.phase === 'joining'" class="meeting-joining" role="status">{{ t('Joining…') }}</p>
        </div>
        <MeetingPanel
          v-if="panel"
          :channel-id="id"
          :title="name"
          :tab="panel"
          @tab="(p) => (panel = p)"
          @close="panel = ''"
        />
      </div>

      <footer class="meeting-bar">
        <div class="meeting-bar-info">
          <time>{{ formatTime(now) }}</time>
          <h1 id="meeting-title" class="truncate">{{ title }}</h1>
        </div>
        <div class="meeting-controls" role="toolbar" :aria-label="t('Call controls')">
          <button
            type="button"
            class="meeting-ctl"
            :class="{ off: !call.mic }"
            data-control="mic"
            :aria-pressed="call.mic"
            :aria-label="call.mic ? t('Turn off microphone') : t('Turn on microphone')"
            :title="call.mic ? t('Turn off microphone') : t('Turn on microphone')"
            @click="toggleMic"
          >
            <Mic v-if="call.mic" :size="20" aria-hidden="true" />
            <MicOff v-else :size="20" aria-hidden="true" />
          </button>
          <button
            type="button"
            class="meeting-ctl"
            :class="{ off: !call.cam }"
            data-control="cam"
            :aria-pressed="call.cam"
            :aria-label="call.cam ? t('Turn off camera') : t('Turn on camera')"
            :title="call.cam ? t('Turn off camera') : t('Turn on camera')"
            @click="toggleCam"
          >
            <Video v-if="call.cam" :size="20" aria-hidden="true" />
            <VideoOff v-else :size="20" aria-hidden="true" />
          </button>
          <button
            type="button"
            class="meeting-ctl"
            :class="{ on: call.sharing }"
            data-control="share"
            :aria-pressed="call.sharing"
            :aria-label="call.sharing ? t('Stop sharing') : t('Share screen')"
            :title="call.sharing ? t('Stop sharing') : t('Share screen')"
            @click="toggleShare"
          >
            <MonitorX v-if="call.sharing" :size="20" aria-hidden="true" />
            <MonitorUp v-else :size="20" aria-hidden="true" />
          </button>
          <button
            type="button"
            class="meeting-ctl"
            :class="{ on: call.hand }"
            data-control="hand"
            :aria-pressed="call.hand"
            :aria-label="call.hand ? t('Lower hand') : t('Raise hand')"
            :title="call.hand ? t('Lower hand') : t('Raise hand')"
            @click="toggleHand"
          >
            <Hand :size="20" aria-hidden="true" />
          </button>
          <div class="relative">
            <button
              type="button"
              class="meeting-ctl"
              data-control="reactions"
              :aria-expanded="menu === 'reactions'"
              aria-haspopup="menu"
              :aria-label="t('Send a reaction')"
              :title="t('Send a reaction')"
              @click="menu = menu === 'reactions' ? '' : 'reactions'"
            >
              <Smile :size="20" aria-hidden="true" />
            </button>
            <div v-if="menu === 'reactions'" class="select-pop meeting-pop meeting-reactions" role="menu">
              <button
                v-for="e in REACTIONS"
                :key="e"
                type="button"
                role="menuitem"
                class="emoji-btn"
                :aria-label="t('React with {emoji}', { emoji: e })"
                @click="sendReaction(e)"
              >
                {{ e }}
              </button>
            </div>
          </div>
          <div class="relative">
            <button
              type="button"
              class="meeting-ctl"
              data-control="more"
              :aria-expanded="menu === 'more'"
              aria-haspopup="menu"
              :aria-label="t('More options')"
              :title="t('More options')"
              @click="menu = menu === 'more' ? '' : 'more'"
            >
              <EllipsisVertical :size="20" aria-hidden="true" />
            </button>
            <ul v-if="menu === 'more'" class="select-pop meeting-pop" role="menu">
              <li role="none">
                <button type="button" role="menuitem" @click="togglePanel('people')">
                  <Users :size="15" aria-hidden="true" /> {{ t('People') }}
                </button>
              </li>
              <li role="none">
                <button type="button" role="menuitem" @click="fullscreen">
                  <Maximize :size="15" aria-hidden="true" /> {{ t('Full screen') }}
                </button>
              </li>
            </ul>
          </div>
          <button
            type="button"
            class="meeting-ctl leave"
            data-control="leave"
            :aria-label="t('Leave the meeting')"
            :title="t('Leave the meeting')"
            @click="hangUp"
          >
            <PhoneOff :size="20" aria-hidden="true" />
          </button>
        </div>
        <div class="meeting-bar-side">
          <button
            type="button"
            class="meeting-ctl small"
            :class="{ on: panel === 'people' }"
            :aria-pressed="panel === 'people'"
            :aria-label="t('People')"
            :title="t('People')"
            @click="togglePanel('people')"
          >
            <Users :size="18" aria-hidden="true" />
          </button>
          <button
            type="button"
            class="meeting-ctl small"
            :class="{ on: panel === 'chat' }"
            data-control="chat"
            :aria-pressed="panel === 'chat'"
            :aria-label="t('Chat')"
            :title="t('Chat')"
            @click="togglePanel('chat')"
          >
            <MessageSquare :size="18" aria-hidden="true" />
          </button>
        </div>
      </footer>
    </template>
  </div>
</template>

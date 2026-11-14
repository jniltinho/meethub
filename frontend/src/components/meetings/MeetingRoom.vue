<script setup lang="ts">
// The full-screen meeting room (meeting-room-ui spec, design D11 of the layout and D12, the AAA+ bar): the pre-join
// screen until connected, then the grid (or the stage of a shared screen with the people in a strip), the round
// controls centered at the bottom with themed tooltips and Meet's shortcuts (Ctrl/⌘+D microphone, Ctrl/⌘+E camera),
// and the channel's messages and people in a panel on the right. A modal dialog: focus stays inside, every state
// change is announced, and leaving returns focus where it was.
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
  WifiOff,
} from '@lucide/vue'
import MeetingPanel from './MeetingPanel.vue'
import MeetingTile from './MeetingTile.vue'
import PreJoin from './PreJoin.vue'
import {
  abort,
  call,
  type EndReason,
  join,
  leave,
  react,
  REACTIONS,
  setCam,
  setHand,
  setMic,
  setSharing,
  stopPreview,
  tiles,
} from './call'
import { ApiError } from '@/api/client'
import { announce } from '@/chat/announce'
import { formatTime } from '@/chat/format'
import { useChat } from '@/stores/chat'
import { useMeetings } from '@/stores/meetings'
import { askConfirm } from '@/feedback/dialogs'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'

const meetings = useMeetings()
const chat = useChat()
const id = meetings.callChannel
const channel = computed(() => chat.channels[id])
const name = computed(() => {
  const c = channel.value
  if (!c) return ''
  return c.type === 'D' || c.type === 'G' ? chat.conversationTitle(c) : '#' + c.display_name
})
const title = computed(() => t('Room of {name}', { name: name.value }))
const mac = /Mac|iPhone|iPad/.test(navigator.platform)
const mod = mac ? '⌘' : 'Ctrl'

const root = ref<HTMLElement | null>(null)
const audio = ref<HTMLElement | null>(null)
const busy = ref(false)
const joinError = ref('')
const width = ref(window.innerWidth)
const panel = ref<'' | 'chat' | 'people'>(window.innerWidth >= 1024 ? 'chat' : '')
const menu = ref<'' | 'reactions' | 'more'>('')
const now = ref(Date.now())
const clock = setInterval(() => (now.value = Date.now()), 15000)
const opener = document.activeElement as HTMLElement | null
const onResize = () => (width.value = window.innerWidth)

// the grid: rows centered, every tile the same size (so nothing moves while people speak, mute or raise a hand); a
// shared screen takes the stage and the people go to a strip
const screens = computed(() => tiles.value.filter((x) => x.screen))
const people = computed(() => tiles.value.filter((x) => !x.screen))
const narrow = computed(() => width.value < 768)
const cols = computed(() => {
  const n = Math.max(1, people.value.length)
  return narrow.value ? (n <= 2 ? 1 : 2) : Math.ceil(Math.sqrt(n))
})
const rows = computed(() => Math.ceil(Math.max(1, people.value.length) / cols.value))
const alone = computed(() => call.phase === 'joined' && people.value.length === 1 && screens.value.length === 0)

let alive = true
onMounted(async () => {
  window.addEventListener('keydown', onKey, true)
  window.addEventListener('resize', onResize)
  await nextTick()
  root.value?.querySelector<HTMLElement>('[data-autofocus]')?.focus()
})

onBeforeUnmount(() => {
  alive = false
  window.removeEventListener('keydown', onKey, true)
  window.removeEventListener('resize', onResize)
  clearInterval(clock)
  abort()
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

function peer(identity: string, joined: boolean) {
  const who = chat.displayName(identity) || t('Someone')
  const msg = joined ? t('{name} joined the meeting.', { name: who }) : t('{name} left the meeting.', { name: who })
  toast.info(msg)
  announce(msg)
}

function reconnecting(on: boolean) {
  if (on) toast.warning(t('Reconnecting to the meeting…'))
  else toast.success(t('Reconnected.'))
  announce(on ? t('Reconnecting to the meeting…') : t('Reconnected.'))
}

async function joinNow() {
  if (busy.value) return
  busy.value = true
  joinError.value = ''
  try {
    let tok
    try {
      tok = await meetings.token(id)
    } catch (e) {
      joinError.value = e instanceof ApiError ? errorMessage(e.code, e.message) : t('Could not join the meeting.')
      return
    }
    if (!alive) return // the pre-join screen closed while the token was on its way
    try {
      await join(tok.url, tok.token, audio.value!, { end: ended, peer, reconnecting })
    } catch {
      joinError.value = t('The meeting server could not be reached. Check your connection and try again.')
      return
    }
    if (!alive || call.phase !== 'joined') return
    announce(t('You joined {name}.', { name: title.value }))
    if (!call.mic) toast.info(t('You are muted. Press {key} to unmute.', { key: mod + '+D' }))
    await nextTick()
    root.value?.querySelector<HTMLElement>('[data-control="mic"]')?.focus()
  } finally {
    busy.value = false
  }
}

function back() {
  stopPreview()
  meetings.closeCall()
}

async function hangUp() {
  if (call.phase === 'idle') return back()
  if (
    call.sharing &&
    !(await askConfirm(t('You are presenting. Stop presenting and leave the meeting?'), {
      ok: t('Leave'),
      danger: true,
    }))
  )
    return
  await leave()
}

async function run(fn: () => Promise<void>, fallback: string, said?: () => string) {
  try {
    await fn()
    if (said) announce(said())
  } catch (e) {
    fail(e, fallback)
  }
}

const micSaid = () => (call.mic ? t('Microphone on.') : t('Microphone off.'))
const camSaid = () => (call.cam ? t('Camera on.') : t('Camera off.'))
const toggleMic = () => run(() => setMic(!call.mic), t('The microphone could not be changed.'), micSaid)
const toggleCam = () => run(() => setCam(!call.cam), t('The camera could not be changed.'), camSaid)
const toggleShare = () =>
  run(
    () => setSharing(!call.sharing),
    t('The screen could not be shared.'),
    () => (call.sharing ? t('You are presenting.') : t('You stopped presenting.')),
  )
const toggleHand = () =>
  run(
    () => setHand(!call.hand),
    t('Could not raise your hand.'),
    () => (call.hand ? t('Hand raised.') : t('Hand lowered.')),
  )
/** closeMenu closes a popup menu and gives focus back to its button (its focused item disappears) */
function closeMenu() {
  const which = menu.value
  menu.value = ''
  if (which) root.value?.querySelector<HTMLElement>(`[data-control="${which}"]`)?.focus()
}
function sendReaction(e: string) {
  closeMenu()
  void run(() => react(e), t('The reaction could not be sent.'))
}
function togglePanel(p: 'chat' | 'people') {
  closeMenu()
  panel.value = panel.value === p ? '' : p
}
function fullscreen() {
  closeMenu()
  if (document.fullscreenElement) void document.exitFullscreen().catch(() => undefined)
  else void root.value?.requestFullscreen?.().catch(() => undefined)
}

// the channel disappeared for this user (removed, archived, team left): the call ends with it
watch(channel, (c) => {
  if (!c) void (call.phase === 'idle' ? back() : leave())
})

// keyboard: Meet's shortcuts; Tab cycles inside the dialog (and brings back focus that fell out of it); Escape closes
// an open menu but never leaves the call. A themed confirmation dialog on top keeps its own focus.
async function shortcut(kind: 'mic' | 'cam') {
  if (kind === 'mic') await toggleMic()
  else await toggleCam()
  if (call.phase === 'joined') toast.info(kind === 'mic' ? micSaid() : camSaid())
}
function editable(el: EventTarget | null): boolean {
  return el instanceof HTMLElement && (el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName))
}
function onKey(e: KeyboardEvent) {
  if (document.querySelector('dialog[open]')) return
  const key = e.key.toLowerCase()
  if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && (key === 'd' || key === 'e') && !editable(e.target)) {
    e.preventDefault() // also keeps the browser's bookmark and search shortcuts away from the call
    // while connecting the devices are being published: a toggle now would say one thing and do another
    if (!busy.value && call.phase !== 'joining') void shortcut(key === 'd' ? 'mic' : 'cam')
    return
  }
  if (e.key === 'Escape') {
    closeMenu()
    return
  }
  if (e.key !== 'Tab' || !root.value) return
  if (!root.value.contains(document.activeElement)) {
    e.preventDefault()
    root.value.querySelector<HTMLElement>('button:not(:disabled)')?.focus()
    return
  }
  const all = [
    ...root.value.querySelectorAll<HTMLElement>(
      'button:not(:disabled), input, select, [tabindex]:not([tabindex="-1"])',
    ),
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
  >
    <div ref="audio" class="sr-only" aria-hidden="true" />

    <PreJoin
      v-if="call.phase !== 'joined'"
      :channel-id="id"
      :title="title"
      :busy="busy"
      :error="joinError"
      :shortcut="mod"
      @join="joinNow"
      @back="back"
    />

    <template v-else>
      <p v-if="call.reconnecting" class="meeting-banner" role="status">
        <WifiOff :size="16" aria-hidden="true" />{{ t('Reconnecting to the meeting…') }}
      </p>
      <div class="meeting-stage" :class="{ 'with-panel': panel }">
        <div v-if="screens.length" class="meeting-spotlight" :class="{ narrow }">
          <div class="meeting-share">
            <MeetingTile :tile="screens[0]!" />
          </div>
          <ul class="meeting-strip" :aria-label="t('Participants')">
            <li v-for="tile in [...screens.slice(1), ...people]" :key="tile.key">
              <MeetingTile :tile="tile" />
            </li>
          </ul>
        </div>
        <ul v-else class="meeting-grid" :style="{ '--cols': cols, '--rows': rows }" :aria-label="t('Participants')">
          <li v-for="tile in people" :key="tile.key">
            <MeetingTile :tile="tile" />
          </li>
        </ul>
        <p v-if="alone" class="meeting-alone" role="status">
          {{ t('You are the only one here. Members of {name} can join from the channel.', { name }) }}
        </p>
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
            :aria-keyshortcuts="mac ? 'Meta+D' : 'Control+D'"
            :data-tip="(call.mic ? t('Turn off microphone') : t('Turn on microphone')) + ' (' + mod + '+D)'"
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
            :aria-keyshortcuts="mac ? 'Meta+E' : 'Control+E'"
            :data-tip="(call.cam ? t('Turn off camera') : t('Turn on camera')) + ' (' + mod + '+E)'"
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
            :data-tip="call.sharing ? t('Stop sharing') : t('Share screen')"
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
            :data-tip="call.hand ? t('Lower hand') : t('Raise hand')"
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
              :data-tip="t('Send a reaction')"
              @click="menu = menu === 'reactions' ? '' : 'reactions'"
            >
              <Smile :size="20" aria-hidden="true" />
            </button>
            <div
              v-if="menu === 'reactions'"
              class="select-pop meeting-pop meeting-reactions"
              role="menu"
              :aria-label="t('Send a reaction')"
            >
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
              :data-tip="t('More options')"
              @click="menu = menu === 'more' ? '' : 'more'"
            >
              <EllipsisVertical :size="20" aria-hidden="true" />
            </button>
            <ul v-if="menu === 'more'" class="select-pop meeting-pop" role="menu" :aria-label="t('More options')">
              <li role="none">
                <button type="button" role="menuitem" @click="togglePanel('people')">
                  <Users :size="15" aria-hidden="true" /> {{ t('People') }}
                </button>
              </li>
              <li role="none">
                <button type="button" role="menuitem" @click="togglePanel('chat')">
                  <MessageSquare :size="15" aria-hidden="true" /> {{ t('Chat') }}
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
            :data-tip="t('Leave the meeting')"
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
            data-control="people"
            :aria-pressed="panel === 'people'"
            :aria-label="t('People')"
            :data-tip="t('People')"
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
            :data-tip="t('Chat')"
            @click="togglePanel('chat')"
          >
            <MessageSquare :size="18" aria-hidden="true" />
          </button>
        </div>
      </footer>
    </template>
  </div>
</template>

<script setup lang="ts">
// The user's own status at the foot of the sidebar and the menu that chooses it (presence spec, add-status-menu):
// Online follows activity; Away, Do not disturb (for a while or until changed) and Appear offline are kept by the
// server until another is chosen. The dot is never the only signal: the status is written beside the name.
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Check } from '@lucide/vue'
import { api, ApiError } from '@/api/client'
import type { Presence, Status } from '@/chat/types'
import { announcement } from '@/chat/announce'
import { toast } from '@/feedback/toasts'
import { formatTime } from '@/chat/format'
import { errorMessage, t } from '@/i18n'
import { useChat } from '@/stores/chat'
import { useSession } from '@/stores/session'
import PresenceDot from './PresenceDot.vue'
import UserPicture from './UserPicture.vue'

const session = useSession()
const chat = useChat()
const me = computed(() => session.profile?.user.id ?? '')
const mine = ref<Status | null>(null)
const open = ref(false)
const picking = ref(false) // the durations of do not disturb are shown
const busy = ref(false)
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)

const current = computed<Presence>(() => mine.value?.status ?? chat.statuses[me.value] ?? 'offline')
// what the menu marks: a chosen status, or Online when the status follows activity
const chosen = computed<Presence>(() => (mine.value?.manual ? mine.value.status : 'online'))
const names = computed<Record<Presence, string>>(() => ({
  online: t('Online'),
  away: t('Away'),
  dnd: t('Do not disturb'),
  offline: t('Offline'),
}))
const label = computed(() => {
  const end = mine.value?.dnd_end_time
  if (current.value === 'dnd' && end) return t('Do not disturb until {time}', { time: formatTime(end) })
  return names.value[current.value]
})
const options = computed<{ id: Presence; label: string; hint: string }[]>(() => [
  { id: 'online', label: t('Online'), hint: t('Follows your activity.') },
  { id: 'away', label: t('Away'), hint: t('Shown as away until you change it.') },
  { id: 'dnd', label: t('Do not disturb'), hint: t('For a while, or until you change it.') },
  { id: 'offline', label: t('Appear offline'), hint: t('You still receive everything.') },
])
const durations = computed(() => [
  { id: '30m', label: t('30 minutes'), until: () => Date.now() + 30 * 60_000 },
  { id: '1h', label: t('1 hour'), until: () => Date.now() + 60 * 60_000 },
  { id: '2h', label: t('2 hours'), until: () => Date.now() + 120 * 60_000 },
  { id: 'tomorrow', label: t('Until tomorrow'), until: tomorrow },
  { id: 'forever', label: t('Until I change it'), until: () => 0 },
])
// 08:00 of the next day, in the user's own time
function tomorrow(): number {
  const d = new Date()
  d.setDate(d.getDate() + 1)
  d.setHours(8, 0, 0, 0)
  return d.getTime()
}

async function load() {
  if (!me.value) return
  try {
    const list = await api<Status[] | null>('POST', '/users/status/ids', [me.value])
    mine.value = list?.[0] ?? null
  } catch {
    /* the dot falls back to what the store knows */
  }
}
onMounted(load)
// another tab, or a do not disturb that ended: the server says the status changed, and how is read again
watch(
  () => chat.statuses[me.value],
  (s) => {
    if (s && s !== mine.value?.status) void load()
  },
)

async function set(status: Presence, until = 0) {
  if (busy.value) return
  busy.value = true
  try {
    const body = status === 'dnd' && until ? { status, dnd_end_time: until } : { status }
    mine.value = await api<Status>('PUT', '/users/me/status', body)
    chat.statuses[me.value] = mine.value.status
    announcement.value = t('Your status is now: {status}', { status: label.value })
    close(true)
  } catch (e) {
    toast.error(e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.'))
  } finally {
    busy.value = false
  }
}
function choose(id: Presence) {
  if (id === 'dnd') picking.value = true
  else void set(id)
}

async function toggle() {
  open.value = !open.value
  picking.value = false
  if (open.value) {
    await nextTick()
    root.value?.querySelector<HTMLElement>('[role="radio"][aria-checked="true"]')?.focus()
  }
}
function close(refocus = false) {
  if (!open.value) return
  open.value = false
  picking.value = false
  if (refocus) trigger.value?.focus()
}
function outside(e: Event) {
  if (open.value && root.value && !root.value.contains(e.target as Node)) close()
}
onMounted(() => document.addEventListener('pointerdown', outside))
onBeforeUnmount(() => document.removeEventListener('pointerdown', outside))
</script>

<template>
  <div ref="root" class="status-menu" @keydown.esc.stop="close(true)">
    <button
      ref="trigger"
      type="button"
      class="status-btn"
      :aria-label="t('Your status: {status}. Change it', { status: label })"
      :title="session.profile?.user.email"
      :aria-expanded="open"
      aria-haspopup="true"
      data-status-button
      :data-status="current"
      @click="toggle"
    >
      <span class="status-pic">
        <UserPicture
          v-if="session.profile"
          :user-id="session.profile.user.id"
          :name="session.profile.user.display_name"
          :size="24"
        />
        <PresenceDot :status="current" />
      </span>
      <span class="min-w-0 flex-1">
        <span class="block truncate">{{ session.profile?.user.display_name }}</span>
        <span class="status-text block truncate" data-status-text>{{ label }}</span>
      </span>
    </button>
    <div v-if="open" class="select-pop status-pop" role="group" :aria-label="t('Your status')" data-status-pop>
      <div role="radiogroup" :aria-label="t('Your status')">
        <button
          v-for="o in options"
          :key="o.id"
          type="button"
          role="radio"
          class="status-item"
          :aria-checked="chosen === o.id"
          :disabled="busy"
          :data-status-option="o.id"
          @click="choose(o.id)"
        >
          <PresenceDot :status="o.id === 'offline' ? 'offline' : o.id" />
          <span class="min-w-0 flex-1">
            <span class="block font-semibold">{{ o.label }}</span>
            <span class="block">{{ o.hint }}</span>
          </span>
          <Check v-if="chosen === o.id" :size="14" aria-hidden="true" />
        </button>
      </div>
      <div
        v-if="picking"
        class="status-durations"
        role="group"
        :aria-label="t('Do not disturb for')"
        data-status-durations
      >
        <p class="lbl">{{ t('Do not disturb for') }}</p>
        <button
          v-for="d in durations"
          :key="d.id"
          type="button"
          class="status-item"
          :disabled="busy"
          :data-status-duration="d.id"
          @click="set('dnd', d.until())"
        >
          {{ d.label }}
        </button>
      </div>
    </div>
  </div>
</template>

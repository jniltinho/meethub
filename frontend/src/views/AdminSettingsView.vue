<script setup lang="ts">
// Administration → Settings (admin-settings spec, add-admin-settings): the allowlisted config.toml keys by group with
// their value, default and source, a lock on keys set by the environment, Restore default, the pending changes, a
// themed confirmation before "Apply and restart", the progress of the restart, "Keep these settings" with the time
// left, the result, and every other setting read-only with its reason. Each group, the meeting summaries setting
// and the read-only values are sub-tabs, the open one in the address (?tab=…).
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { Eye, FileText, KeyRound, Lock, Paperclip, ScrollText, Timer, Video } from '@lucide/vue'
import { api, ApiError } from '@/api/client'
import { formatSize } from '@/chat/format'
import UiAlert from '@/components/ui/UiAlert.vue'
import { askConfirm } from '@/feedback/dialogs'
import { toast } from '@/feedback/toasts'
import { errorMessage, formatDateTime, t } from '@/i18n'
import { useSession } from '@/stores/session'
import AdminSummariesView from '@/views/AdminSummariesView.vue'
import { durationNanos } from '@/views/settings'

type Value = number | string | boolean
interface Key {
  key: string
  group: string
  kind: 'int' | 'duration' | 'string' | 'choice' | 'bool'
  min?: number
  max?: number
  choices?: string[]
  value: Value
  default: Value
  source: 'environment' | 'file' | 'default'
  env: string
  locked: boolean
}
interface ReadOnly {
  key: string
  value?: unknown
  secret?: boolean
  set?: boolean
  env: string
  source: string
  reason: string
}
interface Result {
  id: string
  outcome: 'applied' | 'rolled_back' | 'refused'
  reason?: string
  keys?: string[]
  at: string
}
interface Pending {
  id: string
  phase: 'waiting' | 'applying' | 'confirming' | 'rolling_back'
  keys: Record<string, { old: Value; new: Value }>
  mine: boolean
  can_confirm: boolean
  seconds_left?: number
}
interface View {
  keys: Key[]
  read_only: ReadOnly[]
  applier: { available: boolean; reason?: 'no_file' | 'no_applier' | 'other_file' }
  pending?: Pending
  last_result?: Result
  host: string
  replicas?: string[]
}

const view = ref<View | null>(null)
const draft = reactive<Record<string, string>>({})
const restore = reactive(new Set<string>())
const busy = ref(false)
const watching = ref('') // the request this page follows to its result
const secondsLeft = ref(0)
const unreachable = ref(false)
let poll: ReturnType<typeof setInterval> | undefined
let tick: ReturnType<typeof setInterval> | undefined

const groups = computed(() => [
  { id: 'files', title: t('Files'), icon: Paperclip },
  { id: 'sessions', title: t('Sessions'), icon: Timer },
  { id: 'sso', title: t('Single sign-on'), icon: KeyRound },
  { id: 'logging', title: t('Logging'), icon: ScrollText },
  { id: 'recordings', title: t('Recordings'), icon: Video },
])
// the sub-tabs: the editable groups, then the meeting summaries setting and the read-only values
const tabs = computed(() => [
  ...groups.value,
  { id: 'summaries', title: t('Meeting summaries'), icon: FileText },
  { id: 'read-only', title: t('Read-only'), icon: Eye },
])
const route = useRoute()
const tab = computed(() => (tabs.value.some((x) => x.id === route.query.tab) ? String(route.query.tab) : 'files'))
const group = computed(() => groups.value.find((g) => g.id === tab.value))

function label(key: string): string {
  return (
    {
      'files.max_size_bytes': t('Largest file (bytes)'),
      'files.max_per_post': t('Files per message'),
      'session.idle_timeout': t('Logged out after inactivity'),
      'session.absolute_lifetime': t('Longest session'),
      'oidc.name': t('Single sign-on button label'),
      'log.level': t('Log level'),
      'log.format': t('Log format'),
      'recordings.enabled': t('Meeting recordings'),
      'recordings.mode': t('What is recorded'),
      'recordings.video_format': t('Video file format'),
      'recordings.start': t('Who may record'),
      'recordings.max_minutes': t('Longest recording (minutes)'),
      'recordings.max_bytes': t('Largest recording (bytes)'),
      'recordings.max_concurrent': t('Recordings at the same time'),
    }[key] ?? key
  )
}

function reasonText(reason: string): string {
  return (
    {
      destination: t("It names where secrets or people's data are sent: it is changed on the server only."),
      trust: t('It decides whose forwarded addresses are believed: no check from the browser can prove that safe.'),
      lockout: t('A mistake here could lock this page out: it is changed on the server only.'),
      data: t('It says where the data is: it is changed on the server only.'),
      irreversible: t('What it causes cannot be taken back by a rollback.'),
      secret: t('Secrets come only from the environment and are never shown.'),
    }[reason] ?? t('Not on this page: change it in config.toml.')
  )
}

const canEdit = computed(() => !!view.value?.applier.available && !view.value.pending && !busy.value)

function shown(k: Key): string {
  return String(k.value)
}

function changed(k: Key): boolean {
  return restore.has(k.key) || draft[k.key] !== shown(k)
}

const pendingKeys = computed(() => view.value?.keys.filter((k) => !k.locked && changed(k)) ?? [])
// a change waiting in a tab that is not open is still counted on its tab
const pendingIn = (id: string) => pendingKeys.value.filter((k) => k.group === id).length

// problem says what is wrong with the draft of k, in the reader's language; "" when it is fine
function problem(k: Key): string {
  const v = (draft[k.key] ?? '').trim()
  if (k.kind === 'int') {
    const n = Number(v)
    if (!/^\d+$/.test(v) || n < (k.min ?? 0) || n > (k.max ?? Infinity))
      return t('Enter a whole number from {min} to {max}.', { min: k.min ?? 0, max: k.max ?? 0 })
  }
  if (k.kind === 'duration') {
    const n = durationNanos(v)
    if (n === null || n < (k.min ?? 0) || n > (k.max ?? Infinity))
      return t('Enter a duration such as 30m or 168h, from {min} to {max}.', {
        min: humanDuration(k.min ?? 0),
        max: humanDuration(k.max ?? 0),
      })
    if (k.key === 'session.absolute_lifetime') {
      const idle = view.value?.keys.find((x) => x.key === 'session.idle_timeout')
      const idleNs = idle ? durationNanos((draft[idle.key] ?? '').trim()) : null
      if (idleNs !== null && n < idleNs) return t('The longest session cannot be shorter than the inactivity time.')
    }
  }
  if (k.kind === 'string' && (v.length < (k.min ?? 1) || [...v].length > (k.max ?? 40)))
    return t('Enter 1 to {max} characters.', { max: k.max ?? 40 })
  return ''
}

const invalid = computed(() => pendingKeys.value.some((k) => problem(k) !== ''))

function humanDuration(ns: number): string {
  const h = ns / 3.6e12
  if (h >= 24 && h % 24 === 0) return t('{n} days', { n: h / 24 })
  if (h >= 1) return t('{n} hours', { n: h })
  return t('{n} minutes', { n: ns / 6e10 })
}

function sourceText(k: Key): string {
  if (k.source === 'environment') return t('Set by {env}', { env: k.env })
  if (k.source === 'file') return t('From config.toml')
  return t('Not in config.toml')
}

function resetDraft(v: View) {
  for (const k of v.keys) draft[k.key] = shown(k)
  restore.clear()
}

async function load(quiet = false) {
  try {
    const v = await api<View>('GET', '/admin/settings')
    unreachable.value = false
    const first = !view.value
    const wasPending = !!view.value?.pending
    view.value = v
    secondsLeft.value = v.pending?.seconds_left ?? 0
    if (first || (wasPending && !v.pending)) resetDraft(v)
    // the upload limits this tab checks before sending come from the bootstrap: read them again after a change
    if (wasPending && !v.pending)
      void useSession()
        .loadBootstrap()
        .catch(() => undefined)
    if (v.pending && !watching.value && v.pending.mine) watching.value = v.pending.id
    schedule()
  } catch (e) {
    if (e instanceof ApiError && e.code === 'network' && quiet) {
      unreachable.value = true // the server is restarting
      return
    }
    toast.error(failure(e))
  }
}

function failure(e: unknown): string {
  return e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')
}

// schedule polls while a change is in progress, and counts the time left down every second
function schedule() {
  const active = !!view.value?.pending
  if (active && !poll) poll = setInterval(() => void load(true), 2000)
  if (!active && poll) {
    clearInterval(poll)
    poll = undefined
  }
  if (!tick) tick = setInterval(() => (secondsLeft.value = Math.max(0, secondsLeft.value - 1)), 1000)
}

onMounted(() => void load())
onBeforeUnmount(() => {
  clearInterval(poll)
  clearInterval(tick)
})

function restoreDefault(k: Key) {
  draft[k.key] = String(k.default)
  restore.add(k.key)
}

function edited(k: Key) {
  restore.delete(k.key)
}

function discard() {
  if (view.value) resetDraft(view.value)
}

function display(k: Key, v: Value): string {
  if (k.key.endsWith('_bytes')) return `${v} (${formatSize(Number(v))})`
  if (k.kind === 'bool') return String(v) === 'true' ? t('On') : t('Off')
  return String(v)
}

// the options of a list: a setting that is on or off is one too, so every value on the page is a text draft
function options(k: Key): { value: string; text: string }[] {
  if (k.kind === 'bool')
    return [
      { value: 'true', text: t('On') },
      { value: 'false', text: t('Off') },
    ]
  return (k.choices ?? []).map((c) => ({ value: c, text: c }))
}

async function apply() {
  const v = view.value
  if (!v || !pendingKeys.value.length || invalid.value || busy.value) return
  const names = pendingKeys.value.map((k) => `${label(k.key)}: ${display(k, draft[k.key]!.trim())}`).join('; ')
  const shorter = pendingKeys.value.some(
    (k) => k.group === 'sessions' && (durationNanos(draft[k.key]!.trim()) ?? 0) < (durationNanos(String(k.value)) ?? 0),
  )
  let message = t(
    'Apply {names}? The server restarts (a few seconds); after it, keep the new settings within two minutes or they are undone.',
    { names },
  )
  if (shorter) message += ' ' + t('People inactive for longer than the new time are logged out at the restart.')
  if (!(await askConfirm(message, { title: t('Apply and restart'), ok: t('Apply and restart') }))) return
  const changes: Record<string, Value> = {}
  for (const k of pendingKeys.value) {
    if (restore.has(k.key) && draft[k.key] === String(k.default)) continue
    const d = draft[k.key]!.trim()
    changes[k.key] = k.kind === 'int' ? Number(d) : k.kind === 'bool' ? d === 'true' : d
  }
  const restored = pendingKeys.value.filter((k) => restore.has(k.key) && draft[k.key] === String(k.default))
  busy.value = true
  try {
    const nv = await api<View>('PUT', '/admin/settings', { changes, restore: restored.map((k) => k.key) })
    view.value = nv
    watching.value = nv.pending?.id ?? ''
    schedule()
  } catch (e) {
    toast.error(failure(e))
  } finally {
    busy.value = false
  }
}

async function keep() {
  const p = view.value?.pending
  if (!p || busy.value) return
  busy.value = true
  try {
    await api('POST', '/admin/settings/confirm', { id: p.id })
    toast.success(t('Confirmed: the applier keeps the new settings.'))
    await load(true)
  } catch (e) {
    toast.error(failure(e))
  } finally {
    busy.value = false
  }
}

const progress = computed(() => {
  const p = view.value?.pending
  if (!p) return ''
  if (unreachable.value) return t('The server is restarting…')
  return {
    waiting: t('Waiting for the settings applier…'),
    applying: t('Applying the change and restarting the server…'),
    confirming: p.can_confirm
      ? t('The server restarted with the new settings. Keep them? Without a confirmation they are undone.')
      : t('Waiting for the owner who made the change to confirm it.'),
    rolling_back: t('Putting the previous settings back and restarting the server…'),
  }[p.phase]
})

const result = computed(() => {
  const r = view.value?.last_result
  if (!r || view.value?.pending) return null
  const text = {
    applied: t('The settings were applied and kept.'),
    rolled_back: t('The change was undone: {reason}', { reason: r.reason ?? '' }),
    refused: t('The change was refused: {reason}', { reason: r.reason ?? '' }),
  }[r.outcome]
  return { kind: r.outcome === 'applied' ? 'success' : 'warning', text, at: r.at, mine: r.id === watching.value }
})

const readOnlyNote = computed(() => {
  const a = view.value?.applier
  if (!a || a.available) return ''
  return (
    {
      no_file: t('This server runs without a config.toml: its settings cannot be changed here.'),
      other_file: t('The settings applier of this host serves another config.toml or data directory.'),
    }[a.reason as string] ??
    t(
      'No settings applier runs on this host: every value is read-only. Container images are configured by environment variables; on a Linux package install, check meethub-settings.path.',
    )
  )
})
</script>

<template>
  <div class="max-w-4xl">
    <h1 class="page-title">{{ t('Settings') }}</h1>
    <nav class="subtabs" :aria-label="t('Settings')" data-settings-tabs>
      <!-- custom: only the query changes, so a plain router-link would mark every tab as the current page -->
      <router-link v-for="x in tabs" :key="x.id" v-slot="{ href, navigate }" :to="{ query: { tab: x.id } }" custom>
        <a
          :href="href"
          class="subtab"
          :class="{ on: tab === x.id }"
          :aria-current="tab === x.id ? 'page' : undefined"
          :data-tab="x.id"
          @click="navigate"
        >
          <component :is="x.icon" :size="13" aria-hidden="true" /> {{ x.title }}
          <span v-if="pendingIn(x.id)" class="chat-badge" data-tab-pending>
            <span aria-hidden="true">{{ pendingIn(x.id) }}</span>
            <span class="sr-only">{{
              pendingIn(x.id) === 1 ? t('1 pending change') : t('{n} pending changes', { n: pendingIn(x.id) })
            }}</span>
          </span>
        </a>
      </router-link>
    </nav>
    <p v-if="group" class="mb-3 text-sm text-fg-muted">
      {{
        t(
          'These settings are saved in config.toml, keeping its comments, and take effect when the server restarts. The previous file is kept; without your confirmation after the restart it is put back.',
        )
      }}
    </p>
    <UiAlert v-if="group && readOnlyNote" kind="info" class="mb-3" :title="t('Read-only')" data-settings-readonly>
      {{ readOnlyNote }}
    </UiAlert>
    <UiAlert v-if="group && view?.replicas?.length" kind="warning" class="mb-3" data-settings-replicas>
      {{
        t(
          'Several servers share this database ({hosts}). A change is applied only on {host}, the one serving this page: manage config.toml from outside on such installs.',
          { hosts: view.replicas.join(', '), host: view.host },
        )
      }}
    </UiAlert>

    <div v-if="view?.pending" class="mb-3" aria-live="polite" data-settings-progress :data-phase="view.pending.phase">
      <UiAlert :kind="view.pending.phase === 'rolling_back' ? 'warning' : 'info'" :title="t('A change is in progress')">
        <p>{{ progress }}</p>
        <ul class="mt-1 list-disc pl-5">
          <li v-for="(c, key) in view.pending.keys" :key="key">{{ label(String(key)) }}: {{ c.old }} → {{ c.new }}</li>
        </ul>
        <div v-if="view.pending.phase === 'confirming'" class="mt-2 flex flex-wrap items-center gap-2">
          <button
            v-if="view.pending.can_confirm"
            type="button"
            class="btn"
            :disabled="busy"
            data-settings-keep
            @click="keep"
          >
            {{ t('Keep these settings') }}
          </button>
          <span class="text-sm" data-settings-countdown>{{ t('{n} s left', { n: secondsLeft }) }}</span>
        </div>
      </UiAlert>
    </div>
    <div v-else-if="result" class="mb-3" aria-live="polite">
      <UiAlert
        :kind="result.kind as 'success' | 'warning'"
        data-settings-result
        :data-outcome="view?.last_result?.outcome"
      >
        {{ result.text }}
        <span class="block text-sm text-fg-muted">{{ formatDateTime(result.at) }}</span>
      </UiAlert>
    </div>

    <form v-if="view && group" class="grid gap-4" @submit.prevent="apply">
      <section class="card" :aria-labelledby="`grp-${group.id}`">
        <h2 :id="`grp-${group.id}`" class="card-h">{{ group.title }}</h2>
        <div class="grid gap-4 p-3">
          <div v-for="k in view.keys.filter((x) => x.group === group!.id)" :key="k.key" :data-setting="k.key">
            <label class="lbl" :for="`set-${k.key}`">{{ label(k.key) }}</label>
            <div class="flex flex-wrap items-center gap-2">
              <select
                v-if="k.kind === 'choice' || k.kind === 'bool'"
                :id="`set-${k.key}`"
                v-model="draft[k.key]"
                class="inp w-auto min-w-28"
                :disabled="!canEdit || k.locked"
                :aria-describedby="`set-${k.key}-info`"
                @change="edited(k)"
              >
                <option v-for="o in options(k)" :key="o.value" :value="o.value">{{ o.text }}</option>
              </select>
              <input
                v-else
                :id="`set-${k.key}`"
                v-model="draft[k.key]"
                class="inp max-w-xs"
                :inputmode="k.kind === 'int' ? 'numeric' : undefined"
                :maxlength="k.kind === 'string' ? k.max : undefined"
                :disabled="!canEdit || k.locked"
                :aria-invalid="changed(k) && problem(k) !== '' ? 'true' : undefined"
                :aria-describedby="`set-${k.key}-info${changed(k) && problem(k) ? ` set-${k.key}-err` : ''}`"
                @input="edited(k)"
              />
              <button
                type="button"
                class="btn-ghost"
                :disabled="!canEdit || k.locked || draft[k.key] === String(k.default)"
                data-restore
                @click="restoreDefault(k)"
              >
                {{ t('Restore default') }}
              </button>
            </div>
            <p :id="`set-${k.key}-info`" class="mt-1 flex flex-wrap items-center gap-x-3 text-sm text-fg-muted">
              <span v-if="k.locked" class="inline-flex items-center gap-1" data-locked>
                <Lock :size="14" aria-hidden="true" />{{ sourceText(k) }}
              </span>
              <span v-else>{{ sourceText(k) }}</span>
              <span v-if="String(k.value) === String(k.default)" data-is-default>{{ t('Default') }}</span>
              <span v-else>{{ t('Default: {value}', { value: display(k, k.default) }) }}</span>
              <span v-if="k.key.endsWith('_bytes')">{{ formatSize(Number(draft[k.key]) || 0) }}</span>
            </p>
            <p v-if="changed(k) && problem(k)" :id="`set-${k.key}-err`" class="field-error" role="alert">
              {{ problem(k) }}
            </p>
          </div>
        </div>
      </section>

      <div v-if="view.applier.available" class="flex flex-wrap items-center gap-2" data-settings-bar>
        <span class="text-sm" aria-live="polite" data-pending-count>
          {{ pendingKeys.length === 1 ? t('1 pending change') : t('{n} pending changes', { n: pendingKeys.length }) }}
        </span>
        <button type="submit" class="btn" :disabled="!canEdit || !pendingKeys.length || invalid" data-settings-apply>
          {{ t('Apply and restart') }}
        </button>
        <button type="button" class="btn-ghost" :disabled="!pendingKeys.length || busy" @click="discard">
          {{ t('Discard') }}
        </button>
      </div>
    </form>

    <section v-if="view && tab === 'read-only'" aria-labelledby="ro-title">
      <h2 id="ro-title" class="mb-2 text-lg font-semibold">{{ t('Read-only settings') }}</h2>
      <!-- focusable: the table may scroll sideways on a narrow screen -->
      <div class="overflow-x-auto" tabindex="0" role="region" aria-labelledby="ro-title">
        <table class="grid-t w-full" data-settings-readonly-list>
          <thead>
            <tr>
              <th>{{ t('Setting') }}</th>
              <th>{{ t('Value') }}</th>
              <th>{{ t('Why it is read-only') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r in view.read_only" :key="r.key" :data-key="r.key">
              <td>
                <code>{{ r.key }}</code>
              </td>
              <td>
                <template v-if="r.secret">{{ r.set ? t('Set') : t('Not set') }}</template>
                <template v-else>{{ Array.isArray(r.value) ? r.value.join(', ') : String(r.value ?? '') }}</template>
                <span v-if="r.source === 'environment'" class="block text-sm text-fg-muted">{{
                  t('Set by {env}', { env: r.env })
                }}</span>
              </td>
              <td class="text-sm">{{ reasonText(r.reason) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section v-if="tab === 'summaries'" data-settings-summaries>
      <AdminSummariesView embedded />
    </section>
  </div>
</template>

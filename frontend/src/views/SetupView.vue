<script setup lang="ts">
// The setup wizard (setup-wizard spec, add-setup-wizard): Access (the token) → This server (a diagnosis) →
// Organization and account → Meetings and recordings → AI summaries → Review → Finish. The account is created at
// the third step exactly as the old setup form did; the steps after it are the owner's, through the routes of
// Administration → Settings, so nothing is configured with the setup token. Where the settings page cannot write
// config.toml the review shows the lines to copy instead. An owner who opens /setup later starts at the fourth step.
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  ArrowLeft,
  ArrowRight,
  Building2,
  Check,
  Circle,
  CircleAlert,
  CircleCheck,
  Copy,
  FileText,
  KeyRound,
  ListChecks,
  LoaderCircle,
  Lock,
  RefreshCw,
  Rocket,
  Server,
  Video,
} from '@lucide/vue'
import { api, ApiError } from '@/api/client'
import AuthFrame from '@/components/AuthFrame.vue'
import ReauthFields from '@/components/ReauthFields.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'
import type { SummaryPolicy } from '@/stores/meetings'
import { useSession } from '@/stores/session'
import { hint, label } from '@/views/settings'
import {
  addressProblem,
  blocked,
  canGo,
  FIRST_OPTIONAL,
  passwordProblem,
  STEPS,
  tomlLines,
  type DiagnosisItem,
  type Step,
  type Value,
} from '@/views/setupRules'

interface Key {
  key: string
  kind: 'int' | 'duration' | 'string' | 'choice' | 'bool'
  choices?: string[]
  value: Value
  env: string
  locked: boolean
  sensitive?: boolean
}
interface Pending {
  id: string
  phase: 'waiting' | 'applying' | 'confirming' | 'rolling_back'
  can_confirm: boolean
  seconds_left?: number
}
interface Settings {
  keys: Key[]
  applier: { available: boolean }
  pending?: Pending
  last_result?: { id: string; outcome: 'applied' | 'rolled_back' | 'refused'; reason?: string }
}
interface AIKey {
  key: string
  set: boolean
  env: string
  locked: boolean
}

const session = useSession()
const route = useRoute()
const router = useRouter()

const step = ref<Step>('access')
const busy = ref(false)
const error = ref('')
const heading = ref<HTMLElement | null>(null)
const titles = computed<Record<Step, string>>(() => ({
  access: t('Access'),
  server: t('This server'),
  account: t('Organization and account'),
  recordings: t('Meetings and recordings'),
  summaries: t('AI summaries'),
  review: t('Review'),
  finish: t('Finish'),
}))
// one friendly line under each step's heading
const leads = computed<Record<Step, string>>(() => ({
  access: t('Welcome! A few steps and MeetHub is ready for your team. First, let us make sure it is you.'),
  server: t('A quick look at what this server already has, and what it still lacks.'),
  account: t('Name your organization and create your own account: the owner.'),
  recordings: t('Optional: let people record their meetings.'),
  summaries: t('Optional: let an AI service write the minutes of recorded meetings.'),
  review: t('Check what will be done before anything changes.'),
  finish: t('Almost there.'),
}))
const icons: Record<Step, typeof Check> = {
  access: KeyRound,
  server: Server,
  account: Building2,
  recordings: Video,
  summaries: FileText,
  review: ListChecks,
  finish: Rocket,
}
const jobIcon = { waiting: Circle, running: LoaderCircle, done: CircleCheck, failed: CircleAlert }
const initialized = computed(() => !session.bootstrap?.setup_required)
const running = ref(false) // Finish started: nothing before it can be changed any more

const form = reactive({
  setup_token: typeof route.query.token === 'string' ? route.query.token : '',
  organization_name: '',
  display_name: '',
  email: '',
  password: '',
  again: '',
})
const items = ref<DiagnosisItem[]>([])
const settings = ref<Settings | null>(null)
const aiKey = ref<AIKey | null>(null)
const storable = ref(false)
const draft = reactive<Record<string, string>>({})
const newKey = ref('')
const policy = ref<SummaryPolicy>('off')
const password = ref('')
const code = ref('')
const proven = ref(false)

function failure(e: unknown): string {
  return e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')
}

async function go(to: Step) {
  if (step.value === 'account') form.password = form.again = '' // typed again when the step is reopened
  step.value = to
  error.value = ''
  await nextTick()
  heading.value?.focus() // the new step is announced, and the keyboard starts in it
}
const back = () => go(STEPS[STEPS.indexOf(step.value) - 1]!)

// ── 1 and 2: the token, then the diagnosis it opens ─────────────────────────────────────────────────────────
const commands = [
  { where: () => t('Linux package'), text: 'sudo cat /opt/meethub/data/setup-token' },
  { where: () => t('Container'), text: 'docker logs meethub 2>&1 | grep -i "setup token"' },
]
async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast.success(t('Copied.'))
  } catch {
    toast.error(t('Could not copy: select the text and copy it by hand.'))
  }
}

async function diagnose(): Promise<boolean> {
  busy.value = true
  error.value = ''
  try {
    const r = initialized.value
      ? await api<{ items: DiagnosisItem[] }>('GET', '/admin/diagnosis')
      : await api<{ items: DiagnosisItem[] }>('POST', '/auth/setup/diagnosis', { setup_token: form.setup_token.trim() })
    items.value = r.items
    return true
  } catch (e) {
    // a request from another address than http.public_url is refused before the token is even read
    error.value =
      e instanceof ApiError && e.code === 'csrf_failed'
        ? t(
            'This server expects to be opened at another address. Set public_url under [http] in config.toml (or MEETHUB_HTTP_PUBLIC_URL) to the address in your browser, {origin}, and restart it.',
            { origin: window.location.origin },
          )
        : failure(e)
    return false
  } finally {
    busy.value = false
  }
}
async function access() {
  if (await diagnose()) await go('server')
}

const part = (id: string) => items.value.find((i) => i.id === id)
const isOK = (id: string) => part(id)?.level === 'ok'
const wrong = computed(() => items.value.filter((i) => i.level !== 'ok'))
const fine = computed(() => items.value.filter((i) => i.level === 'ok'))

function partTitle(i: DiagnosisItem): string {
  return (
    {
      database: t('Database ({kind})', { kind: i.detail ?? '' }),
      redis: t('Redis'),
      storage: t('File storage ({kind})', { kind: i.detail ?? '' }),
      https: t('HTTPS'),
      mode: t('Production mode'),
      meetings: t('Meetings (LiveKit)'),
      recordings: t('Meeting recordings'),
      email: t('Notification e-mails'),
      secret_key: t('Key for stored secrets'),
      ai: t('AI summaries'),
      applier: t('Settings from the browser'),
    }[i.id] ?? i.id
  )
}
// what is wrong with a part and how to fix it, by its level
function partText(i: DiagnosisItem): string {
  const texts: Record<string, string> = {
    'database.problem': t(
      'The database does not answer. Check [database] in config.toml and that the database server runs, then check again.',
    ),
    'redis.problem': t('Redis does not answer. Start Redis (or Valkey) and check addr under [redis] in config.toml.'),
    'storage.problem': t(
      'The file storage cannot be used. Check that the data directory can be written, or the [files.s3] settings.',
    ),
    'https.warning': t(
      'People reach this server over plain HTTP: passwords and messages travel unencrypted. Serve HTTPS (the guides "Standalone with HTTPS" or "MeetHub behind nginx") and set public_url to the https:// address.',
    ),
    'mode.note': t(
      'Development mode. For a real install set env = "production" under [app]; it requires an https:// public address.',
    ),
    'meetings.note': t(
      'Meetings are off: no LiveKit server is configured. Set url under [livekit] and the key and secret (MEETHUB_LIVEKIT_API_KEY, MEETHUB_LIVEKIT_API_SECRET).',
    ),
    'meetings.warning': t(
      'LiveKit is configured but does not answer with this key. Run "meethub meetings check" on the server to see which part fails.',
    ),
    'recordings.note': t(
      'Meeting recordings are off. Turn them on in the next steps, or later in Administration → Settings → Recordings.',
    ),
    'email.note': t(
      'No e-mail is sent: no SMTP server is configured. Set host and from under [smtp], and MEETHUB_SMTP_PASSWORD.',
    ),
    'secret_key.warning': t(
      'MEETHUB_SECRET_KEY is not set: two-factor authentication and AI keys entered in the browser are unavailable. Generate one with "openssl rand -base64 32" and set it in the environment.',
    ),
    'ai.note': t(
      'Meeting summaries are not set up. Set them up in the next steps, or later in Administration → Settings → Meeting summaries.',
    ),
    'applier.note': t(
      'Settings cannot be changed from the browser on this install: config.toml and the environment are changed on the server. A container can opt in (the guide "Settings in Administration").',
    ),
  }
  return texts[`${i.id}.${i.level}`] ?? ''
}
const alertKind = (i: DiagnosisItem) =>
  ({ note: 'info', warning: 'warning', problem: 'error', ok: 'success' })[i.level] as 'info'

// ── 3: the organization and its owner, as the setup always did ──────────────────────────────────────────────
const again = ref<HTMLInputElement | null>(null)
function checkAgain() {
  again.value?.setCustomValidity(
    passwordProblem(form.password, form.again) === 'mismatch' ? t('The two passwords are not the same.') : '',
  )
}
async function createAccount() {
  if (passwordProblem(form.password, form.again) !== '') return // whatever the fields' own state says
  busy.value = true
  error.value = ''
  try {
    await session.loadThemes().catch(() => [])
    await session.setup({
      setup_token: form.setup_token.trim(),
      organization_name: form.organization_name,
      display_name: form.display_name,
      email: form.email,
      password: form.password,
    })
    form.password = form.again = '' // not kept in the page after the account exists
    await loadOptional()
    await go(FIRST_OPTIONAL)
  } catch (e) {
    error.value = failure(e)
  } finally {
    busy.value = false
  }
}

// ── 4 and 5: the optional steps, read from and written through Administration → Settings ────────────────────
const recordingKeys = ['recordings.enabled', 'recordings.mode', 'recordings.video_format', 'recordings.start']
const aiKeys = ['ai.base_url', 'ai.transcription_model', 'ai.summary_model', 'ai.language']
const keyOf = (k: string) => settings.value?.keys.find((x) => x.key === k)
const fields = (names: string[]) => names.map(keyOf).filter((k): k is Key => !!k)

async function loadOptional() {
  settings.value = await api<Settings>('GET', '/admin/settings')
  for (const k of settings.value.keys) draft[k.key] = String(k.value)
  const keys = await api<{ keys: AIKey[]; storable: boolean }>('GET', '/admin/summaries/keys')
  aiKey.value = keys.keys[0] ?? null
  storable.value = keys.storable
  policy.value = (await api<{ meeting_summaries: SummaryPolicy }>('GET', '/admin/summaries')).meeting_summaries
  policyWas.value = policy.value
  twoFactor.value = (await api<{ code: boolean }>('GET', '/admin/settings/reauth')).code
  if (!items.value.length) await diagnose()
}

// skip leaves an optional step as the server has it
async function skip(names: string[], to: Step) {
  for (const k of fields(names)) draft[k.key] = String(k.value)
  if (to === 'review') {
    newKey.value = ''
    policy.value = policyWas.value
  }
  await go(to)
}

function options(k: Key): { value: string; text: string }[] {
  if (k.kind === 'bool')
    return [
      { value: 'true', text: t('On') },
      { value: 'false', text: t('Off') },
    ]
  return (k.choices ?? []).map((c) => ({ value: c, text: c }))
}
// what the owner changed, as the settings routes take it
const changes = computed(() => {
  const out: Record<string, Value> = {}
  for (const k of fields([...recordingKeys, ...aiKeys])) {
    const d = (draft[k.key] ?? '').trim()
    if (k.locked || d === String(k.value)) continue
    out[k.key] = k.kind === 'bool' ? d === 'true' : d
  }
  return out
})
const changed = computed(() => Object.keys(changes.value))
const canApply = computed(() => !!settings.value?.applier.available)
const sensitive = computed(() => changed.value.some((k) => keyOf(k)?.sensitive))
const needsProof = computed(() => newKey.value.trim() !== '' || (canApply.value && sensitive.value))
const policyWas = ref<SummaryPolicy>('off')

function checkAddress(e: Event) {
  const el = e.target as HTMLInputElement
  el.setCustomValidity(
    addressProblem(el.value)
      ? t('Enter an https:// address (http:// only for this machine or a private network).')
      : '',
  )
}
function show(k: string, v: Value): string {
  if (typeof v === 'boolean') return v ? t('On') : t('Off')
  return v === '' ? t('(empty)') : String(v)
}
const lines = computed(() => tomlLines(changes.value))

// ── 6 and 7: the review, then Finish does it step by step ───────────────────────────────────────────────────
interface Job {
  id: 'pause' | 'apply' | 'key' | 'summaries'
  title: string
  state: 'waiting' | 'running' | 'done' | 'failed'
  note?: string
}
const jobs = ref<Job[]>([])
const keep = ref<Pending | null>(null)
const secondsLeft = ref(0)
const finished = ref(false)
let tick: ReturnType<typeof setInterval> | undefined
onBeforeUnmount(() => clearInterval(tick))

// a new AI address is never used with summaries on (the server refuses it): they are paused first, and tested
// and turned on again at the end, so the key of one service never goes to another untested
const moving = computed(
  () => canApply.value && changed.value.some((k) => k === 'ai.base_url' || k === 'ai.transcription.base_url'),
)
const plan = computed<Job[]>(() => {
  const out: Job[] = []
  const pause = moving.value && policyWas.value !== 'off'
  if (pause)
    out.push({ id: 'pause', title: t('Turn meeting summaries off while the AI service changes'), state: 'waiting' })
  if (changed.value.length && canApply.value)
    out.push({ id: 'apply', title: t('Apply the settings and restart the server'), state: 'waiting' })
  if (newKey.value.trim()) out.push({ id: 'key', title: t('Store the AI service key, encrypted'), state: 'waiting' })
  if (policy.value !== (pause ? 'off' : policyWas.value))
    out.push({ id: 'summaries', title: t('Test the AI service and set meeting summaries'), state: 'waiting' })
  return out
})

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

// follow polls the settings until the change has its result; the server restarts meanwhile
async function follow(id: string): Promise<void> {
  for (;;) {
    await sleep(2000)
    let v: Settings
    try {
      v = await api<Settings>('GET', '/admin/settings')
    } catch (e) {
      if (e instanceof ApiError && e.code === 'network') continue // restarting
      throw e
    }
    if (v.pending && v.pending.id !== id) {
      // another change, made elsewhere: not this wizard's to keep. Ours has its result, or never started
      keep.value = null
      if (v.last_result?.id === id && v.last_result.outcome === 'applied') return
      throw new Error(t('The change was not applied.'))
    }
    if (v.pending) {
      keep.value = v.pending.phase === 'confirming' && v.pending.can_confirm ? v.pending : null
      if (keep.value) secondsLeft.value = v.pending.seconds_left ?? 0
      continue
    }
    keep.value = null
    settings.value = v
    if (v.last_result?.id === id && v.last_result.outcome === 'applied') return
    throw new Error(
      v.last_result?.reason
        ? t('The change was undone: {reason}', { reason: v.last_result.reason })
        : t('The change was not applied.'),
    )
  }
}
async function keepSettings() {
  const p = keep.value
  if (!p) return
  try {
    await api('POST', '/admin/settings/confirm', { id: p.id })
    keep.value = null
  } catch (e) {
    toast.error(failure(e))
  }
}

// with two-factor on a code proves one operation only: the second protected one asks for a fresh code
const twoFactor = ref(false)
const freshCode = ref('')
const askingCode = ref(false)
let gotCode: ((code: string) => void) | undefined
function anotherCode(): Promise<string> {
  askingCode.value = true
  return new Promise((resolve) => (gotCode = resolve))
}
function submitCode() {
  if (!freshCode.value.trim()) return
  askingCode.value = false
  gotCode?.(freshCode.value.trim())
  freshCode.value = ''
}

async function finish() {
  jobs.value = plan.value.map((j) => ({ ...j }))
  running.value = true
  finished.value = false
  await go('finish')
  tick ??= setInterval(() => (secondsLeft.value = Math.max(0, secondsLeft.value - 1)), 1000)
  const proof = { password: password.value, code: code.value.trim() }
  password.value = code.value = ''
  let used = false // the code went with a protected operation
  const prove = async () => {
    if (used && twoFactor.value) proof.code = await anotherCode()
    used = true
    return proof
  }
  for (const job of jobs.value) {
    job.state = 'running'
    try {
      if (job.id === 'pause') {
        await api('PUT', '/admin/summaries', { meeting_summaries: 'off' })
        policyWas.value = 'off'
      } else if (job.id === 'apply') {
        const v = await api<Settings>('PUT', '/admin/settings', {
          changes: changes.value,
          ...(sensitive.value ? await prove() : {}),
        })
        await follow(v.pending?.id ?? '')
        for (const k of settings.value?.keys ?? []) draft[k.key] = String(k.value)
      } else if (job.id === 'key') {
        await api('PUT', '/admin/summaries/keys', { key: 'ai.api_key', value: newKey.value.trim(), ...(await prove()) })
        newKey.value = ''
      } else {
        await api('PUT', '/admin/summaries', { meeting_summaries: policy.value })
        policyWas.value = policy.value
      }
      job.state = 'done'
    } catch (e) {
      job.state = 'failed'
      job.note = e instanceof ApiError ? failure(e) : e instanceof Error ? e.message : t('Unexpected error.')
      break // what comes after depends on it
    }
  }
  proof.password = proof.code = ''
  running.value = false
  finished.value = jobs.value.every((j) => j.state === 'done')
}

const nextSteps = computed(() => [
  t('Invite people in Administration → Users.'),
  t('Serve HTTPS before people outside this machine log in.'),
  t('Set up e-mail ([smtp]) so people are told what they missed.'),
  t('Back up the data directory and, with PostgreSQL or MariaDB, the database.'),
])

async function open() {
  await router.replace('/')
}

onMounted(async () => {
  if (!initialized.value) return
  try {
    await loadOptional() // an owner back for the optional steps
    step.value = FIRST_OPTIONAL
  } catch (e) {
    error.value = failure(e)
  }
})
</script>

<template>
  <AuthFrame :title="t('Set up MeetHub')" wide>
    <nav :aria-label="t('Steps')" data-wizard-steps>
      <ol class="wizard-steps">
        <li
          v-for="(s, n) in STEPS"
          :key="s"
          class="wizard-step"
          :class="{ reached: STEPS.indexOf(step) >= n, on: s === step }"
        >
          <button
            type="button"
            class="wizard-dot"
            :aria-current="s === step ? 'step' : undefined"
            :aria-label="
              STEPS.indexOf(step) > n
                ? t('{n}. {title} (done)', { n: n + 1, title: titles[s] })
                : t('{n}. {title}', { n: n + 1, title: titles[s] })
            "
            :disabled="!canGo(s, step, initialized, running)"
            :data-step="s"
            @click="go(s)"
          >
            <Check v-if="STEPS.indexOf(step) > n" :size="15" aria-hidden="true" />
            <span v-else aria-hidden="true">{{ n + 1 }}</span>
          </button>
          <span class="wizard-label" aria-hidden="true">{{ titles[s] }}</span>
        </li>
      </ol>
    </nav>
    <p class="hint" data-wizard-position>
      {{ t('Step {n} of {total}', { n: STEPS.indexOf(step) + 1, total: STEPS.length }) }}
    </p>
    <h2 ref="heading" tabindex="-1" class="wizard-heading">
      <component :is="icons[step]" :size="20" aria-hidden="true" />
      <span data-wizard-heading>{{ titles[step] }}</span>
    </h2>
    <p class="wizard-lead" data-wizard-lead>{{ leads[step] }}</p>
    <UiAlert v-if="error" kind="error" class="mb-3" data-wizard-error>{{ error }}</UiAlert>

    <!-- 1 -->
    <form
      v-if="step === 'access'"
      novalidate
      @submit.prevent="($event.target as HTMLFormElement).reportValidity() && access()"
    >
      <div class="wizard-box mb-4">
        <p class="hint mb-2">
          {{
            t(
              'The server wrote a one-time setup token when it first started. It proves that you run this server, and stops working when the setup ends.',
            )
          }}
        </p>
        <ul class="grid gap-2">
          <li v-for="c in commands" :key="c.text" class="flex flex-wrap items-center gap-2">
            <span class="text-xs font-semibold">{{ c.where() }}</span>
            <code class="break-all text-xs">{{ c.text }}</code>
            <button type="button" class="btn-ghost" data-copy @click="copy(c.text)">
              <Copy :size="13" aria-hidden="true" /> {{ t('Copy') }}
            </button>
          </li>
        </ul>
      </div>
      <div class="form-row">
        <label class="lbl" for="token">{{ t('Setup token') }}</label>
        <input id="token" v-model="form.setup_token" class="inp w-full" autocomplete="off" required />
      </div>
      <div class="wizard-actions">
        <span></span>
        <button type="submit" class="btn" :disabled="busy" data-wizard-next>
          {{ busy ? t('Please wait…') : t('Continue') }} <ArrowRight :size="14" aria-hidden="true" />
        </button>
      </div>
    </form>

    <!-- 2 -->
    <section v-else-if="step === 'server'" data-wizard-diagnosis>
      <p class="hint mb-3" aria-live="polite" data-diagnosis-count>
        {{
          wrong.length
            ? t('{n} of {total} parts need attention. Only a problem stops the setup; the rest can wait.', {
                n: wrong.length,
                total: items.length,
              })
            : t('Every part of the server is ready.')
        }}
      </p>
      <div class="grid gap-2">
        <UiAlert
          v-for="i in wrong"
          :key="i.id"
          :kind="alertKind(i)"
          :title="partTitle(i)"
          :data-part="i.id"
          :data-level="i.level"
        >
          {{ partText(i) }}
        </UiAlert>
      </div>
      <div v-if="fine.length" class="wizard-box mt-3">
        <h3 class="lbl">{{ t('Ready') }}</h3>
        <ul class="grid gap-1 text-sm sm:grid-cols-2" data-diagnosis-ok>
          <li v-for="i in fine" :key="i.id" class="flex items-center gap-2" :data-part="i.id" data-level="ok">
            <CircleCheck :size="15" aria-hidden="true" /> {{ partTitle(i) }}
          </li>
        </ul>
      </div>
      <div class="wizard-actions">
        <div class="flex flex-wrap gap-2">
          <button type="button" class="btn-ghost" @click="back">
            <ArrowLeft :size="14" aria-hidden="true" /> {{ t('Back') }}
          </button>
          <button type="button" class="btn-ghost" :disabled="busy" data-diagnosis-again @click="diagnose">
            <RefreshCw :size="13" aria-hidden="true" /> {{ busy ? t('Please wait…') : t('Check again') }}
          </button>
        </div>
        <button type="button" class="btn" :disabled="busy || blocked(items)" data-wizard-next @click="go('account')">
          {{ t('Continue') }} <ArrowRight :size="14" aria-hidden="true" />
        </button>
      </div>
    </section>

    <!-- 3 -->
    <form
      v-else-if="step === 'account'"
      novalidate
      @submit.prevent="(checkAgain(), ($event.target as HTMLFormElement).reportValidity()) && createAccount()"
    >
      <div class="form-row">
        <label class="lbl" for="org">{{ t('Organization name') }}</label>
        <input id="org" v-model="form.organization_name" class="inp w-full" aria-describedby="org-hint" required />
        <p id="org-hint" class="hint mt-1">{{ t('Your company or team, as people will see it.') }}</p>
      </div>
      <div class="form-row">
        <label class="lbl" for="name">{{ t('Your name') }}</label>
        <input
          id="name"
          v-model="form.display_name"
          class="inp w-full"
          autocomplete="name"
          aria-describedby="name-hint"
          required
        />
        <p id="name-hint" class="hint mt-1">{{ t('Shown beside your messages.') }}</p>
      </div>
      <div class="form-row">
        <label class="lbl" for="email">{{ t('E-mail') }}</label>
        <input
          id="email"
          v-model="form.email"
          class="inp w-full"
          type="email"
          autocomplete="username"
          aria-describedby="email-hint"
          required
        />
        <p id="email-hint" class="hint mt-1">
          {{ t('You log in with it. This account is the owner: it manages people and settings.') }}
        </p>
      </div>
      <div class="grid gap-x-3 sm:grid-cols-2">
        <div class="form-row">
          <label class="lbl" for="password">{{ t('Password') }}</label>
          <input
            id="password"
            v-model="form.password"
            class="inp w-full"
            type="password"
            autocomplete="new-password"
            minlength="12"
            aria-describedby="password-hint"
            required
            @input="checkAgain"
          />
        </div>
        <div class="form-row">
          <label class="lbl" for="again">{{ t('Password again') }}</label>
          <input
            id="again"
            ref="again"
            v-model="form.again"
            class="inp w-full"
            type="password"
            autocomplete="new-password"
            required
            @input="checkAgain"
            @change="checkAgain"
          />
        </div>
      </div>
      <p id="password-hint" class="hint">
        {{ t('At least 12 characters. It is stored only as a hash, never sent anywhere else.') }}
      </p>
      <div class="wizard-actions">
        <button type="button" class="btn-ghost" @click="back">
          <ArrowLeft :size="14" aria-hidden="true" /> {{ t('Back') }}
        </button>
        <button type="submit" class="btn" :disabled="busy">
          <Rocket :size="14" aria-hidden="true" /> {{ busy ? t('Please wait…') : t('Create organization') }}
        </button>
      </div>
    </form>

    <!-- 4 -->
    <form v-else-if="step === 'recordings'" novalidate @submit.prevent="go('summaries')">
      <UiAlert kind="success" class="mb-3" data-wizard-account-ready>
        {{
          t(
            'Your account exists: MeetHub can be used now. These two steps are optional and can be done later in Administration → Settings.',
          )
        }}
      </UiAlert>
      <UiAlert
        v-if="!isOK('meetings')"
        kind="info"
        class="mb-3"
        :title="t('Meetings (LiveKit)')"
        data-wizard-no-meetings
      >
        {{ part('meetings') ? partText(part('meetings')!) : '' }}
        {{ t('Recordings need meetings: set LiveKit up first.') }}
      </UiAlert>
      <div class="grid gap-x-4 sm:grid-cols-2">
        <div v-for="k in fields(recordingKeys)" :key="k.key" class="form-row" :data-setting="k.key">
          <label class="lbl" :for="`w-${k.key}`">{{ label(k.key) }}</label>
          <select
            :id="`w-${k.key}`"
            v-model="draft[k.key]"
            class="inp w-full"
            :disabled="k.locked || !isOK('meetings')"
            :aria-describedby="`w-${k.key}-hint`"
          >
            <option v-for="o in options(k)" :key="o.value" :value="o.value">{{ o.text }}</option>
          </select>
          <p :id="`w-${k.key}-hint`" class="hint mt-1">
            <span v-if="k.locked" class="inline-flex items-center gap-1" data-locked>
              <Lock :size="13" aria-hidden="true" />{{ t('Set by {env}', { env: k.env }) }}.
            </span>
            {{ hint(k.key) }}
          </p>
        </div>
      </div>
      <div class="wizard-actions">
        <button type="button" class="btn-ghost" data-wizard-skip @click="skip(recordingKeys, 'summaries')">
          {{ t('Skip this step') }}
        </button>
        <button type="submit" class="btn" data-wizard-next>
          {{ t('Continue') }} <ArrowRight :size="14" aria-hidden="true" />
        </button>
      </div>
    </form>

    <!-- 5 -->
    <form
      v-else-if="step === 'summaries'"
      novalidate
      @submit.prevent="($event.target as HTMLFormElement).reportValidity() && go('review')"
    >
      <p class="hint mb-3">
        {{
          t(
            "An AI service transcribes the recording of a meeting and writes an organized summary of what was said, posted in the recording's thread. The audio, the transcript and the participants' names are sent to that service.",
          )
        }}
      </p>
      <div class="form-row" data-wizard-ai-key>
        <label class="lbl" for="w-ai-key">{{ t('AI service key') }}</label>
        <p v-if="aiKey?.locked" class="hint inline-flex items-center gap-1" data-locked>
          <Lock :size="13" aria-hidden="true" />{{ t('Set by {env}', { env: aiKey.env }) }}
        </p>
        <UiAlert v-else-if="!storable" kind="info">
          {{
            t(
              'A key cannot be stored here: this server runs without MEETHUB_SECRET_KEY. Set the key with MEETHUB_AI_API_KEY in the environment instead.',
            )
          }}
        </UiAlert>
        <template v-else>
          <input
            id="w-ai-key"
            v-model="newKey"
            type="password"
            class="inp w-full"
            autocomplete="off"
            maxlength="512"
            :placeholder="aiKey?.set ? t('Enter a new key to replace it') : t('Paste the key')"
            aria-describedby="w-ai-key-hint"
          />
          <p id="w-ai-key-hint" class="hint mt-1">
            {{
              t(
                'The API key of the service, e.g. one from platform.openai.com. It is stored encrypted and never shown.',
              )
            }}
          </p>
        </template>
      </div>
      <div v-for="k in fields(aiKeys)" :key="k.key" class="form-row" :data-setting="k.key">
        <label class="lbl" :for="`w-${k.key}`">{{ label(k.key) }}</label>
        <input
          :id="`w-${k.key}`"
          v-model="draft[k.key]"
          class="inp w-full"
          :disabled="k.locked"
          :required="k.key === 'ai.base_url'"
          :maxlength="k.key === 'ai.language' ? 2 : 200"
          :aria-describedby="`w-${k.key}-hint`"
          @input="k.key === 'ai.base_url' && checkAddress($event)"
        />
        <p :id="`w-${k.key}-hint`" class="hint mt-1">
          <span v-if="k.locked" class="inline-flex items-center gap-1" data-locked>
            <Lock :size="13" aria-hidden="true" />{{ t('Set by {env}', { env: k.env }) }}.
          </span>
          {{ hint(k.key) }}
        </p>
      </div>
      <fieldset class="wizard-box grid gap-2" data-wizard-policy>
        <legend class="lbl px-1">{{ t('Meeting summaries') }}</legend>
        <p class="hint">
          {{ t('Turned on at the end, only if the AI service answers the connection test.') }}
        </p>
        <label v-for="v in ['off', 'manual', 'automatic'] as const" :key="v" class="flex items-start gap-2">
          <input v-model="policy" type="radio" name="w-policy" :value="v" :data-value="v" />
          <span>
            <span class="font-semibold">{{
              { off: t('Off'), manual: t('Manual'), automatic: t('Automatic') }[v]
            }}</span>
            <span class="hint block">{{
              {
                off: t('Nothing from a meeting is sent to the AI service.'),
                manual: t('Who may record a channel can ask for the summary of one of its recordings.'),
                automatic: t('Every recording is summarized when it is ready.'),
              }[v]
            }}</span>
          </span>
        </label>
      </fieldset>
      <div class="wizard-actions">
        <div class="flex flex-wrap gap-2">
          <button type="button" class="btn-ghost" @click="back">
            <ArrowLeft :size="14" aria-hidden="true" /> {{ t('Back') }}
          </button>
          <button type="button" class="btn-ghost" data-wizard-skip @click="skip(aiKeys, 'review')">
            {{ t('Skip this step') }}
          </button>
        </div>
        <button type="submit" class="btn" data-wizard-next>
          {{ t('Continue') }} <ArrowRight :size="14" aria-hidden="true" />
        </button>
      </div>
    </form>

    <!-- 6 -->
    <form v-else-if="step === 'review'" novalidate data-wizard-review @submit.prevent="finish">
      <p v-if="!plan.length && !changed.length" class="wizard-box mb-3" data-review-nothing>
        {{ t('Nothing will be changed. You can do these steps later in Administration → Settings.') }}
      </p>
      <div v-else class="wizard-box mb-3">
        <h3 class="lbl">{{ t('What will be done') }}</h3>
        <ul class="grid gap-1" data-review-list>
          <li v-if="newKey.trim()" class="flex items-start gap-2">
            <Check :size="15" class="mt-0.5 shrink-0" aria-hidden="true" />
            {{ t('The AI service key is stored encrypted in the database.') }}
          </li>
          <li v-for="k in changed" :key="k" class="flex items-start gap-2">
            <Check :size="15" class="mt-0.5 shrink-0" aria-hidden="true" />
            <span>{{ label(k) }}: {{ show(k, changes[k]!) }}</span>
          </li>
          <li v-if="policy !== policyWas" class="flex items-start gap-2">
            <Check :size="15" class="mt-0.5 shrink-0" aria-hidden="true" />
            <span
              >{{ t('Meeting summaries') }}:
              {{ { off: t('Off'), manual: t('Manual'), automatic: t('Automatic') }[policy] }}</span
            >
          </li>
        </ul>
      </div>
      <UiAlert v-if="changed.length && canApply" kind="info" class="mb-3">
        {{
          t(
            'The server restarts once (a few seconds) to apply the settings; you then keep them with one click, or they are undone.',
          )
        }}
      </UiAlert>
      <div v-if="changed.length && !canApply" class="mb-3" data-review-lines>
        <UiAlert kind="info" class="mb-2" :title="t('Settings cannot be changed from the browser on this install')">
          {{ t('Add these lines to config.toml on the server and restart it; the wizard does not write that file.') }}
        </UiAlert>
        <pre class="wizard-box overflow-x-auto text-xs" tabindex="0"><code>{{ lines }}</code></pre>
        <button type="button" class="btn-ghost mt-2" data-copy @click="copy(lines)">
          <Copy :size="13" aria-hidden="true" /> {{ t('Copy') }}
        </button>
      </div>
      <ReauthFields
        v-if="needsProof"
        id="wizard-reauth"
        v-model:password="password"
        v-model:code="code"
        class="wizard-box"
        @ready="proven = $event"
      />
      <div class="wizard-actions">
        <button type="button" class="btn-ghost" @click="back">
          <ArrowLeft :size="14" aria-hidden="true" /> {{ t('Back') }}
        </button>
        <button type="submit" class="btn" :disabled="needsProof && !proven" data-wizard-finish>
          <Rocket :size="14" aria-hidden="true" /> {{ plan.length ? t('Finish') : t('Finish without changes') }}
        </button>
      </div>
    </form>

    <!-- 7 -->
    <section v-else data-wizard-finish-step>
      <ol v-if="jobs.length" class="wizard-box mb-3 grid gap-2" aria-live="polite" data-wizard-jobs>
        <li v-for="j in jobs" :key="j.id" class="flex items-start gap-2" :data-job="j.id" :data-state="j.state">
          <component
            :is="jobIcon[j.state]"
            :size="16"
            class="mt-0.5 shrink-0"
            :class="{ 'wizard-spin': j.state === 'running' }"
            aria-hidden="true"
          />
          <span>
            <span class="font-semibold">{{ j.title }}</span>
            <span class="hint block">
              {{ { waiting: t('waiting'), running: t('in progress…'), done: t('done'), failed: t('failed') }[j.state] }}
              <template v-if="j.note"> — {{ j.note }}</template>
            </span>
          </span>
        </li>
      </ol>
      <UiAlert
        v-if="keep"
        kind="info"
        class="mb-3"
        :title="t('The server restarted with the new settings')"
        data-wizard-keep
      >
        <p>{{ t('Keep them? Without a confirmation they are undone.') }}</p>
        <div class="mt-2 flex flex-wrap items-center gap-2">
          <button type="button" class="btn" data-settings-keep @click="keepSettings">
            {{ t('Keep these settings') }}
          </button>
          <span class="text-sm">{{ t('{n} s left', { n: secondsLeft }) }}</span>
        </div>
      </UiAlert>
      <form v-if="askingCode" class="wizard-box mb-3" data-wizard-code @submit.prevent="submitCode">
        <label class="lbl" for="w-fresh-code">{{ t('Two-factor code') }}</label>
        <p id="w-fresh-code-hint" class="hint mb-2">
          {{ t('The first code was used. Enter a new one from your authenticator to go on.') }}
        </p>
        <div class="flex flex-wrap items-center gap-2">
          <input
            id="w-fresh-code"
            v-model="freshCode"
            class="inp w-40"
            inputmode="numeric"
            autocomplete="one-time-code"
            aria-describedby="w-fresh-code-hint"
          />
          <button type="submit" class="btn" :disabled="!freshCode.trim()">{{ t('Continue') }}</button>
        </div>
      </form>
      <template v-if="!running">
        <UiAlert v-if="finished" kind="success" class="mb-3" :title="t('MeetHub is set up.')" data-wizard-done>
          {{ t('Everything you chose is in place. Welcome aboard!') }}
        </UiAlert>
        <UiAlert v-else kind="warning" class="mb-3" data-wizard-failed>
          {{
            t(
              'A step did not finish. Go back to the review to correct it, or do it later in Administration → Settings.',
            )
          }}
        </UiAlert>
        <div class="wizard-box">
          <h3 class="lbl">{{ t('Next steps') }}</h3>
          <ul class="grid gap-1 text-sm">
            <li v-for="n in nextSteps" :key="n" class="flex items-start gap-2">
              <ArrowRight :size="14" class="mt-0.5 shrink-0" aria-hidden="true" /> {{ n }}
            </li>
          </ul>
        </div>
        <div class="wizard-actions">
          <button v-if="!finished" type="button" class="btn-ghost" @click="go('review')">
            <ArrowLeft :size="14" aria-hidden="true" /> {{ t('Back to the review') }}
          </button>
          <span v-else></span>
          <button type="button" class="btn" data-wizard-open @click="open">
            {{ t('Open MeetHub') }} <ArrowRight :size="14" aria-hidden="true" />
          </button>
        </div>
      </template>
    </section>
  </AuthFrame>
</template>

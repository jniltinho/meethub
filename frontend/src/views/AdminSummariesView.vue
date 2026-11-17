<script setup lang="ts">
// Meeting summaries in Administration (chat-ui spec, add-meeting-summaries): off, manual or automatic, a
// "Test connection" action with its result, and, before turning it off, a themed note that what was already sent
// to the AI service cannot be recalled. Unavailable, with the reason, while no AI service is configured. The AI
// keys are entered here too (admin-settings spec, edit-ai-and-recording-settings): never shown, replaced or removed
// with the owner's password again, locked when the environment sets them.
import { computed, onMounted, reactive, ref } from 'vue'
import { Lock } from '@lucide/vue'
import { api, ApiError } from '@/api/client'
import ReauthFields from '@/components/ReauthFields.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import { askConfirm } from '@/feedback/dialogs'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'
import type { SummaryPolicy } from '@/stores/meetings'

// embedded: a section of Administration → Settings (add-admin-settings), under its own heading level
defineProps<{ embedded?: boolean }>()

interface Setting {
  meeting_summaries: SummaryPolicy
  available: boolean
}
const setting = ref<Setting | null>(null)
// what the radios show: back to the stored value when a change is cancelled or refused
const selected = ref<SummaryPolicy>('off')
const busy = ref(false)
const result = ref<{ ok: boolean; text: string } | null>(null)
const values: { value: SummaryPolicy; label: () => string; hint: () => string }[] = [
  { value: 'off', label: () => t('Off'), hint: () => t('Nothing from a meeting is sent to the AI service.') },
  {
    value: 'manual',
    label: () => t('Manual'),
    hint: () => t('Who may record a channel can ask for the summary of one of its recordings.'),
  },
  {
    value: 'automatic',
    label: () => t('Automatic'),
    hint: () => t('Every recording is summarized when it is ready.'),
  },
]

interface AIKey {
  key: 'ai.api_key' | 'ai.transcription.api_key'
  set: boolean
  source: '' | 'environment' | 'stored'
  env: string
  locked: boolean
  unreadable?: boolean
  needed: boolean
}
const keys = ref<{ keys: AIKey[]; storable: boolean } | null>(null)
const typed = reactive<Record<string, string>>({}) // a key being entered, never read back from the server
const password = ref('')
const code = ref('')
const proven = ref(false)
// the transcription key only matters with ai.transcription.base_url
const shownKeys = computed(() => keys.value?.keys.filter((k) => k.needed || k.set || k.unreadable) ?? [])
const editable = computed(() => !!keys.value?.storable && shownKeys.value.some((k) => !k.locked))

function failure(e: unknown): string {
  return e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')
}

async function load() {
  try {
    setting.value = await api<Setting>('GET', '/admin/summaries')
    selected.value = setting.value.meeting_summaries
    keys.value = await api('GET', '/admin/summaries/keys')
  } catch (e) {
    toast.error(failure(e))
  }
}
onMounted(load)

function keyLabel(k: AIKey): string {
  return k.key === 'ai.api_key' ? t('AI service key') : t('Transcription service key')
}

function keyState(k: AIKey): string {
  if (k.locked) return t('Set by {env}', { env: k.env })
  if (k.unreadable) return t('Stored under another MEETHUB_SECRET_KEY: enter it again.')
  return k.set ? t('Set: entered here and stored encrypted. It is never shown.') : t('Not set')
}

// putKey stores what was typed, or removes the key (value ""), with the password again
async function putKey(k: AIKey, value: string) {
  if (busy.value || !proven.value) return
  if (value === '') {
    const ok = await askConfirm(
      t('Remove the {name}? Meeting summaries are turned off, and summaries being written stop.', {
        name: keyLabel(k),
      }),
      { ok: t('Remove'), danger: true },
    )
    if (!ok) return
  }
  busy.value = true
  result.value = null
  try {
    keys.value = await api('PUT', '/admin/summaries/keys', {
      key: k.key,
      value,
      password: password.value,
      code: code.value.trim(),
    })
    typed[k.key] = ''
    toast.success(value === '' ? t('The key was removed.') : t('The key was saved.'))
    await load()
  } catch (e) {
    toast.error(failure(e))
  } finally {
    password.value = code.value = '' // sent or refused: never kept
    busy.value = false
  }
}

async function choose(value: SummaryPolicy) {
  const cur = setting.value
  if (!cur || busy.value || value === cur.meeting_summaries) return
  if (value === 'off') {
    const ok = await askConfirm(
      t(
        'Summaries being written will stop within seconds, and no more will be written. What was already sent to the AI service stays with it and cannot be recalled. Turn meeting summaries off?',
      ),
      { ok: t('Turn off'), danger: true },
    )
    if (!ok) {
      selected.value = cur.meeting_summaries
      return
    }
  }
  busy.value = true
  result.value = null
  try {
    setting.value = await api<Setting>('PUT', '/admin/summaries', { meeting_summaries: value })
    toast.success(t('Meeting summaries setting saved.'))
  } catch (e) {
    result.value = { ok: false, text: failure(e) } // the reason the setting stayed as it was
  } finally {
    busy.value = false
    selected.value = setting.value?.meeting_summaries ?? 'off'
  }
}

async function test() {
  if (busy.value) return
  busy.value = true
  result.value = null
  try {
    await api('POST', '/admin/summaries/test')
    result.value = { ok: true, text: t('The AI service answered with timed transcription and a summary.') }
  } catch (e) {
    result.value = { ok: false, text: failure(e) }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="max-w-2xl">
    <component :is="embedded ? 'h2' : 'h1'" :class="embedded ? 'mb-2 text-lg font-semibold' : 'page-title'">
      {{ t('Meeting summaries') }}
    </component>
    <p class="mb-3 text-sm text-fg-muted">
      {{
        t(
          "An AI service transcribes the recording of a meeting and writes an organized summary of what was said, posted in the recording's thread. The audio, the transcript and the participants' names are sent to that service.",
        )
      }}
    </p>
    <UiAlert v-if="setting && !setting.available" kind="info" data-summaries-unavailable>
      {{ t('To offer summaries, set the key and the two models below and turn meeting recordings on.') }}
    </UiAlert>
    <fieldset v-if="setting" class="mt-3 grid gap-2" :disabled="!setting.available || busy" data-summaries-setting>
      <legend class="lbl">{{ t('Meeting summaries') }}</legend>
      <label v-for="v in values" :key="v.value" class="flex items-start gap-2">
        <input
          type="radio"
          name="meeting-summaries"
          v-model="selected"
          :value="v.value"
          :data-value="v.value"
          @change="choose(v.value)"
        />
        <span>
          <span class="font-semibold">{{ v.label() }}</span>
          <span class="block text-sm text-fg-muted">{{ v.hint() }}</span>
        </span>
      </label>
    </fieldset>
    <div v-if="setting?.available" class="mt-4 flex flex-wrap items-center gap-2">
      <button type="button" class="btn-ghost" :disabled="busy" data-summaries-test @click="test">
        {{ busy ? t('Testing…') : t('Test connection') }}
      </button>
    </div>
    <div v-if="result" class="mt-3">
      <UiAlert :kind="result.ok ? 'success' : 'error'" data-summaries-result>{{ result.text }}</UiAlert>
    </div>

    <section v-if="keys" class="card mt-6" aria-labelledby="ai-keys-title" data-ai-keys>
      <h3 id="ai-keys-title" class="card-h">{{ t('AI service keys') }}</h3>
      <div class="grid gap-4 p-3">
        <UiAlert v-if="!keys.storable" kind="info" data-ai-keys-unstorable>
          {{
            t(
              'A key cannot be stored here: this server runs without MEETHUB_SECRET_KEY. Set the key with MEETHUB_AI_API_KEY in the environment instead.',
            )
          }}
        </UiAlert>
        <div v-for="k in shownKeys" :key="k.key" :data-ai-key="k.key">
          <label class="lbl" :for="`key-${k.key}`">{{ keyLabel(k) }}</label>
          <p :id="`key-${k.key}-state`" class="mb-1 flex items-center gap-1 text-sm text-fg-muted" data-ai-key-state>
            <Lock v-if="k.locked" :size="14" aria-hidden="true" />{{ keyState(k) }}
          </p>
          <div v-if="!k.locked && keys.storable" class="flex flex-wrap items-center gap-2">
            <input
              :id="`key-${k.key}`"
              v-model="typed[k.key]"
              type="password"
              class="inp max-w-md"
              autocomplete="off"
              maxlength="512"
              :placeholder="k.set ? t('Enter a new key to replace it') : t('Paste the key')"
              :aria-describedby="`key-${k.key}-state`"
            />
            <button
              type="button"
              class="btn"
              :disabled="busy || !proven || !(typed[k.key] ?? '').trim()"
              data-ai-key-save
              @click="putKey(k, (typed[k.key] ?? '').trim())"
            >
              {{ t('Save key') }}
            </button>
            <button
              v-if="k.source === 'stored' || k.unreadable"
              type="button"
              class="btn-ghost"
              :disabled="busy || !proven"
              data-ai-key-remove
              @click="putKey(k, '')"
            >
              {{ t('Remove') }}
            </button>
          </div>
        </div>
        <ReauthFields
          v-if="editable"
          id="ai-keys-reauth"
          v-model:password="password"
          v-model:code="code"
          @ready="proven = $event"
        />
      </div>
    </section>
  </div>
</template>

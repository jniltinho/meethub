<script setup lang="ts">
// Meeting summaries in Administration (chat-ui spec, add-meeting-summaries): off, manual or automatic, a
// "Test connection" action with its result, and, before turning it off, a themed note that what was already sent
// to the AI service cannot be recalled. Unavailable, with the reason, while no AI service is configured.
import { onMounted, ref } from 'vue'
import { api, ApiError } from '@/api/client'
import UiAlert from '@/components/ui/UiAlert.vue'
import { askConfirm } from '@/feedback/dialogs'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'
import type { SummaryPolicy } from '@/stores/meetings'

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

function failure(e: unknown): string {
  return e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')
}

onMounted(async () => {
  try {
    setting.value = await api<Setting>('GET', '/admin/summaries')
    selected.value = setting.value.meeting_summaries
  } catch (e) {
    toast.error(failure(e))
  }
})

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
    <h1 class="page-title">{{ t('Meeting summaries') }}</h1>
    <p class="mb-3 text-sm text-fg-muted">
      {{
        t(
          "An AI service transcribes the recording of a meeting and writes an organized summary of what was said, posted in the recording's thread. The audio, the transcript and the participants' names are sent to that service.",
        )
      }}
    </p>
    <UiAlert v-if="setting && !setting.available" kind="info" data-summaries-unavailable>
      {{ t('An AI service must be configured first (the [ai] settings), and meeting recordings must be on.') }}
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
  </div>
</template>

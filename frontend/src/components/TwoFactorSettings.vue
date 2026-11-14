<script setup lang="ts">
// Settings → Security: add an authenticator app (QR code drawn by the server, the secret as text), the recovery
// codes shown once, new recovery codes, turning two-factor off.
import { onMounted, ref } from 'vue'
import { Check, ShieldCheck } from '@lucide/vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import { api, ApiError } from '@/api/client'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'

interface Status {
  enabled: boolean
  recovery_codes_left: number
}
interface Enrollment {
  secret: string
  uri: string
  qr_code: string
}

const status = ref<Status | null>(null)
const unavailable = ref(false)
const error = ref('')
const enrollment = ref<Enrollment | null>(null)
const codes = ref<string[]>([]) // recovery codes, shown once
const code = ref('')
const password = ref('')
const offCode = ref('')

function fail(e: unknown) {
  if (e instanceof ApiError && e.code === 'invalid_code') error.value = t('That code is not valid.')
  else error.value = e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')
}

async function load() {
  try {
    status.value = await api<Status>('GET', '/me/two-factor')
  } catch (e) {
    if (e instanceof ApiError && e.code === 'two_factor_unavailable') unavailable.value = true
    else fail(e)
  }
}
onMounted(load)

async function call(fn: () => Promise<void>) {
  error.value = ''
  try {
    await fn()
    code.value = password.value = offCode.value = ''
  } catch (e) {
    fail(e)
  }
}

const begin = () =>
  call(async () => {
    enrollment.value = await api<Enrollment>('POST', '/me/two-factor')
  })

const confirm = () =>
  call(async () => {
    codes.value = (
      await api<{ recovery_codes: string[] }>('POST', '/me/two-factor/confirm', { code: code.value })
    ).recovery_codes
    enrollment.value = null
    toast.success(t('Two-factor authentication is on.'))
    await load()
  })

const regenerate = () =>
  call(async () => {
    codes.value = (
      await api<{ recovery_codes: string[] }>('POST', '/me/two-factor/recovery-codes', { code: code.value })
    ).recovery_codes
    await load()
  })

const disable = () =>
  call(async () => {
    await api('POST', '/me/two-factor/disable', { password: password.value, code: offCode.value })
    codes.value = []
    toast.success(t('Two-factor authentication is off.'))
    await load()
  })

const submit = (fn: () => Promise<void>) => (e: Event) => (e.target as HTMLFormElement).reportValidity() && fn()
</script>

<template>
  <section>
    <h1 class="page-title">{{ t('Security') }}</h1>
    <h2 class="lbl">{{ t('Two-factor authentication') }}</h2>
    <UiAlert v-if="unavailable" kind="info">
      {{ t('Two-factor authentication is not available on this server.') }}
    </UiAlert>
    <UiAlert v-if="error" kind="error" class="mb-3" dismissible @dismiss="error = ''">{{ error }}</UiAlert>

    <div v-if="codes.length" class="max-w-md" data-recovery-codes>
      <UiAlert kind="warning">
        {{
          t(
            'Save these recovery codes somewhere safe: each one logs you in once without the app. They are shown only now.',
          )
        }}
      </UiAlert>
      <ul class="recovery-codes">
        <li v-for="c in codes" :key="c">
          <code>{{ c }}</code>
        </li>
      </ul>
      <button type="button" class="btn" @click="codes = []">
        <Check :size="14" aria-hidden="true" /> {{ t('I saved them') }}
      </button>
    </div>

    <template v-else-if="status && !status.enabled">
      <form v-if="enrollment" class="max-w-md" novalidate @submit.prevent="submit(confirm)($event)">
        <p>{{ t('Scan this QR code with your authenticator app, then type the code it shows.') }}</p>
        <img :src="enrollment.qr_code" :alt="t('QR code for the authenticator app')" class="qr-code" />
        <p class="text-xs text-fg-muted">{{ t('Or type this key in the app:') }}</p>
        <p><code data-secret>{{ enrollment.secret }}</code></p>
        <div class="form-row">
          <label class="lbl" for="tf-code">{{ t('Authentication code') }}</label>
          <input
            id="tf-code"
            v-model="code"
            class="inp w-full"
            inputmode="numeric"
            autocomplete="one-time-code"
            maxlength="6"
            required
          />
        </div>
        <button type="submit" class="btn"><Check :size="14" aria-hidden="true" /> {{ t('Turn on') }}</button>
      </form>
      <template v-else>
        <p class="mb-3">
          {{ t('Two-factor authentication is off. With it, logging in also needs a code from an app on your phone.') }}
        </p>
        <button type="button" class="btn" @click="begin">
          <ShieldCheck :size="14" aria-hidden="true" /> {{ t('Add an authenticator app') }}
        </button>
      </template>
    </template>

    <template v-else-if="status?.enabled">
      <p class="mb-3">
        {{ t('Two-factor authentication is on. {n} recovery codes left.', { n: status.recovery_codes_left }) }}
      </p>
      <form class="max-w-md" novalidate @submit.prevent="submit(regenerate)($event)">
        <h2 class="lbl">{{ t('New recovery codes') }}</h2>
        <div class="form-row">
          <label class="lbl" for="rc-code">{{ t('Authentication code') }}</label>
          <input
            id="rc-code"
            v-model="code"
            class="inp w-full"
            inputmode="numeric"
            autocomplete="one-time-code"
            maxlength="6"
            required
          />
        </div>
        <button type="submit" class="btn-ghost">{{ t('Replace the recovery codes') }}</button>
      </form>
      <form class="mt-6 max-w-md" novalidate @submit.prevent="submit(disable)($event)">
        <h2 class="lbl">{{ t('Turn off') }}</h2>
        <div class="form-row">
          <label class="lbl" for="off-pw">{{ t('Password') }}</label>
          <input
            id="off-pw"
            v-model="password"
            class="inp w-full"
            type="password"
            autocomplete="current-password"
            required
          />
        </div>
        <div class="form-row">
          <label class="lbl" for="off-code">{{ t('Authentication code or recovery code') }}</label>
          <input id="off-code" v-model="offCode" class="inp w-full" autocomplete="one-time-code" required />
        </div>
        <button type="submit" class="btn-danger">{{ t('Turn off two-factor authentication') }}</button>
      </form>
    </template>
  </section>
</template>

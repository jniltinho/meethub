<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { LogIn } from '@lucide/vue'
import AuthFrame from '@/components/AuthFrame.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import { ApiError } from '@/api/client'
import { safeNext } from '@/router'
import { useSession } from '@/stores/session'
import { errorMessage, t } from '@/i18n'

const session = useSession()
const route = useRoute()
const router = useRouter()
const email = ref('')
const password = ref('')
const busy = ref(false)
const error = ref('')
// the second step: the ticket of a right password, then the authenticator's code (or a recovery code)
const ticket = ref('')
const code = ref('')
const recovery = ref(false)
const codeInput = ref<HTMLInputElement | null>(null)

async function focusCode() {
  await nextTick()
  codeInput.value?.focus()
}

async function run(step: () => Promise<void>) {
  busy.value = true
  error.value = ''
  try {
    await step()
  } catch (e) {
    if (e instanceof ApiError && e.code === 'invalid_code') {
      error.value = t('That code is not valid.')
      code.value = ''
      await focusCode()
    } else if (e instanceof ApiError && e.code === 'two_factor_expired') {
      ticket.value = code.value = password.value = '' // back to the password
      error.value = t('The login expired. Log in again.')
    } else {
      error.value = e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')
    }
  } finally {
    busy.value = false
  }
}

const submit = () =>
  run(async () => {
    await session.loadThemes().catch(() => [])
    const r = await session.login(email.value, password.value)
    if ('two_factor_required' in r) {
      ticket.value = r.ticket
      await focusCode()
      return
    }
    await router.replace(safeNext(route.query.next))
  })

const submitCode = () =>
  run(async () => {
    await session.loginTwoFactor(ticket.value, code.value)
    await router.replace(safeNext(route.query.next))
  })

async function toggleRecovery() {
  recovery.value = !recovery.value
  code.value = error.value = ''
  await focusCode()
}
</script>

<template>
  <AuthFrame :title="t('Log in to your workspace')">
    <UiAlert v-if="error" kind="error" class="mb-3">{{ error }}</UiAlert>
    <form
      v-if="ticket"
      novalidate
      @submit.prevent="($event.target as HTMLFormElement).reportValidity() && submitCode()"
    >
      <div class="form-row">
        <label class="lbl" for="code">{{ recovery ? t('Recovery code') : t('Authentication code') }}</label>
        <input
          id="code"
          ref="codeInput"
          v-model="code"
          class="inp w-full"
          :inputmode="recovery ? 'text' : 'numeric'"
          :autocomplete="recovery ? 'off' : 'one-time-code'"
          :maxlength="recovery ? 11 : 6"
          required
        />
        <p class="text-xs text-fg-muted">
          {{
            recovery
              ? t('One of the recovery codes you saved; each works once.')
              : t('The 6-digit code your authenticator app shows.')
          }}
        </p>
      </div>
      <button type="submit" class="btn w-full justify-center" :disabled="busy">
        <LogIn :size="14" aria-hidden="true" /> {{ busy ? t('Please wait…') : t('Verify') }}
      </button>
      <button type="button" class="btn-ghost mt-2 w-full justify-center" @click="toggleRecovery">
        {{ recovery ? t('Use the authenticator app') : t('Use a recovery code') }}
      </button>
    </form>
    <form v-else novalidate @submit.prevent="($event.target as HTMLFormElement).reportValidity() && submit()">
      <div class="form-row">
        <label class="lbl" for="email">{{ t('E-mail') }}</label>
        <input id="email" v-model="email" class="inp w-full" type="email" autocomplete="username" required />
      </div>
      <div class="form-row">
        <label class="lbl" for="password">{{ t('Password') }}</label>
        <input
          id="password"
          v-model="password"
          class="inp w-full"
          type="password"
          autocomplete="current-password"
          required
        />
      </div>
      <button type="submit" class="btn w-full justify-center" :disabled="busy">
        <LogIn :size="14" aria-hidden="true" /> {{ busy ? t('Please wait…') : t('Log in') }}
      </button>
    </form>
  </AuthFrame>
</template>

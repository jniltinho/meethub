<script setup lang="ts">
import { ref } from 'vue'
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

async function submit() {
  busy.value = true
  error.value = ''
  try {
    await session.loadThemes().catch(() => [])
    await session.login(email.value, password.value)
    await router.replace(safeNext(route.query.next))
  } catch (e) {
    error.value = e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <AuthFrame :title="t('Log in to your workspace')">
    <UiAlert v-if="error" kind="error" class="mb-3">{{ error }}</UiAlert>
    <form novalidate @submit.prevent="($event.target as HTMLFormElement).reportValidity() && submit()">
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

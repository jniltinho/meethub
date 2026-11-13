<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Rocket } from '@lucide/vue'
import AuthFrame from '@/components/AuthFrame.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import { ApiError } from '@/api/client'
import { useSession } from '@/stores/session'
import { errorMessage, t } from '@/i18n'

const session = useSession()
const route = useRoute()
const router = useRouter()
const form = reactive({
  setup_token: typeof route.query.token === 'string' ? route.query.token : '',
  organization_name: '',
  display_name: '',
  email: '',
  password: '',
})
const busy = ref(false)
const error = ref('')

async function submit() {
  busy.value = true
  error.value = ''
  try {
    await session.loadThemes().catch(() => [])
    await session.setup({ ...form })
    await router.replace('/')
  } catch (e) {
    error.value = e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <AuthFrame :title="t('Set up MeetHub')">
    <UiAlert kind="info" class="mb-3">
      {{ t('Enter the setup token shown in the server log (or in the setup-token file of the data directory).') }}
    </UiAlert>
    <UiAlert v-if="error" kind="error" class="mb-3">{{ error }}</UiAlert>
    <form novalidate @submit.prevent="($event.target as HTMLFormElement).reportValidity() && submit()">
      <div class="form-row">
        <label class="lbl" for="token">{{ t('Setup token') }}</label>
        <input id="token" v-model="form.setup_token" class="inp w-full" autocomplete="off" required />
      </div>
      <div class="form-row">
        <label class="lbl" for="org">{{ t('Organization name') }}</label>
        <input id="org" v-model="form.organization_name" class="inp w-full" required />
      </div>
      <div class="form-row">
        <label class="lbl" for="name">{{ t('Your name') }}</label>
        <input id="name" v-model="form.display_name" class="inp w-full" autocomplete="name" required />
      </div>
      <div class="form-row">
        <label class="lbl" for="email">{{ t('E-mail') }}</label>
        <input id="email" v-model="form.email" class="inp w-full" type="email" autocomplete="username" required />
      </div>
      <div class="form-row">
        <label class="lbl" for="password">{{ t('Password') }}</label>
        <input
          id="password"
          v-model="form.password"
          class="inp w-full"
          type="password"
          autocomplete="new-password"
          minlength="12"
          required
        />
      </div>
      <button type="submit" class="btn w-full justify-center" :disabled="busy">
        <Rocket :size="14" aria-hidden="true" /> {{ busy ? t('Please wait…') : t('Create organization') }}
      </button>
    </form>
  </AuthFrame>
</template>

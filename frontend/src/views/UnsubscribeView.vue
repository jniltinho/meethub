<script setup lang="ts">
// The page of a notification e-mail's "Turn off these e-mails" link (notifications spec "Unsubscribing"): no login,
// the link's token is the authorization. It asks first, so a mail scanner opening the link changes nothing.
import { ref } from 'vue'
import { useRoute } from 'vue-router'
import AuthFrame from '@/components/AuthFrame.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import { api, ApiError } from '@/api/client'
import { errorMessage, t } from '@/i18n'

const route = useRoute()
const token = typeof route.query.token === 'string' ? route.query.token : ''
const state = ref<'ask' | 'busy' | 'done' | 'error'>('ask')
const error = ref('')

async function confirm() {
  state.value = 'busy'
  try {
    await api('POST', `/notifications/unsubscribe?token=${encodeURIComponent(token)}`)
    state.value = 'done'
  } catch (e) {
    error.value = e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')
    state.value = 'error'
  }
}
</script>

<template>
  <AuthFrame :title="t('Notification e-mails')">
    <UiAlert v-if="state === 'done'" kind="success">
      {{ t('Notification e-mails are off. You can turn them on again in Settings → Notifications.') }}
    </UiAlert>
    <UiAlert v-else-if="state === 'error'" kind="error">{{ error }}</UiAlert>
    <template v-else>
      <p class="mb-4">{{ t('Stop receiving e-mails about messages sent to you while you are away?') }}</p>
      <button type="button" class="btn w-full" :disabled="state === 'busy' || !token" @click="confirm">
        {{ t('Turn off these e-mails') }}
      </button>
    </template>
  </AuthFrame>
</template>

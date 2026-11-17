<script setup lang="ts">
// What a sensitive settings change needs again (admin-settings spec, edit-ai-and-recording-settings): the owner's
// password and, with two-factor on, a code. An account without a password cannot make such a change here.
import { onMounted, ref, watchEffect } from 'vue'
import { api } from '@/api/client'
import UiAlert from '@/components/ui/UiAlert.vue'
import { t } from '@/i18n'

defineProps<{ id: string }>()
const password = defineModel<string>('password', { required: true })
const code = defineModel<string>('code', { required: true })
const emit = defineEmits<{ ready: [ok: boolean] }>()
const needs = ref<{ password: boolean; code: boolean } | null>(null)

onMounted(async () => {
  try {
    needs.value = await api('GET', '/admin/settings/reauth')
  } catch {
    needs.value = { password: true, code: false } // the server says what is missing when the change is sent
  }
})
watchEffect(() =>
  emit('ready', !!needs.value?.password && password.value !== '' && (!needs.value.code || code.value.trim() !== '')),
)
</script>

<template>
  <fieldset class="grid gap-2" data-reauth>
    <legend class="lbl">{{ t('Confirm it is you') }}</legend>
    <UiAlert v-if="needs && !needs.password" kind="warning" data-reauth-unavailable>
      {{ t('Your account has no password (single sign-on only): this is changed on the server.') }}
    </UiAlert>
    <template v-else>
      <p class="hint">
        {{ t('This decides where meeting data goes or what is deleted: enter your password again.') }}
      </p>
      <div class="flex flex-wrap items-end gap-3">
        <div>
          <label class="lbl" :for="`${id}-password`">{{ t('Your password') }}</label>
          <input
            :id="`${id}-password`"
            v-model="password"
            type="password"
            class="inp w-56"
            autocomplete="current-password"
            data-reauth-password
          />
        </div>
        <div v-if="needs?.code">
          <label class="lbl" :for="`${id}-code`">{{ t('Two-factor code') }}</label>
          <input
            :id="`${id}-code`"
            v-model="code"
            class="inp w-40"
            inputmode="numeric"
            autocomplete="one-time-code"
            data-reauth-code
          />
        </div>
      </div>
    </template>
  </fieldset>
</template>

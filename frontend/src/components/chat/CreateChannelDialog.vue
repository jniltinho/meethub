<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import ChatModal from './ChatModal.vue'
import { api, ApiError } from '@/api/client'
import type { Channel } from '@/chat/types'
import { channelPath } from '@/chat/location'
import { useChat } from '@/stores/chat'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'

const props = defineProps<{ teamId: string; teamName: string }>()
const emit = defineEmits<{ close: [] }>()
const chat = useChat()
const router = useRouter()
const form = reactive({ display_name: '', purpose: '', type: 'O' as 'O' | 'P' })
const error = ref('')
const busy = ref(false)

async function create() {
  error.value = ''
  busy.value = true
  try {
    const ch = await api<Channel>('POST', '/channels', { team_id: props.teamId, ...form })
    await chat.loadChannels(props.teamId)
    toast.success(t('Channel {name} created.', { name: ch.display_name }))
    emit('close')
    await router.push(channelPath(props.teamName, ch.name))
  } catch (e) {
    error.value = e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <ChatModal :title="t('Create a channel')" @close="emit('close')">
    <form @submit.prevent="create">
      <div class="pnl-body grid gap-3">
        <div>
          <label class="lbl" for="new-channel-name">{{ t('Name') }}</label>
          <input id="new-channel-name" v-model="form.display_name" class="inp" maxlength="64" required autofocus />
        </div>
        <div>
          <label class="lbl" for="new-channel-purpose">{{ t('Purpose (optional)') }}</label>
          <input id="new-channel-purpose" v-model="form.purpose" class="inp" maxlength="250" />
        </div>
        <fieldset class="grid gap-1">
          <legend class="lbl">{{ t('Visibility') }}</legend>
          <label class="flex items-center gap-2">
            <input v-model="form.type" type="radio" value="O" />{{ t('Public: anyone in the team can join') }}
          </label>
          <label class="flex items-center gap-2">
            <input v-model="form.type" type="radio" value="P" />{{ t('Private: only invited members') }}
          </label>
        </fieldset>
        <p v-if="error" class="field-error" role="alert">{{ error }}</p>
      </div>
      <div class="pnl-foot">
        <button type="submit" class="btn" :disabled="busy || !form.display_name.trim()">{{ t('Create') }}</button>
        <button type="button" class="btn-ghost" @click="emit('close')">{{ t('Cancel') }}</button>
      </div>
    </form>
  </ChatModal>
</template>

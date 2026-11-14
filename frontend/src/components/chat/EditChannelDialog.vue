<script setup lang="ts">
// Edit a channel's display name, purpose and header (channel administrators).
import { reactive, ref } from 'vue'
import ChatModal from './ChatModal.vue'
import { api, ApiError } from '@/api/client'
import type { ChannelInfo } from '@/chat/types'
import { useChat, type ChatChannel } from '@/stores/chat'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'

const props = defineProps<{ channel: ChatChannel }>()
const emit = defineEmits<{ close: [] }>()
const chat = useChat()
const form = reactive({
  display_name: props.channel.display_name,
  purpose: props.channel.purpose,
  header: props.channel.header,
})
const error = ref('')
const busy = ref(false)

async function save() {
  error.value = ''
  busy.value = true
  try {
    await api<ChannelInfo>('PATCH', `/channels/${props.channel.id}`, form)
    await chat.loadChannel(props.channel.id)
    toast.success(t('Channel updated.'))
    emit('close')
  } catch (e) {
    error.value = e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <ChatModal :title="t('Edit channel')" @close="emit('close')">
    <form @submit.prevent="save">
      <div class="pnl-body grid gap-3">
        <div>
          <label class="lbl" for="edit-channel-name">{{ t('Name') }}</label>
          <input id="edit-channel-name" v-model="form.display_name" class="inp" maxlength="64" required autofocus />
        </div>
        <div>
          <label class="lbl" for="edit-channel-purpose">{{ t('Purpose (optional)') }}</label>
          <input id="edit-channel-purpose" v-model="form.purpose" class="inp" maxlength="250" />
        </div>
        <div>
          <label class="lbl" for="edit-channel-header">{{ t('Header (optional)') }}</label>
          <input id="edit-channel-header" v-model="form.header" class="inp" maxlength="1024" />
        </div>
        <p v-if="error" class="field-error" role="alert">{{ error }}</p>
      </div>
      <div class="pnl-foot">
        <button type="submit" class="btn" :disabled="busy || !form.display_name.trim()">{{ t('Save') }}</button>
        <button type="button" class="btn-ghost" @click="emit('close')">{{ t('Cancel') }}</button>
      </div>
    </form>
  </ChatModal>
</template>

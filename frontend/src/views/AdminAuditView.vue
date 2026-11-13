<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, ApiError } from '@/api/client'
import type { AuditEvent } from '@/api/types'
import { toast } from '@/feedback/toasts'
import { errorMessage, formatDateTime, t } from '@/i18n'

const events = ref<AuditEvent[]>([])
const cursor = ref('')
const action = ref('')
const actions = [
  'system.initialized',
  'auth.login_succeeded',
  'auth.login_failed',
  'auth.logout',
  'user.created',
  'user.updated',
  'user.role_changed',
  'user.disabled',
  'user.enabled',
  'user.password_changed',
  'user.password_reset',
]

async function load(more = false) {
  try {
    const q = new URLSearchParams({ limit: '50' })
    if (action.value) q.set('action', action.value)
    if (more && cursor.value) q.set('cursor', cursor.value)
    const page = await api<{ events: AuditEvent[]; next_cursor?: string }>('GET', `/admin/audit?${q}`)
    events.value = more ? [...events.value, ...page.events] : page.events
    cursor.value = page.next_cursor ?? ''
  } catch (e) {
    toast.error(e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.'))
  }
}
onMounted(() => load())
</script>

<template>
  <div>
    <h1 class="page-title">{{ t('Audit log') }}</h1>
    <label class="lbl" for="act">{{ t('Action') }}</label>
    <select id="act" v-model="action" class="inp mb-3" @change="load()">
      <option value="">{{ t('All actions') }}</option>
      <option v-for="a in actions" :key="a" :value="a">{{ a }}</option>
    </select>
    <div class="overflow-x-auto">
      <table class="grid-t w-full">
        <thead>
          <tr>
            <th>{{ t('When') }}</th>
            <th>{{ t('Action') }}</th>
            <th>{{ t('Target') }}</th>
            <th>{{ t('Client address') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="e in events" :key="e.id">
            <td>{{ formatDateTime(e.created_at) }}</td>
            <td>{{ e.action }}</td>
            <td>{{ e.target_type }} {{ e.target_id }}</td>
            <td>{{ e.client_ip }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <button v-if="cursor" type="button" class="btn-ghost mt-3" @click="load(true)">{{ t('Load more') }}</button>
  </div>
</template>

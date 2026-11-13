<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { UserPlus } from '@lucide/vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import { api, ApiError } from '@/api/client'
import type { Member, Role } from '@/api/types'
import { useSession } from '@/stores/session'
import { askConfirm, confirmTyped } from '@/feedback/dialogs'
import { toast } from '@/feedback/toasts'
import { errorMessage, formatDateTime, t } from '@/i18n'

const session = useSession()
const users = ref<Member[]>([])
const cursor = ref('')
const error = ref('')
const creating = ref(false)
const form = reactive({ email: '', display_name: '', role: 'member' as Role, password: '' })
const roles: Role[] = ['owner', 'admin', 'member', 'guest']
const roleLabel = (r: Role) => ({ owner: t('Owner'), admin: t('Admin'), member: t('Member'), guest: t('Guest') })[r]

function fail(e: unknown) {
  const msg = e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')
  toast.error(msg)
  error.value = msg
}

async function load(more = false) {
  try {
    const q = new URLSearchParams({ limit: '50' })
    if (more && cursor.value) q.set('cursor', cursor.value)
    const page = await api<{ users: Member[]; next_cursor?: string }>('GET', `/admin/users?${q}`)
    users.value = more ? [...users.value, ...page.users] : page.users
    cursor.value = page.next_cursor ?? ''
  } catch (e) {
    fail(e)
  }
}
onMounted(() => load())

async function create() {
  error.value = ''
  try {
    await api('POST', '/admin/users', { ...form })
    toast.success(t('User {name} created.', { name: form.display_name }))
    Object.assign(form, { email: '', display_name: '', role: 'member', password: '' })
    creating.value = false
    await load()
  } catch (e) {
    fail(e)
  }
}

async function setRole(u: Member, role: Role) {
  if (role === u.role) return
  if (
    role === 'owner' &&
    !(await confirmTyped(
      t('{name} will be able to manage everything, including other owners.', { name: u.display_name }),
      u.email,
      { ok: t('Make owner'), danger: false },
    ))
  ) {
    await load()
    return
  }
  try {
    await api('PATCH', `/admin/users/${u.id}`, { role })
    toast.success(t('Role changed.'))
  } catch (e) {
    fail(e)
  }
  await load()
}

async function toggleStatus(u: Member) {
  const disabling = u.status === 'active'
  if (
    disabling &&
    !(await askConfirm(t('Disable {name}? Their sessions end at once.', { name: u.display_name }), {
      ok: t('Disable'),
      danger: true,
    }))
  )
    return
  try {
    await api('PATCH', `/admin/users/${u.id}`, { status: disabling ? 'disabled' : 'active' })
    toast.success(disabling ? t('User disabled.') : t('User enabled.'))
    await load()
  } catch (e) {
    fail(e)
  }
}
</script>

<template>
  <div>
    <div class="mb-3 flex items-center gap-2">
      <h1 class="page-title mb-0 flex-1">{{ t('Users') }}</h1>
      <button type="button" class="btn" @click="creating = !creating">
        <UserPlus :size="14" aria-hidden="true" /> {{ t('New user') }}
      </button>
    </div>
    <UiAlert v-if="error" kind="error" class="mb-3" dismissible @dismiss="error = ''">{{ error }}</UiAlert>
    <form
      v-if="creating"
      class="card mb-4 grid max-w-xl gap-2 p-3"
      novalidate
      @submit.prevent="($event.target as HTMLFormElement).reportValidity() && create()"
    >
      <label class="lbl" for="nu-name">{{ t('Display name') }}</label>
      <input id="nu-name" v-model="form.display_name" class="inp" required />
      <label class="lbl" for="nu-email">{{ t('E-mail') }}</label>
      <input id="nu-email" v-model="form.email" class="inp" type="email" required />
      <label class="lbl" for="nu-role">{{ t('Role') }}</label>
      <select id="nu-role" v-model="form.role" class="inp">
        <option v-for="r in roles" :key="r" :value="r" :disabled="r === 'owner' && !session.can('manage_owners')">
          {{ roleLabel(r) }}
        </option>
      </select>
      <label class="lbl" for="nu-pw">{{ t('Initial password') }}</label>
      <input
        id="nu-pw"
        v-model="form.password"
        class="inp"
        type="password"
        autocomplete="new-password"
        minlength="12"
        required
      />
      <div class="flex gap-2">
        <button type="submit" class="btn">{{ t('Create') }}</button>
        <button type="button" class="btn-ghost" @click="creating = false">{{ t('Cancel') }}</button>
      </div>
    </form>
    <div class="overflow-x-auto">
      <table class="grid-t w-full">
        <thead>
          <tr>
            <th>{{ t('Name') }}</th>
            <th>{{ t('E-mail') }}</th>
            <th>{{ t('Role') }}</th>
            <th>{{ t('Status') }}</th>
            <th>{{ t('Last login') }}</th>
            <th><span class="sr-only">{{ t('Actions') }}</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="u in users" :key="u.id">
            <td>{{ u.display_name }}</td>
            <td>{{ u.email }}</td>
            <td>
              <select
                class="inp"
                :value="u.role"
                :aria-label="t('Role of {name}', { name: u.display_name })"
                :disabled="u.role === 'owner' && !session.can('manage_owners')"
                @change="setRole(u, ($event.target as HTMLSelectElement).value as Role)"
              >
                <option
                  v-for="r in roles"
                  :key="r"
                  :value="r"
                  :disabled="r === 'owner' && !session.can('manage_owners')"
                >
                  {{ roleLabel(r) }}
                </option>
              </select>
            </td>
            <td>{{ u.status === 'active' ? t('Active') : t('Disabled') }}</td>
            <td>{{ u.last_login_at ? formatDateTime(u.last_login_at) : '—' }}</td>
            <td>
              <button
                type="button"
                class="btn-ghost"
                :disabled="u.id === session.profile?.user.id || (u.role === 'owner' && !session.can('manage_owners'))"
                @click="toggleStatus(u)"
              >
                {{ u.status === 'active' ? t('Disable') : t('Enable') }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <button v-if="cursor" type="button" class="btn-ghost mt-3" @click="load(true)">{{ t('Load more') }}</button>
  </div>
</template>

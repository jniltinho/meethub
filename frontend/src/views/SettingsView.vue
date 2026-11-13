<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { Check } from '@lucide/vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import { ApiError } from '@/api/client'
import { useSession } from '@/stores/session'
import { toast } from '@/feedback/toasts'
import { themeCatalog } from '@/themes/catalog'
import { applyTheme, currentTheme } from '@/themes/apply'
import { errorMessage, isLanguage, languages, locale, setLanguage, t } from '@/i18n'

const session = useSession()
const route = useRoute()
const section = computed(() => (route.params.section as string) || 'profile')

const name = ref(session.profile?.user.display_name ?? '')
watch(
  () => session.profile?.user.display_name,
  (v) => (name.value = v ?? ''),
)
const current = ref('')
const next = ref('')
const error = ref('')
const active = ref(currentTheme())

function fail(e: unknown) {
  error.value = e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')
}

async function saveName() {
  error.value = ''
  try {
    await session.updateMe({ display_name: name.value })
    toast.success(t('Profile saved.'))
  } catch (e) {
    fail(e)
  }
}

async function savePassword() {
  error.value = ''
  try {
    await session.changePassword(current.value, next.value)
    current.value = next.value = ''
    toast.success(t('Password changed. Your other sessions were ended.'))
  } catch (e) {
    fail(e)
  }
}

// the theme applies at once, then is saved in the user's preferences
async function chooseTheme(id: string) {
  const entry = themeCatalog.find((x) => x.id === id)
  applyTheme(id, entry?.scheme)
  active.value = id
  try {
    await session.updateMe({ preferences: { theme: id } })
  } catch (e) {
    fail(e)
  }
}

async function chooseLanguage(e: Event) {
  const v = (e.target as HTMLSelectElement).value
  if (!isLanguage(v)) return
  setLanguage(v)
  try {
    await session.updateMe({ preferences: { language: v } })
    toast.success(t('Language saved.'))
  } catch (err) {
    fail(err)
  }
}
</script>

<template>
  <div class="max-w-3xl">
    <UiAlert v-if="error" kind="error" class="mb-3" dismissible @dismiss="error = ''">{{ error }}</UiAlert>

    <section v-if="section === 'profile'">
      <h1 class="page-title">{{ t('Profile') }}</h1>
      <form
        class="max-w-md"
        novalidate
        @submit.prevent="($event.target as HTMLFormElement).reportValidity() && saveName()"
      >
        <div class="form-row">
          <label class="lbl" for="pname">{{ t('Display name') }}</label>
          <input id="pname" v-model="name" class="inp w-full" required maxlength="200" />
        </div>
        <div class="form-row">
          <span class="lbl">{{ t('E-mail') }}</span>
          <div>{{ session.profile?.user.email }}</div>
        </div>
        <button type="submit" class="btn"><Check :size="14" aria-hidden="true" /> {{ t('Save') }}</button>
      </form>
    </section>

    <section v-else-if="section === 'password'">
      <h1 class="page-title">{{ t('Password') }}</h1>
      <form
        class="max-w-md"
        novalidate
        @submit.prevent="($event.target as HTMLFormElement).reportValidity() && savePassword()"
      >
        <div class="form-row">
          <label class="lbl" for="cur">{{ t('Current password') }}</label>
          <input
            id="cur"
            v-model="current"
            class="inp w-full"
            type="password"
            autocomplete="current-password"
            required
          />
        </div>
        <div class="form-row">
          <label class="lbl" for="new">{{ t('New password') }}</label>
          <input
            id="new"
            v-model="next"
            class="inp w-full"
            type="password"
            autocomplete="new-password"
            minlength="12"
            required
          />
        </div>
        <button type="submit" class="btn"><Check :size="14" aria-hidden="true" /> {{ t('Change password') }}</button>
      </form>
    </section>

    <section v-else-if="section === 'appearance'">
      <h1 class="page-title">{{ t('Appearance') }}</h1>
      <div class="theme-grid" role="radiogroup" :aria-label="t('Theme')">
        <button
          v-for="th in themeCatalog"
          :key="th.id"
          type="button"
          role="radio"
          class="theme-tile"
          :class="{ on: active === th.id }"
          :aria-checked="active === th.id"
          :data-theme-id="th.id"
          @click="chooseTheme(th.id)"
        >
          <span class="theme-preview" aria-hidden="true">
            <span :style="{ background: th.swatch[3] }" />
            <span :style="{ background: th.swatch[1] }" />
            <span :style="{ background: th.swatch[0] }"><i :style="{ background: th.swatch[2] }" /></span>
          </span>
          <span class="theme-name">{{ th.label }}</span>
        </button>
      </div>
    </section>

    <section v-else>
      <h1 class="page-title">{{ t('Language') }}</h1>
      <label class="lbl" for="ulang">{{ t('Language') }}</label>
      <select id="ulang" class="inp" :value="locale" @change="chooseLanguage">
        <option v-for="l in languages" :key="l.id" :value="l.id">{{ l.label }}</option>
      </select>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { Check } from '@lucide/vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import UserPicture from '@/components/chat/UserPicture.vue'
import { api, ApiError } from '@/api/client'
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

// ── profile picture: chosen, previewed, saved or removed ─────────────────────
const picked = ref<File | null>(null)
const preview = ref('')
const pictureError = ref('')
const hasPicture = computed(() => !!session.profile?.user.last_picture_update)

function pick(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0] ?? null
  pictureError.value = ''
  if (preview.value) URL.revokeObjectURL(preview.value)
  preview.value = ''
  picked.value = null
  if (!file) return
  if (!['image/png', 'image/jpeg', 'image/gif'].includes(file.type)) {
    pictureError.value = t('That file is not a PNG, JPEG or GIF image.')
  } else if (file.size > 5 * 1024 * 1024) {
    pictureError.value = t('The image is larger than 5 MB.')
  } else {
    picked.value = file
    preview.value = URL.createObjectURL(file)
  }
  ;(e.target as HTMLInputElement).value = ''
}

async function savePicture() {
  if (!picked.value || !session.profile) return
  pictureError.value = ''
  const form = new FormData()
  form.append('image', picked.value)
  try {
    const res = await api<{ last_picture_update: number }>('POST', '/users/me/image', form)
    session.profile.user.last_picture_update = res.last_picture_update
    URL.revokeObjectURL(preview.value)
    preview.value = ''
    picked.value = null
    toast.success(t('Picture saved.'))
  } catch (e) {
    pictureError.value = e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')
  }
}

async function removePicture() {
  if (!session.profile) return
  pictureError.value = ''
  try {
    await api('DELETE', '/users/me/image')
    session.profile.user.last_picture_update = 0
    toast.success(t('Picture removed.'))
  } catch (e) {
    pictureError.value = e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.')
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

      <h2 class="page-title mt-6">{{ t('Profile picture') }}</h2>
      <div class="picture-row">
        <img v-if="preview" :src="preview" alt="" class="picture-preview" width="72" height="72" />
        <UserPicture
          v-else-if="session.profile"
          :user-id="session.profile.user.id"
          :name="session.profile.user.display_name"
          :size="72"
        />
        <div class="grid gap-2">
          <div class="flex flex-wrap gap-2">
            <label class="btn-ghost file-btn">
              {{ t('Choose a picture') }}
              <input
                type="file"
                class="sr-only"
                accept="image/png,image/jpeg,image/gif"
                data-picture-input
                @change="pick"
              />
            </label>
            <button v-if="picked" type="button" class="btn" @click="savePicture">
              <Check :size="14" aria-hidden="true" /> {{ t('Save picture') }}
            </button>
            <button v-else-if="hasPicture" type="button" class="btn-ghost" @click="removePicture">
              {{ t('Remove picture') }}
            </button>
          </div>
          <p class="text-xs text-fg-muted">{{ t('PNG, JPEG or GIF, up to 5 MB.') }}</p>
          <p v-if="pictureError" class="field-error" role="alert">{{ pictureError }}</p>
        </div>
      </div>
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

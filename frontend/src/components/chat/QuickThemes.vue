<script setup lang="ts">
// The quick theme switcher of the sidebar's foot (ui-themes spec): four themes one click away, with the same effect
// as the full picker, and a link to all of them.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { Palette } from '@lucide/vue'
import { useSession } from '@/stores/session'
import { themeCatalog } from '@/themes/catalog'
import { applyTheme, currentTheme } from '@/themes/apply'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const QUICK = ['meethub-dark', 'meethub-light', 'classic-blue', 'matrix']
const session = useSession()
const open = ref(false)
const active = ref(currentTheme())
const root = ref<HTMLElement | null>(null)
const themes = computed(() => QUICK.map((id) => themeCatalog.find((x) => x.id === id)!).filter(Boolean))

async function choose(id: string) {
  const entry = themeCatalog.find((x) => x.id === id)
  applyTheme(id, entry?.scheme)
  active.value = id
  open.value = false
  try {
    await session.updateMe({ preferences: { theme: id } })
  } catch {
    toast.error(t('Unexpected error.'))
  }
}

function toggle() {
  active.value = currentTheme()
  open.value = !open.value
}
function outside(e: Event) {
  if (open.value && root.value && !root.value.contains(e.target as Node)) open.value = false
}
onMounted(() => document.addEventListener('pointerdown', outside))
onBeforeUnmount(() => document.removeEventListener('pointerdown', outside))
</script>

<template>
  <div ref="root" class="quick-themes" @keydown.esc="open = false">
    <button
      type="button"
      class="icon-btn"
      :aria-label="t('Quick themes')"
      :title="t('Quick themes')"
      :aria-expanded="open"
      aria-haspopup="true"
      @click="toggle"
    >
      <Palette :size="16" aria-hidden="true" />
    </button>
    <div v-if="open" class="select-pop quick-themes-pop" role="group" :aria-label="t('Quick themes')">
      <div class="theme-grid" role="radiogroup" :aria-label="t('Theme')">
        <button
          v-for="th in themes"
          :key="th.id"
          type="button"
          role="radio"
          class="theme-tile"
          :class="{ on: active === th.id }"
          :aria-checked="active === th.id"
          :data-quick-theme="th.id"
          @click="choose(th.id)"
        >
          <span class="theme-preview" aria-hidden="true">
            <span :style="{ background: th.swatch[3] }" />
            <span :style="{ background: th.swatch[1] }" />
            <span :style="{ background: th.swatch[0] }"><i :style="{ background: th.swatch[2] }" /></span>
          </span>
          <span class="theme-name">{{ th.label }}</span>
        </button>
      </div>
      <RouterLink to="/settings/appearance" class="act" @click="open = false">{{ t('All themes') }}</RouterLink>
    </div>
  </div>
</template>

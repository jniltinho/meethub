<script setup lang="ts">
// Frame of the public pages (setup, login): centered box and the language selector.
import mark from '@/assets/brand/mark.svg'
import { languages, locale, setLanguage, isLanguage, t } from '@/i18n'

// wide: a page with more than a short form (the setup wizard)
defineProps<{ title: string; wide?: boolean }>()
function choose(e: Event) {
  const v = (e.target as HTMLSelectElement).value
  if (isLanguage(v)) setLanguage(v)
}
</script>

<template>
  <div class="auth-page">
    <main class="auth-box" :class="{ 'auth-box-wide': wide }">
      <div class="auth-brand"><img :src="mark" alt="" width="28" height="28" /> MeetHub</div>
      <h1 class="auth-title">{{ title }}</h1>
      <slot />
      <label class="lbl mt-4 block" for="lang">{{ t('Language') }}</label>
      <select id="lang" class="inp w-full" :value="locale" @change="choose">
        <option v-for="l in languages" :key="l.id" :value="l.id">{{ l.label }}</option>
      </select>
    </main>
  </div>
</template>

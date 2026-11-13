<script setup lang="ts">
// Emoji picker: frequent emoji first, search by short name; emits the short name (the API stores names, not characters).
import { computed, onMounted, ref } from 'vue'
import { gemoji } from 'gemoji'
import { t } from '@/i18n'

const emit = defineEmits<{ pick: [name: string]; close: [] }>()
const FREQUENT = [
  '+1',
  'white_check_mark',
  'heart',
  'joy',
  'tada',
  'rocket',
  'eyes',
  'pray',
  'fire',
  'ok_hand',
  'raised_hands',
  'thinking',
]
const q = ref('')
const input = ref<HTMLInputElement | null>(null)
const results = computed(() => {
  const s = q.value.trim().toLowerCase().replace(/^:/, '')
  if (!s) return FREQUENT.map((n) => ({ name: n, emoji: gemoji.find((g) => g.names.includes(n))?.emoji ?? '' }))
  const out: { name: string; emoji: string }[] = []
  for (const g of gemoji) {
    const name = g.names.find((n) => n.includes(s))
    if (name) out.push({ name, emoji: g.emoji })
    if (out.length >= 48) break
  }
  return out
})
onMounted(() => input.value?.focus())
</script>

<template>
  <div
    class="emoji-picker select-pop"
    role="dialog"
    :aria-label="t('Choose an emoji')"
    @keydown.esc.stop="emit('close')"
  >
    <input
      ref="input"
      v-model="q"
      class="inp"
      type="search"
      :placeholder="t('Search emoji')"
      :aria-label="t('Search emoji')"
    />
    <div class="emoji-grid">
      <button
        v-for="e in results"
        :key="e.name"
        type="button"
        class="emoji-btn"
        :title="`:${e.name}:`"
        :aria-label="e.name"
        @click="emit('pick', e.name)"
      >
        {{ e.emoji }}
      </button>
    </div>
    <p v-if="results.length === 0" class="text-xs text-fg-muted">{{ t('No emoji found.') }}</p>
  </div>
</template>

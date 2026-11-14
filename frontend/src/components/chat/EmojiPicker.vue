<script setup lang="ts">
// Emoji picker: the whole gemoji set (1 870 emoji, the names the server accepts) by category, the frequent ones
// first, and a search over names, tags and descriptions; emits the short name (the API stores names, not characters).
// Only the open category is rendered, so opening the picker stays instant.
import { computed, onMounted, ref } from 'vue'
import { gemoji } from 'gemoji'
import { t } from '@/i18n'

const emit = defineEmits<{ pick: [name: string]; close: [] }>()
const FREQUENT = [
  '+1',
  '-1',
  'white_check_mark',
  'heart',
  'joy',
  'smile',
  'tada',
  'rocket',
  'eyes',
  'pray',
  'fire',
  'ok_hand',
  'raised_hands',
  'thinking',
  'clap',
  '100',
  'wave',
  'muscle',
  'sob',
  'sunglasses',
  'bulb',
  'warning',
  'calendar',
  'hourglass',
]
interface Entry {
  name: string
  emoji: string
  title: string
}
const entry = (g: (typeof gemoji)[number], name = g.names[0]!): Entry => ({ name, emoji: g.emoji, title: `:${name}:` })

// the categories of the data, each under its first emoji; the labels are literals so the catalogs translate them
const categories = computed(() => [
  { id: 'frequent', icon: '🕒', label: t('Frequently used') },
  { id: 'Smileys & Emotion', icon: '😀', label: t('Smileys & Emotion') },
  { id: 'People & Body', icon: '👋', label: t('People & Body') },
  { id: 'Animals & Nature', icon: '🐶', label: t('Animals & Nature') },
  { id: 'Food & Drink', icon: '🍏', label: t('Food & Drink') },
  { id: 'Travel & Places', icon: '🌍', label: t('Travel & Places') },
  { id: 'Activities', icon: '🎃', label: t('Activities') },
  { id: 'Objects', icon: '💡', label: t('Objects') },
  { id: 'Symbols', icon: '💟', label: t('Symbols') },
  { id: 'Flags', icon: '🏁', label: t('Flags') },
])
const category = ref('frequent')
const q = ref('')
const input = ref<HTMLInputElement | null>(null)

const results = computed<Entry[]>(() => {
  const s = q.value.trim().toLowerCase().replace(/^:/, '').replace(/:$/, '')
  if (s) {
    const out: Entry[] = []
    for (const g of gemoji) {
      const name = g.names.find((n) => n.includes(s))
      if (name) out.push(entry(g, name))
      else if (g.tags.some((tag) => tag.includes(s)) || g.description.includes(s)) out.push(entry(g))
      if (out.length >= 160) break
    }
    return out
  }
  if (category.value === 'frequent')
    return FREQUENT.map((n) => gemoji.find((g) => g.names.includes(n))).flatMap((g, i) =>
      g ? [entry(g, FREQUENT[i])] : [],
    )
  return gemoji.filter((g) => g.category === category.value).map((g) => entry(g))
})
const heading = computed(() =>
  q.value.trim() ? t('Results') : (categories.value.find((c) => c.id === category.value)?.label ?? ''),
)

function choose(id: string) {
  category.value = id
  q.value = ''
}
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
    <div class="emoji-tabs" role="tablist" :aria-label="t('Emoji categories')">
      <button
        v-for="c in categories"
        :key="c.id"
        type="button"
        role="tab"
        class="emoji-btn"
        :class="{ on: !q.trim() && category === c.id }"
        :aria-selected="!q.trim() && category === c.id"
        :aria-label="c.label"
        :title="c.label"
        :data-emoji-category="c.id"
        @click="choose(c.id)"
      >
        {{ c.icon }}
      </button>
    </div>
    <p class="emoji-heading">{{ heading }}</p>
    <div class="emoji-grid" role="tabpanel" :aria-label="heading">
      <button
        v-for="e in results"
        :key="e.name"
        type="button"
        class="emoji-btn"
        :title="e.title"
        :aria-label="e.name"
        @click="emit('pick', e.name)"
      >
        {{ e.emoji }}
      </button>
    </div>
    <p v-if="results.length === 0" class="text-xs text-fg-muted">{{ t('No emoji found.') }}</p>
  </div>
</template>

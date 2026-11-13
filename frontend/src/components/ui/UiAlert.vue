<script setup lang="ts">
// Inline alert banner for messages tied to a page or form.
import { computed } from 'vue'
import { CircleAlert, CircleCheck, Info, TriangleAlert, X } from '@lucide/vue'
import { t } from '@/i18n'

const props = withDefaults(
  defineProps<{
    kind?: 'info' | 'success' | 'warning' | 'error'
    title?: string
    action?: string
    dismissible?: boolean
  }>(),
  { kind: 'info', title: '', action: '', dismissible: false },
)
const emit = defineEmits<{ dismiss: []; action: [] }>()
const icon = computed(
  () => ({ info: Info, success: CircleCheck, warning: TriangleAlert, error: CircleAlert })[props.kind],
)
</script>

<template>
  <div class="alert" :class="`alert-${kind}`" :role="kind === 'error' ? 'alert' : 'status'">
    <component :is="icon" :size="16" class="alert-icon" aria-hidden="true" />
    <div class="min-w-0 flex-1">
      <div v-if="title" class="alert-title">{{ title }}</div>
      <div class="alert-body"><slot /></div>
    </div>
    <button v-if="action" type="button" class="btn-ghost alert-action" @click="emit('action')">{{ action }}</button>
    <button v-if="dismissible" type="button" class="alert-close" :aria-label="t('Dismiss')" @click="emit('dismiss')">
      <X :size="14" aria-hidden="true" />
    </button>
  </div>
</template>

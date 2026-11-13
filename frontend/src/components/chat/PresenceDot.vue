<script setup lang="ts">
// A presence dot: the color is never the only signal (the status is also its accessible name and tooltip), and the
// ring in the sidebar's text color keeps it visible on every theme.
import { computed } from 'vue'
import type { Presence } from '@/chat/types'
import { t } from '@/i18n'

const props = defineProps<{ status?: Presence }>()
const label = computed(
  () =>
    ({ online: t('Online'), away: t('Away'), dnd: t('Do not disturb'), offline: t('Offline') })[
      props.status ?? 'offline'
    ],
)
</script>

<template>
  <span class="presence" :class="status ?? 'offline'" role="img" :aria-label="label" :title="label" />
</template>

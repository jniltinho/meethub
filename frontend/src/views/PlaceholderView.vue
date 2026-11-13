<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { Inbox } from '@lucide/vue'
import { t } from '@/i18n'

const route = useRoute()
// translated inside the computed (follows a language change); each literal is visible to the catalog test
const title = computed(
  () =>
    ({
      Activity: t('Activity'),
      Conversations: t('Conversations'),
      Teams: t('Teams'),
      Meetings: t('Meetings'),
      Files: t('Files'),
    })[route.meta.title ?? ''] ?? '',
)
</script>

<template>
  <section class="empty-state">
    <Inbox :size="40" aria-hidden="true" />
    <h1 class="page-title">{{ title }}</h1>
    <p>{{ t('This section arrives in a later version of MeetHub.') }}</p>
  </section>
</template>

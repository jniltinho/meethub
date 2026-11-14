<script setup lang="ts">
// The channel header's primary meeting button (chat-ui spec): "Start meeting", or "Join meeting (n)" while people are
// in the channel's room.
import { computed } from 'vue'
import { Video } from '@lucide/vue'
import { useMeetings } from '@/stores/meetings'
import { t } from '@/i18n'

const props = defineProps<{ channelId: string }>()
const meetings = useMeetings()
const n = computed(() => meetings.people(props.channelId).length)
const label = computed(() => (n.value ? t('Join meeting ({n})', { n: n.value }) : t('Start meeting')))
</script>

<template>
  <button
    v-if="meetings.enabled"
    type="button"
    class="btn start-meeting"
    :title="label"
    data-start-meeting
    @click="meetings.openCall(channelId)"
  >
    <Video :size="15" aria-hidden="true" /><span class="max-lg:sr-only">{{ label }}</span>
  </button>
</template>

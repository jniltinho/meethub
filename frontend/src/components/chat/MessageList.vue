<script setup lang="ts">
// The timeline: day separators, consecutive messages of one author within 5 minutes grouped (Mattermost), older
// messages loaded when scrolling up (keeping the position), and kept at the bottom while the reader is there.
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import MessageItem from './MessageItem.vue'
import { dayKey, formatDay } from '@/chat/format'
import type { LocalPost } from '@/chat/types'
import { t } from '@/i18n'

const props = defineProps<{
  posts: LocalPost[]
  canModerate?: boolean
  inThread?: boolean
  complete?: boolean
  readonly?: boolean
  label: string
}>()
const emit = defineEmits<{ older: []; reply: [id: string] }>()
const box = ref<HTMLElement | null>(null)
const items = ref<InstanceType<typeof MessageItem>[]>([])
const GROUP_MS = 5 * 60 * 1000

const rows = computed(() =>
  props.posts.map((p, i) => {
    const prev = props.posts[i - 1]
    const newDay = !prev || dayKey(prev.create_at) !== dayKey(p.create_at)
    const grouped =
      !!prev &&
      !newDay &&
      prev.user_id === p.user_id &&
      !prev.delete_at &&
      p.create_at - prev.create_at < GROUP_MS &&
      (props.inThread || !prev.reply_count)
    return { post: p, newDay, grouped }
  }),
)

let atBottom = true
let loading = false
function onScroll() {
  const el = box.value
  if (!el) return
  atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 48
  if (el.scrollTop < 80 && !props.complete && !loading) {
    loading = true
    const before = el.scrollHeight
    emit('older')
    // the parent loads; keep the reader's position once the new rows render
    setTimeout(async () => {
      await nextTick()
      el.scrollTop += el.scrollHeight - before
      loading = false
    }, 250)
  }
}

function toBottom() {
  const el = box.value
  if (el) el.scrollTop = el.scrollHeight
}

watch(
  () => props.posts.length,
  async () => {
    if (!atBottom) return
    await nextTick()
    toBottom()
  },
)
onMounted(async () => {
  await nextTick()
  toBottom()
})

/** editLast puts the viewer's last message in edit mode (Up in an empty composer) */
function editLast(userId: string): boolean {
  for (let i = props.posts.length - 1; i >= 0; i--) {
    const p = props.posts[i]!
    if (p.user_id === userId && !p.delete_at && !p.local) {
      items.value.find((it) => (it.$el as HTMLElement)?.dataset?.postId === p.id)?.startEdit()
      return true
    }
  }
  return false
}
defineExpose({ toBottom, editLast })
</script>

<template>
  <div ref="box" class="timeline" role="log" :aria-label="label" tabindex="0" @scroll.passive="onScroll">
    <p v-if="complete && posts.length === 0" class="empty-state">{{ t('No messages yet. Say hello!') }}</p>
    <p v-else-if="complete && !inThread" class="timeline-start">
      {{ t('This is the beginning of the conversation.') }}
    </p>
    <template v-for="r in rows" :key="r.post.id">
      <div v-if="r.newDay" class="day-sep" role="separator">
        <span>{{ formatDay(r.post.create_at) }}</span>
      </div>
      <MessageItem
        ref="items"
        :post="r.post"
        :grouped="r.grouped"
        :can-moderate="canModerate"
        :in-thread="inThread"
        :readonly="readonly"
        @reply="emit('reply', $event)"
      />
    </template>
  </div>
</template>

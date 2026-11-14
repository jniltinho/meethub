<script setup lang="ts">
// The timeline: dated day separators, consecutive messages of one author within 5 minutes grouped (Mattermost),
// older messages loaded when scrolling up (keeping the position), and kept at the bottom while the reader is there.
// After a jump to an old message the window is detached from the present: the message is centered and highlighted,
// scrolling down loads the newer ones and "Jump to recent" returns to the newest.
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { ArrowDown } from '@lucide/vue'
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
  /** the window does not reach the present (after a jump): newer messages load on scroll */
  detached?: boolean
  /** the message to center and highlight */
  highlight?: string
}>()
const emit = defineEmits<{ older: []; reply: [id: string]; newer: []; recent: []; seen: [] }>()
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
let loadingNewer = false
function onScroll() {
  const el = box.value
  if (!el) return
  atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 48
  if (props.detached && !loadingNewer && el.scrollHeight - el.scrollTop - el.clientHeight < 120) {
    loadingNewer = true
    emit('newer')
    setTimeout(() => (loadingNewer = false), 400)
  }
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
    if (!atBottom || props.detached || props.highlight) return
    await nextTick()
    toBottom()
  },
)

/** showHighlight centers the message a jump landed on; the mark fades after a moment. */
let fade: ReturnType<typeof setTimeout> | null = null
async function showHighlight() {
  if (!props.highlight) return
  await nextTick()
  const el = box.value?.querySelector<HTMLElement>(`[data-post-id="${CSS.escape(props.highlight)}"]`)
  if (!el) return
  atBottom = false
  el.scrollIntoView({ block: 'center' })
  if (fade) clearTimeout(fade)
  fade = setTimeout(() => emit('seen'), 2500)
}
watch(() => props.highlight, showHighlight)
onMounted(async () => {
  await nextTick()
  if (props.highlight) await showHighlight()
  else toBottom()
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
        :highlighted="r.post.id === highlight"
        @reply="emit('reply', $event)"
      />
    </template>
    <button v-if="detached" type="button" class="btn-ghost jump-recent" @click="emit('recent')">
      <ArrowDown :size="14" aria-hidden="true" />{{ t('Jump to recent') }}
    </button>
  </div>
</template>

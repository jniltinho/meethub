<script setup lang="ts">
// One message: author and time (unless grouped), Markdown text, edited mark, reactions, thread summary and the
// actions the viewer may use (reply, react, edit in place, delete after a themed confirmation).
import { computed, nextTick, ref } from 'vue'
import { MessageSquare, Pencil, RotateCcw, SmilePlus, Trash2 } from '@lucide/vue'
import EmojiPicker from './EmojiPicker.vue'
import { renderMessage, emojiFor } from '@/chat/render'
import { formatTime, initials } from '@/chat/format'
import { useChat } from '@/stores/chat'
import { useSession } from '@/stores/session'
import type { LocalPost } from '@/chat/types'
import { askConfirm } from '@/feedback/dialogs'
import { toast } from '@/feedback/toasts'
import { ApiError } from '@/api/client'
import { errorMessage, t } from '@/i18n'

const props = defineProps<{
  post: LocalPost
  grouped?: boolean
  canModerate?: boolean
  inThread?: boolean
  readonly?: boolean
}>()
const emit = defineEmits<{ reply: [id: string] }>()
const chat = useChat()
const session = useSession()
const me = computed(() => session.profile?.user.id ?? '')
const mine = computed(() => props.post.user_id === me.value)
const author = computed(() => chat.displayName(props.post.user_id) || t('Someone'))
const html = computed(() =>
  props.post.delete_at ? '' : renderMessage(props.post.message ?? '', { me: session.profile?.user.username ?? '' }),
)
const editing = ref(false)
const draft = ref('')
const picker = ref(false)
const editor = ref<HTMLTextAreaElement | null>(null)
const stored = computed(() => !props.post.local && !props.post.delete_at)

function fail(e: unknown) {
  toast.error(e instanceof ApiError ? errorMessage(e.code, e.message) : t('Unexpected error.'))
}

async function startEdit() {
  draft.value = props.post.message ?? ''
  editing.value = true
  await nextTick()
  editor.value?.focus()
}
defineExpose({ startEdit })

async function saveEdit() {
  const text = draft.value.trim()
  if (!text || text === props.post.message) {
    editing.value = false
    return
  }
  try {
    await chat.edit(props.post.id, text)
    editing.value = false
  } catch (e) {
    fail(e)
  }
}

function editKey(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.preventDefault()
    editing.value = false
  } else if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
    e.preventDefault()
    void saveEdit()
  }
}

async function remove() {
  const ok = await askConfirm(t('Delete this message? This cannot be undone.'), { ok: t('Delete'), danger: true })
  if (!ok) return
  try {
    await chat.remove(props.post.id)
  } catch (e) {
    fail(e)
  }
}

async function toggle(name: string) {
  picker.value = false
  const g = props.post.reactions?.find((r) => r.emoji_name === name)
  try {
    await chat.react(props.post.id, name, !g?.user_ids.includes(me.value))
  } catch (e) {
    fail(e)
  }
}

function reactors(userIds: string[], count: number): string {
  const names = userIds.map((u) => (u === me.value ? t('you') : chat.displayName(u) || '…'))
  const more = count - names.length
  return more > 0 ? t('{names} and {n} more', { names: names.join(', '), n: more }) : names.join(', ')
}
</script>

<template>
  <article
    class="msg"
    :class="{ grouped, pending: post.local === 'pending', failed: post.local === 'failed', deleted: !!post.delete_at }"
    :aria-label="t('{name} at {time}', { name: author, time: formatTime(post.create_at) })"
    :data-post-id="post.id"
  >
    <div class="msg-gutter">
      <span v-if="!grouped" class="chat-avatar" aria-hidden="true">{{ initials(author) }}</span>
      <time v-else class="msg-time-hover" :datetime="new Date(post.create_at).toISOString()">{{
        formatTime(post.create_at)
      }}</time>
    </div>
    <div class="msg-body">
      <header v-if="!grouped" class="msg-head">
        <span class="msg-author">{{ author }}</span>
        <time class="msg-time" :datetime="new Date(post.create_at).toISOString()">{{
          formatTime(post.create_at)
        }}</time>
      </header>

      <p v-if="post.delete_at" class="msg-deleted">{{ t('(message deleted)') }}</p>
      <div v-else-if="editing" class="msg-edit">
        <textarea
          ref="editor"
          v-model="draft"
          class="inp composer-input"
          rows="2"
          :aria-label="t('Edit message')"
          @keydown="editKey"
        />
        <p class="text-xs text-fg-muted">{{ t('Enter to save, Escape to cancel') }}</p>
      </div>
      <!-- eslint-disable-next-line vue/no-v-html -- sanitized by the fixed pipeline of chat/render.ts -->
      <div v-else class="msg-text" v-html="html" />
      <span v-if="post.edit_at && !post.delete_at && !editing" class="msg-edited">{{ t('(edited)') }}</span>

      <p v-if="post.local === 'failed'" class="msg-failed" role="alert">
        {{ t('Not sent.') }}
        <button type="button" class="act" @click="chat.retry(post.id).catch(fail)">
          <RotateCcw :size="12" aria-hidden="true" />{{ t('Retry') }}
        </button>
      </p>

      <div v-if="post.reactions?.length && !post.delete_at" class="reactions">
        <button
          v-for="r in post.reactions"
          :key="r.emoji_name"
          type="button"
          class="reaction"
          :class="{ mine: r.user_ids.includes(me) }"
          :aria-pressed="r.user_ids.includes(me)"
          :aria-label="t(':{name}: by {who}', { name: r.emoji_name, who: reactors(r.user_ids, r.count) })"
          :title="reactors(r.user_ids, r.count)"
          :disabled="readonly"
          @click="toggle(r.emoji_name)"
        >
          <span aria-hidden="true">{{ emojiFor(r.emoji_name) }}</span> {{ r.count }}
        </button>
      </div>

      <button
        v-if="!inThread && post.reply_count > 0 && !post.root_id"
        type="button"
        class="thread-link"
        @click="emit('reply', post.id)"
      >
        <MessageSquare :size="14" aria-hidden="true" />
        {{ post.reply_count === 1 ? t('1 reply') : t('{n} replies', { n: post.reply_count }) }}
        <span class="text-fg-muted">· {{ t('last reply {time}', { time: formatTime(post.last_reply_at) }) }}</span>
      </button>
    </div>

    <div v-if="stored && !editing && !readonly" class="msg-actions" role="toolbar" :aria-label="t('Message actions')">
      <button
        type="button"
        class="icon-btn"
        :aria-label="t('Add reaction')"
        :title="t('Add reaction')"
        @click="picker = !picker"
      >
        <SmilePlus :size="15" aria-hidden="true" />
      </button>
      <button
        v-if="!inThread"
        type="button"
        class="icon-btn"
        :aria-label="t('Reply')"
        :title="t('Reply')"
        @click="emit('reply', post.root_id || post.id)"
      >
        <MessageSquare :size="15" aria-hidden="true" />
      </button>
      <button v-if="mine" type="button" class="icon-btn" :aria-label="t('Edit')" :title="t('Edit')" @click="startEdit">
        <Pencil :size="15" aria-hidden="true" />
      </button>
      <button
        v-if="mine || canModerate"
        type="button"
        class="icon-btn"
        :aria-label="t('Delete')"
        :title="t('Delete')"
        @click="remove"
      >
        <Trash2 :size="15" aria-hidden="true" />
      </button>
      <EmojiPicker v-if="picker" class="msg-picker" @pick="toggle" @close="picker = false" />
    </div>
  </article>
</template>

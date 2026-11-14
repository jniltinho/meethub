<script setup lang="ts">
// The composer (chat-ui spec): Enter sends, Shift+Enter adds a line; the message shows at once as pending and
// stays as failed with Retry when it cannot be sent; @username autocomplete, emoji picker, formatting buttons,
// typing notifications (the server throttles them), Up in an empty composer edits the last own message.
import { computed, nextTick, ref } from 'vue'
import { AtSign, Bold, Code, Italic, Link, List, ListOrdered, SendHorizontal, Smile } from '@lucide/vue'
import EmojiPicker from './EmojiPicker.vue'
import { api, ApiError } from '@/api/client'
import { insertAt, linkSelection, prefixLines, wrapSelection, type Edit } from '@/chat/compose'
import type { Page, PublicUser } from '@/chat/types'
import { useChat } from '@/stores/chat'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'

const props = defineProps<{ channelId: string; rootId?: string; placeholder: string; disabled?: boolean }>()
const emit = defineEmits<{ editLast: [] }>()
const chat = useChat()
const text = ref('')
const area = ref<HTMLTextAreaElement | null>(null)
const picker = ref(false)
const suggestions = ref<PublicUser[]>([])
const active = ref(0)
const listId = computed(() => `mention-list-${props.rootId || props.channelId}`)
let lastTyping = 0
let lookup: ReturnType<typeof setTimeout> | null = null

function focus() {
  area.value?.focus()
}
defineExpose({ focus })

async function submit() {
  const message = text.value.trim()
  if (!message || props.disabled) return
  text.value = ''
  suggestions.value = []
  try {
    await chat.send(props.channelId, message, props.rootId ?? '')
  } catch (e) {
    // the message stays in the timeline as failed with Retry; rate limits and validation also get a toast
    if (e instanceof ApiError && e.status !== 0) toast.error(errorMessage(e.code, e.message))
  }
}

/** the @word being typed right before the caret, if any */
function mentionQuery(): { start: number; q: string } | null {
  const el = area.value
  if (!el) return null
  const upto = text.value.slice(0, el.selectionStart)
  const m = /(^|\s)@([\p{L}\p{N}._-]*)$/u.exec(upto)
  return m ? { start: upto.length - m[2]!.length - 1, q: m[2]! } : null
}

function onInput() {
  const now = Date.now()
  if (text.value && now - lastTyping > 3000) {
    lastTyping = now
    chat.sendTyping(props.channelId, props.rootId ?? '')
  }
  const mq = mentionQuery()
  if (lookup) clearTimeout(lookup)
  if (!mq) {
    suggestions.value = []
    return
  }
  lookup = setTimeout(async () => {
    try {
      const page = await api<Page<PublicUser>>(
        'GET',
        `/users?per_page=8&in_channel=${props.channelId}&q=${encodeURIComponent(mq.q)}`,
      )
      chat.rememberUsers(page.items)
      suggestions.value = page.items
      active.value = 0
    } catch {
      suggestions.value = []
    }
  }, 120)
}

async function choose(u: PublicUser) {
  const mq = mentionQuery()
  if (!mq) return
  const el = area.value!
  const before = text.value.slice(0, mq.start)
  const after = text.value.slice(el.selectionStart)
  text.value = `${before}@${u.username} ${after}`
  suggestions.value = []
  await nextTick()
  const pos = before.length + u.username.length + 2
  el.setSelectionRange(pos, pos)
  el.focus()
}

function onKeydown(e: KeyboardEvent) {
  if (suggestions.value.length) {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      const n = suggestions.value.length
      active.value = (active.value + (e.key === 'ArrowDown' ? 1 : n - 1)) % n
      return
    }
    if (e.key === 'Enter' || e.key === 'Tab') {
      e.preventDefault()
      void choose(suggestions.value[active.value]!)
      return
    }
    if (e.key === 'Escape') {
      e.preventDefault()
      suggestions.value = []
      return
    }
  }
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
    e.preventDefault()
    void submit()
  } else if (e.key === 'ArrowUp' && !text.value && !e.altKey) {
    e.preventDefault()
    emit('editLast')
  }
}

/** change applies a toolbar transform (chat/compose.ts) to the text and restores the selection it returns. */
async function change(fn: (text: string, start: number, end: number) => Edit) {
  const el = area.value!
  const start = el.selectionStart ?? text.value.length
  const e = fn(text.value, start, el.selectionEnd ?? start)
  text.value = e.text
  await nextTick()
  el.setSelectionRange(e.start, e.end)
  el.focus()
}
const insert = (s: string) => change((tx, a, b) => insertAt(tx, a, b, s))
const wrap = (mark: string) => change((tx, a, b) => wrapSelection(tx, a, b, mark))

/** the mention button types "@" where the caret is and opens the people list */
async function mention() {
  const el = area.value!
  const before = text.value.slice(0, el.selectionStart ?? text.value.length)
  await insert(before && !/\s$/.test(before) ? ' @' : '@')
  onInput()
}

function pickEmoji(name: string) {
  picker.value = false
  void insert(`:${name}: `)
}
</script>

<template>
  <form class="composer" @submit.prevent="submit">
    <div class="composer-tools" role="toolbar" :aria-label="t('Formatting')">
      <button type="button" class="icon-btn" :aria-label="t('Bold')" :title="t('Bold')" @click="wrap('**')">
        <Bold :size="15" aria-hidden="true" />
      </button>
      <button type="button" class="icon-btn" :aria-label="t('Italic')" :title="t('Italic')" @click="wrap('_')">
        <Italic :size="15" aria-hidden="true" />
      </button>
      <button type="button" class="icon-btn" :aria-label="t('Code')" :title="t('Code')" @click="wrap('`')">
        <Code :size="15" aria-hidden="true" />
      </button>
      <button type="button" class="icon-btn" :aria-label="t('Link')" :title="t('Link')" @click="change(linkSelection)">
        <Link :size="15" aria-hidden="true" />
      </button>
      <button
        type="button"
        class="icon-btn"
        :aria-label="t('Bulleted list')"
        :title="t('Bulleted list')"
        @click="change((tx, a, b) => prefixLines(tx, a, b, false))"
      >
        <List :size="15" aria-hidden="true" />
      </button>
      <button
        type="button"
        class="icon-btn"
        :aria-label="t('Numbered list')"
        :title="t('Numbered list')"
        @click="change((tx, a, b) => prefixLines(tx, a, b, true))"
      >
        <ListOrdered :size="15" aria-hidden="true" />
      </button>
    </div>
    <div class="relative">
      <textarea
        ref="area"
        v-model="text"
        class="composer-input"
        rows="1"
        :placeholder="placeholder"
        :aria-label="placeholder"
        :disabled="disabled"
        role="combobox"
        aria-autocomplete="list"
        :aria-expanded="suggestions.length > 0"
        :aria-controls="listId"
        :aria-activedescendant="suggestions.length ? `${listId}-${active}` : undefined"
        data-composer
        @input="onInput"
        @keydown="onKeydown"
        @blur="chat.sendActivity()"
      />
      <ul
        v-if="suggestions.length"
        :id="listId"
        class="select-pop mention-pop"
        role="listbox"
        :aria-label="t('People')"
      >
        <li
          v-for="(u, i) in suggestions"
          :id="`${listId}-${i}`"
          :key="u.id"
          role="option"
          :aria-selected="i === active"
          :class="{ on: i === active }"
          @mousedown.prevent="choose(u)"
        >
          <strong>@{{ u.username }}</strong> <span class="text-fg-muted">{{ u.display_name }}</span>
        </li>
      </ul>
    </div>
    <div class="composer-foot">
      <div class="relative">
        <button
          type="button"
          class="icon-btn"
          :aria-label="t('Insert emoji')"
          :title="t('Insert emoji')"
          @click="picker = !picker"
        >
          <Smile :size="16" aria-hidden="true" />
        </button>
        <EmojiPicker v-if="picker" class="composer-picker" @pick="pickEmoji" @close="picker = false" />
      </div>
      <button
        type="button"
        class="icon-btn"
        :aria-label="t('Mention someone')"
        :title="t('Mention someone')"
        @click="mention"
      >
        <AtSign :size="16" aria-hidden="true" />
      </button>
      <span class="flex-1 text-right text-xs text-fg-muted max-sm:hidden">{{ t('Shift + Enter for a new line') }}</span>
      <button type="submit" class="btn" :disabled="disabled || !text.trim()" :aria-label="t('Send')" :title="t('Send')">
        <SendHorizontal :size="15" aria-hidden="true" />
      </button>
    </div>
  </form>
</template>

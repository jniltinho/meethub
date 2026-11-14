<script setup lang="ts">
// Development/test-only page (absent from production builds): every toast, alert and dialog kind, for the visual
// and contrast checks of each theme.
import UiAlert from '@/components/ui/UiAlert.vue'
import { askConfirm, confirmTyped, showAlert } from '@/feedback/dialogs'
import { toast } from '@/feedback/toasts'

function allToasts() {
  toast.success('Saved successfully.')
  toast.info('A new version is available.')
  toast.warning('Your session ends in 5 minutes.')
  toast.error('The server could not be reached.')
}
</script>

<template>
  <div data-gallery>
    <h1 class="page-title">Feedback gallery</h1>
    <div class="mb-3 flex flex-wrap gap-2">
      <button type="button" class="btn" data-open="toasts" @click="allToasts">Toasts</button>
      <button type="button" class="btn-ghost" data-open="alert" @click="showAlert('Maintenance tonight at 22:00.')">
        Alert dialog
      </button>
      <button
        type="button"
        class="btn-ghost"
        data-open="confirm"
        @click="askConfirm('Disable Ana Silva?', { danger: true, ok: 'Disable' })"
      >
        Confirm dialog
      </button>
      <button
        type="button"
        class="btn-danger"
        data-open="typed"
        @click="confirmTyped('This cannot be undone.', 'ana@example.com')"
      >
        Typed confirm
      </button>
    </div>
    <div class="grid max-w-xl gap-2">
      <UiAlert kind="info" title="Information">Meetings arrive in the next version.</UiAlert>
      <UiAlert kind="success" title="Success">Profile saved.</UiAlert>
      <UiAlert kind="warning" title="Attention" action="Review">Two members have no role.</UiAlert>
      <UiAlert kind="error" title="Error" dismissible>Invalid e-mail or password.</UiAlert>
    </div>
    <form
      class="mt-4 grid max-w-xs gap-1"
      novalidate
      @submit.prevent="($event.target as HTMLFormElement).reportValidity()"
    >
      <label class="lbl" for="g-email">E-mail</label>
      <input id="g-email" class="inp" type="email" required />
      <button type="submit" class="btn mt-2" data-open="validate">Validate</button>
    </form>

    <!-- chat recipes: sidebar rows (unread, mention badge, presence, selected), mention highlight, reaction,
         reconnecting; and the layout's elements: team tiles, pinned banner and mark, search result, jump button -->
    <div class="mt-4 flex flex-wrap gap-4" data-gallery-chat>
      <div class="sidebar w-64 py-2">
        <a class="chat-item unread"
          ><span aria-hidden="true">#</span><span class="flex-1">devops</span><span class="chat-badge">1</span></a
        >
        <a class="chat-item on"><span aria-hidden="true">#</span><span class="flex-1">projects</span></a>
        <a class="chat-item"
          ><span class="presence online" role="img" aria-label="Online" /><span class="flex-1">Bruno Martins</span></a
        >
        <a class="chat-item"
          ><span class="presence away" role="img" aria-label="Away" /><span class="flex-1">Carla Pereira</span></a
        >
        <a class="chat-item"
          ><span class="presence dnd" role="img" aria-label="Do not disturb" /><span class="flex-1"
            >Diego Rocha</span
          ></a
        >
        <a class="chat-item"
          ><span class="presence offline" role="img" aria-label="Offline" /><span class="flex-1">Eduarda Lima</span></a
        >
      </div>
      <div class="channel-view max-w-md flex-1">
        <article class="msg">
          <div class="msg-gutter"><span class="chat-avatar" aria-hidden="true">AS</span></div>
          <div class="msg-body">
            <header class="msg-head">
              <span class="msg-author">Ana Silva</span><time class="msg-time">10:24</time>
            </header>
            <div class="msg-text">
              <p>
                Deploy at 17h, <span class="mention mention-me">@ana</span> and <span class="mention">@bruno</span>.
              </p>
            </div>
            <div class="reactions"><span class="reaction mine">🚀 3</span><span class="reaction">👍 1</span></div>
          </div>
        </article>
        <p class="reconnecting m-2">Reconnecting…</p>
        <div class="pinned-banner">
          <span class="pinned-banner-text"
            ><strong>Pinned by Ana Silva</strong><span class="truncate">Deploy checklist for the release</span></span
          >
        </div>
        <article class="msg pinned">
          <div class="msg-gutter"><span class="chat-avatar" aria-hidden="true">BM</span></div>
          <div class="msg-body">
            <p class="msg-pinned">Pinned</p>
            <div class="msg-text"><p>Rollback plan in the docs folder.</p></div>
          </div>
        </article>
        <article class="msg highlight">
          <div class="msg-gutter"><span class="chat-avatar" aria-hidden="true">CP</span></div>
          <div class="msg-body">
            <div class="msg-text"><p>The message a search result opened.</p></div>
          </div>
        </article>
        <span class="btn-ghost jump-recent">Jump to recent</span>
      </div>
      <div class="rail p-2">
        <ul class="team-tiles">
          <li><span class="team-tile on">TI</span></li>
          <li>
            <span class="team-tile">PD<span class="chat-badge">3</span></span>
          </li>
          <li>
            <span class="team-tile">RH<span class="unread-dot" /></span>
          </li>
        </ul>
      </div>
      <div class="side-panel">
        <div class="search-result">
          <span class="chat-avatar" aria-hidden="true">AS</span>
          <span class="min-w-0 flex-1">
            <span class="search-meta"><strong>Ana Silva</strong><span>#projects</span><time>Jan 21, 10:24</time></span>
            <span class="search-text">The <mark>deploy</mark> is at 17h.</span>
          </span>
        </div>
      </div>
    </div>
  </div>
</template>

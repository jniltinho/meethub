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
  </div>
</template>

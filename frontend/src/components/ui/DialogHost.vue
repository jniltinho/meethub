<script setup lang="ts">
// Renders the current queued dialog with <dialog>.showModal(): modal, focus kept inside by the browser, Escape is a
// cancel, initial focus on the safest action, focus returned to the opener afterwards.
import { computed, nextTick, ref, watch } from 'vue'
import { dialogState, settleDialog, typedMatches } from '@/feedback/dialogs'
import { t } from '@/i18n'

const dlg = ref<HTMLDialogElement | null>(null)
const safe = ref<HTMLButtonElement | null>(null)
const typedInput = ref<HTMLInputElement | null>(null)
const cur = computed(() => dialogState.current)
const okEnabled = computed(() => !!cur.value && (cur.value.kind !== 'typed' || typedMatches(cur.value)))
let opener: HTMLElement | null = null

watch(
  () => dialogState.current,
  async (req, prev) => {
    if (req && !prev) opener = document.activeElement as HTMLElement | null
    if (!req) {
      opener?.focus?.()
      opener = null
      return
    }
    await nextTick()
    const el = dlg.value
    if (el && !el.open && typeof el.showModal === 'function') el.showModal()
    ;(req.kind === 'typed' ? typedInput.value : safe.value)?.focus()
  },
)

function answer(ok: boolean) {
  if (ok && !okEnabled.value) return
  dlg.value?.close()
  settleDialog(ok)
}
</script>

<template>
  <dialog
    v-if="cur"
    :key="cur.title + cur.message"
    ref="dlg"
    class="pnl w-fit min-w-[min(360px,92vw)] max-w-[min(640px,92vw)]"
    :role="cur.kind === 'alert' ? 'dialog' : 'alertdialog'"
    aria-labelledby="dlg-title"
    aria-describedby="dlg-msg"
    @cancel.prevent="answer(false)"
  >
    <form @submit.prevent="answer(true)">
      <div id="dlg-title" class="pnl-head">{{ cur.title }}</div>
      <div class="pnl-body">
        <p id="dlg-msg" class="whitespace-pre-line" :class="{ 'mb-2 font-semibold text-err': cur.kind === 'typed' }">
          {{ cur.message }}
        </p>
        <template v-if="cur.kind === 'typed'">
          <label class="mb-1 block" for="dlg-typed">
            {{ t('Type') }} <strong>{{ cur.expected }}</strong> {{ t('to confirm:') }}
          </label>
          <input id="dlg-typed" ref="typedInput" v-model="cur.value" class="inp w-full" autocomplete="off" />
        </template>
      </div>
      <div class="pnl-foot">
        <button
          type="submit"
          :class="cur.danger ? 'btn-danger' : 'btn'"
          :disabled="!okEnabled"
          :ref="cur.kind === 'alert' ? 'safe' : undefined"
        >
          {{ cur.ok }}
        </button>
        <button v-if="cur.kind !== 'alert'" ref="safe" type="button" class="btn-ghost" @click="answer(false)">
          {{ cur.cancel }}
        </button>
      </div>
    </form>
  </dialog>
</template>

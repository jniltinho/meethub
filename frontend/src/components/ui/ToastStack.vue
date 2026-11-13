<script setup lang="ts">
// The toast stack is a manual popover in the top layer, so a toast shows above an open modal dialog; it is raised
// again on every new toast. Errors are announced assertively, the other kinds politely.
import { onMounted, ref, watch } from 'vue'
import { CircleAlert, CircleCheck, Info, TriangleAlert, X } from '@lucide/vue'
import { closeToast, pauseToast, resumeToast, toastState, type ToastKind } from '@/feedback/toasts'
import { t } from '@/i18n'

const stack = ref<HTMLElement | null>(null)
const raise = () => {
  const el = stack.value
  if (!el || typeof el.showPopover !== 'function') return
  if (el.matches(':popover-open')) el.hidePopover()
  el.showPopover()
}
onMounted(raise)
watch(
  () => toastState.items.length,
  (n, old) => n > old && raise(),
)

const css: Record<ToastKind, string> = { success: '', info: 'info', warning: 'warn', error: 'err' }
const icons = { success: CircleCheck, info: Info, warning: TriangleAlert, error: CircleAlert }
</script>

<template>
  <div id="toasts" ref="stack" popover="manual">
    <div
      v-for="it in toastState.items"
      :key="it.id"
      class="toast"
      :class="[css[it.kind], { show: it.show }]"
      :role="it.kind === 'error' ? 'alert' : 'status'"
      :aria-live="it.kind === 'error' ? 'assertive' : 'polite'"
      @mouseenter="pauseToast(it.id)"
      @mouseleave="resumeToast(it.id)"
      @focusin="pauseToast(it.id)"
      @focusout="resumeToast(it.id)"
    >
      <div class="toastHead flex items-center gap-1.5">
        <component :is="icons[it.kind]" :size="14" aria-hidden="true" />
        <span class="flex-1">{{ it.title }}</span>
        <button type="button" class="toast-close" :aria-label="t('Close')" @click="closeToast(it.id)">
          <X :size="13" aria-hidden="true" />
        </button>
      </div>
      <div class="toastBody">{{ it.message }}</div>
    </div>
  </div>
</template>

// Themed toasts (port of ad-manager's toasts.js): top right, slide in, hide after 4 s (errors 8 s), pause while
// hovered or focused and resume with the remaining time, at most 5 visible (the oldest goes first).
import { reactive } from 'vue'
import { t } from '@/i18n'

export type ToastKind = 'success' | 'info' | 'warning' | 'error'

export interface Toast {
  id: number
  kind: ToastKind
  title: string
  message: string
  show: boolean
}

export const MAX_TOASTS = 5
export const DURATION: Record<ToastKind, number> = { success: 4000, info: 4000, warning: 4000, error: 8000 }

export const toastState = reactive({ items: [] as Toast[] })

interface Timer {
  handle: ReturnType<typeof setTimeout> | null
  remaining: number
  startedAt: number
}
const timers = new Map<number, Timer>()
let seq = 0

function startTimer(id: number, ms: number): void {
  const tm: Timer = { handle: null, remaining: ms, startedAt: Date.now() }
  tm.handle = setTimeout(() => dismissToast(id), ms)
  timers.set(id, tm)
}

export function pushToast(kind: ToastKind, message: string, title?: string): number {
  const id = ++seq
  toastState.items.push({ id, kind, title: title ?? defaultTitle(kind), message, show: false })
  while (toastState.items.length > MAX_TOASTS) closeToast(toastState.items[0]!.id)
  setTimeout(() => {
    const it = toastState.items.find((x) => x.id === id)
    if (it) it.show = true
  }, 30)
  startTimer(id, DURATION[kind])
  return id
}

function defaultTitle(kind: ToastKind): string {
  return { success: t('Success'), info: t('Information'), warning: t('Attention'), error: t('Error') }[kind]
}

// pauseToast stops the countdown (pointer over the toast or focus inside it).
export function pauseToast(id: number): void {
  const tm = timers.get(id)
  if (!tm || tm.handle === null) return
  clearTimeout(tm.handle)
  tm.handle = null
  tm.remaining -= Date.now() - tm.startedAt
}

// resumeToast continues with the time that was left.
export function resumeToast(id: number): void {
  const tm = timers.get(id)
  if (!tm || tm.handle !== null) return
  tm.startedAt = Date.now()
  tm.handle = setTimeout(() => dismissToast(id), Math.max(tm.remaining, 0))
}

// dismissToast slides the toast out, then removes it.
export function dismissToast(id: number): void {
  const it = toastState.items.find((x) => x.id === id)
  if (!it) return
  it.show = false
  setTimeout(() => closeToast(id), 400)
}

export function closeToast(id: number): void {
  const tm = timers.get(id)
  if (tm?.handle) clearTimeout(tm.handle)
  timers.delete(id)
  const i = toastState.items.findIndex((x) => x.id === id)
  if (i >= 0) toastState.items.splice(i, 1)
}

export const toast = {
  success: (msg: string, title?: string) => pushToast('success', msg, title),
  info: (msg: string, title?: string) => pushToast('info', msg, title),
  warning: (msg: string, title?: string) => pushToast('warning', msg, title),
  error: (msg: string, title?: string) => pushToast('error', msg, title),
}

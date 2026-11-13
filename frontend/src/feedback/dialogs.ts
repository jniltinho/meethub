// Themed dialogs instead of alert()/confirm() (port of ad-manager's dialogs.js), promise-based and queued: one dialog
// at a time, later requests wait in order and no promise is lost. cancelDialogs() answers every pending one as
// cancelled (logout, expired session): nothing confirmed for the old session runs.
import { reactive } from 'vue'
import { t } from '@/i18n'

export type DialogKind = 'alert' | 'confirm' | 'typed'

export interface DialogRequest {
  kind: DialogKind
  title: string
  message: string
  ok: string
  cancel: string
  danger: boolean
  expected: string // typed confirm: the text to type
  value: string
  resolve: (ok: boolean) => void
}

export interface DialogOptions {
  title?: string
  ok?: string
  cancel?: string
  danger?: boolean
}

export const dialogState = reactive({ current: null as DialogRequest | null, queue: [] as DialogRequest[] })

function open(kind: DialogKind, message: string, opts: DialogOptions, expected = ''): Promise<boolean> {
  return new Promise((resolve) => {
    const req: DialogRequest = {
      kind,
      message,
      expected,
      value: '',
      danger: opts.danger ?? kind === 'typed',
      title: opts.title ?? { alert: t('Notice'), confirm: t('Confirmation'), typed: t('Safety confirmation') }[kind],
      ok: opts.ok ?? (kind === 'typed' ? t('Confirm') : t('OK')),
      cancel: opts.cancel ?? t('Cancel'),
      resolve,
    }
    if (dialogState.current) dialogState.queue.push(req)
    else dialogState.current = req
  })
}

export function askConfirm(message: string, opts: DialogOptions = {}): Promise<boolean> {
  return open('confirm', message, opts)
}

export async function showAlert(message: string, opts: DialogOptions = {}): Promise<void> {
  await open('alert', message, opts)
}

// confirmTyped guards destructive actions: OK enables only when the typed text matches (trimmed, case-insensitive).
export function confirmTyped(message: string, expected: string, opts: DialogOptions = {}): Promise<boolean> {
  return open('typed', message, opts, expected)
}

export function typedMatches(req: DialogRequest): boolean {
  return req.value.trim().toLowerCase() === req.expected.trim().toLowerCase()
}

// settleDialog answers the current dialog and opens the next queued one.
export function settleDialog(ok: boolean): void {
  const cur = dialogState.current
  if (!cur) return
  cur.resolve(cur.kind === 'typed' ? ok && typedMatches(cur) : cur.kind === 'alert' ? true : ok)
  dialogState.current = dialogState.queue.shift() ?? null
}

export function cancelDialogs(): void {
  const pending = [dialogState.current, ...dialogState.queue].filter((d): d is DialogRequest => d !== null)
  dialogState.current = null
  dialogState.queue = []
  for (const d of pending) d.resolve(d.kind === 'alert')
}

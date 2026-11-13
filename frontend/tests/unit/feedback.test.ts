import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { closeToast, MAX_TOASTS, pauseToast, resumeToast, toast, toastState } from '@/feedback/toasts'
import { askConfirm, cancelDialogs, confirmTyped, dialogState, settleDialog, showAlert } from '@/feedback/dialogs'

describe('toasts', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => {
    for (const it of toastState.items.slice()) closeToast(it.id)
    vi.useRealTimers()
  })

  it('success hides after 4 s, error after 8 s', () => {
    toast.success('Saved.')
    toast.error('Failed.')
    vi.advanceTimersByTime(4000)
    expect(toastState.items.find((t) => t.kind === 'success')?.show).toBe(false)
    expect(toastState.items.find((t) => t.kind === 'error')?.show).toBe(true)
    vi.advanceTimersByTime(4500)
    expect(toastState.items).toHaveLength(0)
  })

  it('pauses while hovered and resumes with the remaining time', () => {
    const id = toast.info('Hello')
    vi.advanceTimersByTime(3000)
    pauseToast(id)
    vi.advanceTimersByTime(60_000) // pointer stays over it
    expect(toastState.items.find((t) => t.id === id)?.show).toBe(true)
    resumeToast(id)
    vi.advanceTimersByTime(900)
    expect(toastState.items.find((t) => t.id === id)?.show).toBe(true)
    vi.advanceTimersByTime(200) // 1 s was left
    expect(toastState.items.find((t) => t.id === id)?.show).toBe(false)
  })

  it(`keeps at most ${MAX_TOASTS}, removing the oldest`, () => {
    const ids = Array.from({ length: 7 }, (_, i) => toast.warning(`w${i}`))
    expect(toastState.items).toHaveLength(MAX_TOASTS)
    expect(toastState.items[0]!.id).toBe(ids[2])
  })

  it('gives each kind its title', () => {
    toast.error('x')
    expect(toastState.items[0]!.title).toBe('Error')
  })
})

describe('dialogs', () => {
  afterEach(() => cancelDialogs())

  it('confirm resolves with the answer', async () => {
    const p = askConfirm('Disable Ana?')
    expect(dialogState.current?.kind).toBe('confirm')
    settleDialog(true)
    await expect(p).resolves.toBe(true)
    expect(dialogState.current).toBeNull()
  })

  it('queues overlapping dialogs without losing a promise', async () => {
    const first = askConfirm('first')
    const second = askConfirm('second')
    const third = showAlert('third')
    expect(dialogState.current?.message).toBe('first')
    expect(dialogState.queue).toHaveLength(2)
    settleDialog(false)
    expect(dialogState.current?.message).toBe('second')
    settleDialog(true)
    settleDialog(true)
    await expect(first).resolves.toBe(false)
    await expect(second).resolves.toBe(true)
    await expect(third).resolves.toBeUndefined()
  })

  it('typed confirm only accepts the expected text (trimmed, case-insensitive)', async () => {
    const p = confirmTyped('Delete forever?', 'ana@example.com')
    dialogState.current!.value = 'ana@'
    settleDialog(true)
    await expect(p).resolves.toBe(false)
    const q = confirmTyped('Delete forever?', 'ana@example.com')
    dialogState.current!.value = 'ANA@example.com '
    settleDialog(true)
    await expect(q).resolves.toBe(true)
  })

  it('cancelDialogs answers every pending dialog as cancelled', async () => {
    const a = askConfirm('a')
    const b = confirmTyped('b', 'x')
    const c = showAlert('c')
    cancelDialogs()
    await expect(a).resolves.toBe(false)
    await expect(b).resolves.toBe(false)
    await expect(c).resolves.toBeUndefined()
    expect(dialogState.current).toBeNull()
  })
})

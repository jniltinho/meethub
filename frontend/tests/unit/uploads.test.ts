import { effectScope, nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { ApiError } from '@/api/client'
import { useUploads } from '@/chat/uploads'
import { useSession } from '@/stores/session'
import { closeToast, toastState } from '@/feedback/toasts'
import type { FileInfo } from '@/chat/types'

// each upload() call is captured here so the test settles it by hand
interface Call {
  form: FormData
  progress: (f: number) => void
  resolve: (v: { file_infos: FileInfo[] }) => void
  reject: (e: unknown) => void
  aborted: boolean
}
const calls: Call[] = []
vi.mock('@/api/client', async (original) => ({
  ...(await original<typeof import('@/api/client')>()),
  upload: (_path: string, form: FormData, progress: (f: number) => void) => {
    const call = { form, progress, aborted: false } as Call
    const done = new Promise((resolve, reject) => Object.assign(call, { resolve, reject }))
    calls.push(call)
    return { done, abort: () => (call.aborted = true) }
  },
}))

const file = (name: string, size = 4, type = 'text/plain') => new File([new Uint8Array(size)], name, { type })
const info = (id: string, name: string) => ({ id, name }) as FileInfo
const settled = async () => {
  await Promise.resolve()
  await nextTick()
}

describe('the composer attachments', () => {
  let scope: ReturnType<typeof effectScope>
  let uploads: ReturnType<typeof useUploads>
  beforeEach(() => {
    setActivePinia(createPinia())
    useSession().bootstrap = {
      setup_required: false,
      default_theme: 'meethub-dark',
      languages: ['en'],
      files: { max_size_bytes: 100, max_per_post: 2 },
    }
    calls.length = 0
    globalThis.URL.createObjectURL = () => 'blob:preview'
    globalThis.URL.revokeObjectURL = () => undefined
    scope = effectScope()
    uploads = scope.run(() => useUploads(() => 'channel-1'))!
  })
  afterEach(() => {
    scope.stop()
    for (const t of toastState.items.slice()) closeToast(t.id)
  })

  it('uploads each file at once, channel first, with progress, then ready', async () => {
    uploads.add([file('plan.txt'), file('photo.png', 8, 'image/png')])
    expect(uploads.items.value.map((u) => u.state)).toEqual(['uploading', 'uploading'])
    expect(uploads.busy.value).toBe(true)
    expect([...calls[0]!.form.keys()]).toEqual(['channel_id', 'files']) // the channel decides who may upload
    expect(calls[0]!.form.get('channel_id')).toBe('channel-1')
    expect(uploads.items.value[1]!.preview).toBe('blob:preview') // images preview locally, other files do not
    expect(uploads.items.value[0]!.preview).toBe('')

    calls[0]!.progress(0.5)
    expect(uploads.items.value[0]!.progress).toBe(0.5)
    calls[0]!.resolve({ file_infos: [info('f1', 'plan.txt')] })
    calls[1]!.resolve({ file_infos: [info('f2', 'photo.png')] })
    await settled()
    expect(uploads.items.value.map((u) => u.state)).toEqual(['ready', 'ready'])
    expect(uploads.busy.value).toBe(false)
  })

  it('refuses what the limits do not allow, with a themed message each', () => {
    uploads.add([file('empty.txt', 0), file('big.bin', 101), file('a.txt'), file('b.txt'), file('c.txt')])
    expect(uploads.items.value.map((u) => u.file.name)).toEqual(['a.txt', 'b.txt'])
    const said = toastState.items.map((t) => t.message).join(' | ')
    expect(said).toContain('empty.txt is empty.')
    expect(said).toContain('big.bin is larger than 100 B.')
    expect(said).toContain('at most 2 files')
  })

  it('a failed upload stays with its reason and can be retried or removed', async () => {
    uploads.add([file('plan.txt')])
    calls[0]!.reject(new ApiError(503, 'storage_unavailable', 'x'))
    await settled()
    const item = uploads.items.value[0]!
    expect(item.state).toBe('failed')
    expect(item.error).toContain('storage is unavailable')

    uploads.retry(item)
    expect(item.state).toBe('uploading')
    calls[1]!.reject(new ApiError(429, 'rate_limited', 'x'))
    await settled()
    expect(item.error).toContain('Too many uploads')

    uploads.remove(item)
    expect(uploads.items.value).toHaveLength(0)
  })

  it('removing an upload in flight cancels it', async () => {
    uploads.add([file('plan.txt')])
    uploads.remove(uploads.items.value[0]!)
    expect(calls[0]!.aborted).toBe(true)
    calls[0]!.reject(new ApiError(0, 'aborted', 'Cancelled.'))
    await settled()
    expect(uploads.items.value).toHaveLength(0)
  })

  it('take waits for the uploads in the tray; a file added meanwhile belongs to the next message', async () => {
    uploads.add([file('a.txt')])
    let taken: FileInfo[] | null | undefined
    void uploads.take().then((f) => (taken = f))
    uploads.add([file('later.txt')]) // after the message was submitted
    await settled()
    expect(taken).toBeUndefined() // still uploading
    calls[0]!.resolve({ file_infos: [info('f1', 'a.txt')] })
    await settled()
    await settled()
    expect(taken?.map((f) => f.id)).toEqual(['f1'])
    expect(uploads.items.value.map((u) => [u.file.name, u.state])).toEqual([['later.txt', 'uploading']])
  })

  it('take hands nothing over when an upload of the message failed: they all stay for Retry', async () => {
    uploads.add([file('a.txt'), file('b.txt')])
    let taken: FileInfo[] | null | undefined
    void uploads.take().then((f) => (taken = f))
    calls[0]!.resolve({ file_infos: [info('f1', 'a.txt')] })
    calls[1]!.reject(new ApiError(413, 'file_too_large', 'x'))
    await settled()
    await settled()
    expect(taken).toBeNull()
    expect(uploads.items.value.map((u) => [u.file.name, u.state])).toEqual([
      ['a.txt', 'ready'],
      ['b.txt', 'failed'],
    ])
  })
})

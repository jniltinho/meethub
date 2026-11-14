// The attachments of one composer (chat-ui spec): each chosen file uploads at once with its progress, can be
// removed or retried, and the ready ones leave with the message. The limits are checked here first, with themed
// messages; the server enforces them anyway.
import { computed, onScopeDispose, ref, watch } from 'vue'
import { ApiError, upload } from '@/api/client'
import { formatSize } from '@/chat/format'
import type { FileInfo } from '@/chat/types'
import { useSession } from '@/stores/session'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'

export interface UploadItem {
  key: number
  file: File
  state: 'uploading' | 'ready' | 'failed'
  /** 0 to 1 */
  progress: number
  error: string
  info?: FileInfo
  /** a local preview of an image, '' for other files */
  preview: string
  abort?: () => void
}

export function useUploads(channelId: () => string) {
  const session = useSession()
  const items = ref<UploadItem[]>([])
  const busy = computed(() => items.value.some((i) => i.state === 'uploading'))
  let seq = 0

  function start(item: UploadItem) {
    item.state = 'uploading'
    item.progress = 0
    item.error = ''
    const form = new FormData()
    form.append('channel_id', channelId()) // first: it decides who may upload
    form.append('files', item.file, item.file.name)
    const up = upload<{ file_infos: FileInfo[] }>('/files', form, (f) => (item.progress = f))
    item.abort = up.abort
    up.done.then(
      (r) => {
        item.info = r.file_infos[0]
        item.state = 'ready'
        item.progress = 1
      },
      (e: unknown) => {
        if (e instanceof ApiError && e.code === 'aborted') return
        item.state = 'failed'
        item.error = !(e instanceof ApiError)
          ? t('Unexpected error.')
          : e.status === 429
            ? t('Too many uploads. Wait a moment and try again.')
            : errorMessage(e.code, e.message)
      },
    )
  }

  /** add starts uploading the files the limits allow and says which ones they refuse. */
  function add(files: Iterable<File>) {
    const limits = session.bootstrap?.files ?? { max_size_bytes: 50 * 1024 * 1024, max_per_post: 10 }
    for (const file of files) {
      if (items.value.length >= limits.max_per_post) {
        toast.error(t('A message carries at most {n} files.', { n: limits.max_per_post }))
        break
      }
      if (file.size === 0) {
        toast.error(t('{name} is empty.', { name: file.name }))
        continue
      }
      if (file.size > limits.max_size_bytes) {
        toast.error(t('{name} is larger than {size}.', { name: file.name, size: formatSize(limits.max_size_bytes) }))
        continue
      }
      items.value.push({
        key: ++seq,
        file,
        state: 'uploading',
        progress: 0,
        error: '',
        preview: file.type.startsWith('image/') ? URL.createObjectURL(file) : '',
      })
      start(items.value[items.value.length - 1]!) // the reactive one
    }
  }

  function forget(item: UploadItem) {
    if (item.preview) URL.revokeObjectURL(item.preview)
  }

  function remove(item: UploadItem) {
    item.abort?.()
    forget(item)
    items.value = items.value.filter((i) => i.key !== item.key)
  }

  /** take waits for the uploads now in the tray (files added meanwhile belong to the next message) and hands
   *  their files over, which leave the tray; null when one of them failed: then they all stay, for Retry. */
  async function take(): Promise<FileInfo[] | null> {
    const keys = new Set(items.value.filter((i) => i.state !== 'failed').map((i) => i.key))
    const batch = () => items.value.filter((i) => keys.has(i.key)) // one removed while waiting is out
    const waiting = () => batch().some((i) => i.state === 'uploading')
    if (waiting())
      await new Promise<void>((resolve) => {
        const stop = watch(waiting, (w) => {
          if (!w) {
            stop()
            resolve()
          }
        })
      })
    const mine = batch()
    if (mine.some((i) => i.state === 'failed')) return null
    mine.forEach(forget)
    items.value = items.value.filter((i) => !keys.has(i.key))
    return mine.map((i) => i.info!)
  }

  onScopeDispose(() => items.value.forEach((i) => (i.abort?.(), forget(i))))
  return { items, busy, add, remove, retry: start, take }
}

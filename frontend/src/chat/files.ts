// Files of the chat: their URLs, the kind an icon stands for, and opening the message that carries one.
import type { Router } from 'vue-router'
import { api } from '@/api/client'
import { jumpToPost } from '@/chat/jump'
import type { FileInfo, Page, Post } from '@/chat/types'
import type { useChat } from '@/stores/chat'

/** fileUrl is the download of a file, or its server-made thumbnail or preview (images only). */
export function fileUrl(id: string, variant?: 'thumbnail' | 'preview'): string {
  return `/api/v1/files/${id}${variant ? '/' + variant : ''}`
}

export type FileKind = 'image' | 'sheet' | 'archive' | 'code' | 'text' | 'media' | 'file'

const kinds: Record<string, FileKind> = {}
for (const [kind, exts] of Object.entries({
  image: 'png jpg jpeg gif webp svg bmp heic avif',
  sheet: 'xls xlsx csv ods numbers',
  archive: 'zip tar gz tgz bz2 7z rar xz',
  code: 'js ts go py rb rs java c h cpp cs php sh json yaml yml toml xml html css sql vue',
  text: 'pdf doc docx odt rtf txt md ppt pptx odp pages key',
  media: 'mp3 wav ogg flac m4a mp4 mov mkv webm avi',
}))
  for (const ext of exts.split(' ')) kinds[ext] = kind as FileKind

/** fileKind groups a file by its name's extension (what the icon shows; never used to decide anything). */
export function fileKind(name: string): FileKind {
  return kinds[name.slice(name.lastIndexOf('.') + 1).toLowerCase()] ?? 'file'
}

/** jumpToFile opens the channel at the message that carries the file (a reply opens in its thread). */
export async function jumpToFile(chat: ReturnType<typeof useChat>, router: Router, f: FileInfo): Promise<void> {
  if (!f.post_id) return
  // any post of a thread answers with the thread's root first
  const thread = await api<Page<Post>>('GET', `/posts/${f.post_id}/thread?per_page=1`)
  const root = thread.items[0]
  await jumpToPost(chat, router, f.channel_id, f.post_id, root && root.id !== f.post_id ? root.id : '')
}

/** postExcerpt is one line standing for a message: its text, or the names of its files when it has none. */
export function postExcerpt(p: Post | undefined, max: number): string {
  const text = (p?.message ?? '').split('\n')[0]!
  return (text || (p?.metadata?.files ?? []).map((f) => f.name).join(', ')).slice(0, max)
}

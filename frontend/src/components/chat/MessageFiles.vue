<script setup lang="ts">
// The files of a message (chat-ui spec): images show the server-made thumbnail and open enlarged; any other file
// is a card that downloads it under its name.
import { ref } from 'vue'
import { Download } from '@lucide/vue'
import FileIcon from './FileIcon.vue'
import ImageDialog from './ImageDialog.vue'
import { fileUrl } from '@/chat/files'
import { formatSize } from '@/chat/format'
import type { FileInfo } from '@/chat/types'
import { t } from '@/i18n'

defineProps<{ files: FileInfo[] }>()
const open = ref<FileInfo | null>(null)

/** the thumbnail's box, known before it loads so the timeline does not jump (thumbnails are at most 240 px high) */
function box(f: FileInfo): { width: number; height: number } {
  const h = Math.min(180, f.height || 180)
  return { width: Math.max(1, Math.round(((f.width || h) * h) / (f.height || h))), height: h }
}
</script>

<template>
  <ul class="msg-files" :aria-label="t('Attachments')">
    <li v-for="f in files" :key="f.id">
      <button
        v-if="f.has_preview_image"
        type="button"
        class="file-thumb"
        :aria-label="t('Open image {name}', { name: f.name })"
        :title="f.name"
        @click="open = f"
      >
        <img :src="fileUrl(f.id, 'thumbnail')" :alt="f.name" loading="lazy" v-bind="box(f)" />
      </button>
      <a
        v-else
        class="file-card"
        :href="fileUrl(f.id)"
        :download="f.name"
        :aria-label="t('Download {name}', { name: f.name })"
        :title="t('Download {name}', { name: f.name })"
      >
        <FileIcon :name="f.name" :size="24" />
        <span class="file-text">
          <span class="file-name">{{ f.name }}</span>
          <span class="file-meta">{{ formatSize(f.size) }}</span>
        </span>
        <Download :size="15" aria-hidden="true" />
      </a>
    </li>
  </ul>
  <ImageDialog v-if="open" :file="open" @close="open = null" />
</template>

<script setup lang="ts">
// The enlarged preview of an image (chat-ui spec): the server-made preview on the themed modal, with the original
// one click away. Escape closes it and the focus returns to the thumbnail (ChatModal).
import { Download } from '@lucide/vue'
import ChatModal from './ChatModal.vue'
import { fileUrl } from '@/chat/files'
import { formatSize } from '@/chat/format'
import type { FileInfo } from '@/chat/types'
import { t } from '@/i18n'

defineProps<{ file: FileInfo }>()
const emit = defineEmits<{ close: [] }>()
</script>

<template>
  <ChatModal class="image-modal" :title="file.name" @close="emit('close')">
    <div class="image-modal-body">
      <img :src="fileUrl(file.id, 'preview')" :alt="file.name" :width="file.width" :height="file.height" />
    </div>
    <div class="pnl-foot image-modal-foot">
      <span class="file-meta">{{ formatSize(file.size) }} · {{ file.width }} × {{ file.height }}</span>
      <a class="btn" :href="fileUrl(file.id)" :download="file.name" autofocus>
        <Download :size="15" aria-hidden="true" />{{ t('Download') }}
      </a>
    </div>
  </ChatModal>
</template>

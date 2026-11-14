<script setup lang="ts">
// The icon of a file by its name's extension.
import { computed } from 'vue'
import { File, FileArchive, FileCode, FileImage, FileMusic, FileSpreadsheet, FileText } from '@lucide/vue'
import { fileKind } from '@/chat/files'

const props = withDefaults(defineProps<{ name: string; size?: number }>(), { size: 20 })
const icon = computed(
  () =>
    ({
      image: FileImage,
      sheet: FileSpreadsheet,
      archive: FileArchive,
      code: FileCode,
      text: FileText,
      media: FileMusic,
      file: File,
    })[fileKind(props.name)],
)
</script>

<template>
  <component :is="icon" :size="size" class="file-icon" aria-hidden="true" />
</template>

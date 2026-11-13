<script setup lang="ts">
import DialogHost from '@/components/ui/DialogHost.vue'
import ToastStack from '@/components/ui/ToastStack.vue'
</script>

<template>
  <RouterView />
  <ToastStack />
  <DialogHost />
</template>

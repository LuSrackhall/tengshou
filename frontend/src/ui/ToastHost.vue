<script setup lang="ts">
// 顶栏 toast 宿主：DOM 与迁移前 index.html 的 <div id="toast"> 完全一致，
// 由 ui/toast.ts 的 showToast() 驱动。所有 Vue 域经 bridge/app.ts 的 toast()
// 调用，不再依赖 window.__tengshouApp。
import { toastState } from "./toast";
</script>

<template>
  <Teleport to="body">
    <div
      id="toast"
      class="toast"
      :class="{ show: toastState.visible, error: toastState.isError }"
      role="status"
      aria-live="polite"
    >{{ toastState.message }}</div>
  </Teleport>
</template>

<script setup lang="ts">
// 公共 modal/backdrop 基础设施：DOM 结构与迁移前 app.js openModal 一致
// （modal-backdrop / modal、可选 wide、modal-head/body/foot），Escape 关闭。
// 各域已有自己的弹窗保持原样，新增外壳弹窗统一用本组件。
import { onBeforeUnmount, onMounted } from "vue";

withDefaults(defineProps<{ title: string; wide?: boolean }>(), { wide: false });

const emit = defineEmits<{ close: [] }>();

function onKeydown(event: KeyboardEvent): void {
  if (event.key === "Escape") {
    emit("close");
  }
}

onMounted(() => {
  document.addEventListener("keydown", onKeydown);
});

onBeforeUnmount(() => {
  document.removeEventListener("keydown", onKeydown);
});
</script>

<template>
  <Teleport to="body">
    <div class="modal-backdrop">
      <div
        class="modal"
        :class="{ wide }"
        role="dialog"
        aria-modal="true"
      >
        <div class="modal-head">
          <h2>{{ title }}</h2>
          <button type="button" class="close" aria-label="关闭" @click="emit('close')">×</button>
        </div>
        <div class="modal-body">
          <slot />
        </div>
        <div class="modal-foot">
          <slot name="foot" />
        </div>
      </div>
    </div>
  </Teleport>
</template>

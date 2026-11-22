<script setup lang="ts">
// 全屏 PDF 预览 overlay（阶段 1 迁移）：承接 app.js showPDFOverlay 在生成
// 流程里的用法。DOM 结构与类名不变（pdf-overlay / tpl-preview-head /
// tpl-preview-pages / tpl-preview-warnings），Escape 关闭，渲染用 index.html
// 的本地 vendor pdf.js（全局 pdfjsLib），不引入 CDN 或打包依赖。
import { onBeforeUnmount, onMounted, ref } from "vue";
import type { OverlayWarning } from "../../api/types";
import {
  renderPdfPagesInto,
  showPdfMessage,
  type PdfDocument,
} from "./pdf";

const props = defineProps<{
  title: string;
  bytes: Uint8Array;
  warnings: OverlayWarning[];
}>();

const emit = defineEmits<{ close: [] }>();

const pagesEl = ref<HTMLDivElement | null>(null);
let documentHandle: PdfDocument | null = null;
let disposed = false;

function onKeydown(event: KeyboardEvent): void {
  if (event.key !== "Escape") {
    return;
  }
  event.preventDefault();
  event.stopPropagation();
  emit("close");
}

onMounted(() => {
  // 捕获阶段拦截 Escape：先于合同弹窗的关闭处理器，只关 overlay。
  document.addEventListener("keydown", onKeydown, true);
  const container = pagesEl.value;
  if (!container) {
    return;
  }
  showPdfMessage(container, "正在渲染预览…");
  void renderPdfPagesInto(container, props.bytes, 0.9)
    .then((doc) => {
      if (disposed) {
        void doc.destroy().catch(() => undefined);
        return;
      }
      documentHandle = doc;
    })
    .catch((error: unknown) => {
      if (!disposed) {
        const message = error instanceof Error ? error.message : String(error);
        showPdfMessage(container, `预览渲染失败：${message}`);
      }
    });
});

onBeforeUnmount(() => {
  disposed = true;
  document.removeEventListener("keydown", onKeydown, true);
  if (documentHandle) {
    void documentHandle.destroy().catch(() => undefined);
    documentHandle = null;
  }
});
</script>

<template>
  <Teleport to="body">
    <div class="pdf-overlay">
      <div class="tpl-preview-head">
        <strong>{{ title }}</strong>
        <span class="spacer"></span>
        <button type="button" id="pdfOverlayClose" @click="emit('close')">关闭预览</button>
      </div>
      <div ref="pagesEl" class="tpl-preview-pages"></div>
      <div class="tpl-preview-warnings">
        <template v-if="warnings.length">
          <div
            v-for="(warning, index) in warnings"
            :key="`${warning.code}-${index}`"
            class="tpl-warning"
          >
            {{ warning.message || warning.code }}
          </div>
        </template>
        <div v-else class="tpl-status ok">未发现缺失字段或位置问题。</div>
      </div>
    </div>
  </Teleport>
</template>

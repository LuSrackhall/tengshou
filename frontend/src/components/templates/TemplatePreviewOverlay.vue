<script setup lang="ts">
// 标定编辑器内的全屏预览层（样例预览 / 合同预览 / 生成结果）。
// 结构与类名对应迁移前 showTemplatePreview；PDF 由本地 pdf.js 渲染。
// 预览层只负责展示与触发下载，下载/引导步骤记录由 TemplateEditor 处理。
import { onBeforeUnmount, onMounted, ref } from "vue";
import type { OverlayWarning } from "../../api/types";
import { renderPdfPagesInto } from "../pdf/pdf";
import type { PdfDocument } from "./pdfjs";
import type { PreviewDownload } from "./types";

const props = defineProps<{
  bytes: Uint8Array;
  warnings: OverlayWarning[];
  title: string;
  download: PreviewDownload | null;
}>();

const emit = defineEmits<{
  close: [];
  download: [];
}>();

const pagesEl = ref<HTMLDivElement | null>(null);
const loading = ref(true);
const errorText = ref("");
let doc: PdfDocument | null = null;

onMounted(() => {
  void (async () => {
    const host = pagesEl.value;
    if (!host) {
      return;
    }
    try {
      doc = await renderPdfPagesInto(host, props.bytes, 0.9);
    } catch (error) {
      errorText.value = error instanceof Error ? error.message : String(error);
    } finally {
      loading.value = false;
    }
  })();
});

onBeforeUnmount(() => {
  try {
    void doc?.destroy();
  } catch {
    /* 预览关闭时的清理失败不影响页面 */
  }
  doc = null;
});
</script>

<template>
  <div class="tpl-preview-overlay">
    <div class="tpl-preview-head">
      <strong>{{ title }}</strong>
      <span class="spacer"></span>
      <button
        v-if="download"
        type="button"
        id="tplPreviewDownload"
        @click="emit('download')"
      >
        {{ download.sample ? "下载试生成 PDF" : "下载 PDF" }}
      </button>
      <button type="button" id="tplPreviewClose" @click="emit('close')">关闭预览</button>
    </div>
    <div class="tpl-preview-pages">
      <div v-if="loading" class="tpl-loading">正在渲染预览…</div>
      <div v-if="errorText" class="tpl-loading">预览渲染失败：{{ errorText }}</div>
      <div
        ref="pagesEl"
        style="display: flex; flex-direction: column; align-items: center; gap: 16px; width: 100%"
      ></div>
    </div>
    <div class="tpl-preview-warnings">
      <template v-if="warnings.length">
        <div v-for="(warning, index) in warnings" :key="index" class="tpl-warning">
          {{ warning.message || warning.code }}
        </div>
      </template>
      <div v-else class="tpl-status ok">未发现位置或数据问题。</div>
    </div>
  </div>
</template>

<script setup lang="ts">
// 模板标定编辑器（阶段 1 迁出 app.js 的 openTemplateEditor 及 30+ 函数）。
// 状态与业务逻辑在 useTemplateEditor.ts（单文件 ≤ 600 行），本文件只负责
// 全屏 modal 的结构与组件接线；DOM/类名与迁移前一致。
import type { Template } from "../../api/types";
import type { TemplateEditorAction } from "../../bridge/app";
import EditorSidebar from "./EditorSidebar.vue";
import TemplatePageCanvas from "./TemplatePageCanvas.vue";
import TemplatePreviewOverlay from "./TemplatePreviewOverlay.vue";
import { ZOOM_OPTIONS, useTemplateEditor } from "./useTemplateEditor";

const props = defineProps<{
  template: Template;
  initialAction?: TemplateEditorAction;
}>();

const emit = defineEmits<{
  close: [];
}>();

const {
  setEditorRoot,
  placements,
  scale,
  selectedId,
  fields,
  seals,
  contracts,
  contractsLoaded,
  contractId,
  usageText,
  check,
  pdfDoc,
  pages,
  pageCount,
  loadingPages,
  pagesError,
  headerTitle,
  currentText,
  previewStatus,
  canGenerate,
  warnings,
  result,
  previewOverlay,
  previewOverlayKey,
  registerPlacementElement,
  selectPlacement,
  patchPlacement,
  removePlacement,
  addPlacement,
  updateFontSize,
  updateAlign,
  requestSave,
  requestClose,
  closeDirect,
  previewTemplate,
  generate,
  openGenerated,
  downloadGenerated,
  onPreviewDownload,
} = useTemplateEditor(props, {
  onClose: () => emit("close"),
});
</script>

<template>
  <Teleport to="body">
    <div class="modal-backdrop">
      <div class="modal full" role="dialog" aria-modal="true">
        <div class="modal-head">
          <h2>{{ headerTitle }}</h2>
          <button type="button" class="close" aria-label="关闭" @click="closeDirect">×</button>
        </div>
        <div class="modal-body">
          <div :ref="setEditorRoot" class="template-editor">
            <div class="tpl-toolbar">
              <span id="tplCurrent" class="tpl-current">{{ currentText }}</span>
              <label class="inline">缩放
                <select id="tplZoom" v-model.number="scale">
                  <option v-for="zoom in ZOOM_OPTIONS" :key="zoom" :value="zoom">
                    {{ Math.round(zoom * 100) }}%
                  </option>
                </select>
              </label>
              <span class="spacer"></span>
              <button type="button" id="tplSaveBtn" @click="requestSave">保存为新版本</button>
              <button type="button" id="tplCloseBtn" @click="requestClose">关闭</button>
            </div>
            <div class="tpl-body">
              <div id="tplPages" class="tpl-pages">
                <div v-if="loadingPages" class="tpl-loading">正在加载模板 PDF…</div>
                <div v-else-if="pagesError" class="tpl-loading">
                  模板 PDF 加载失败：{{ pagesError }}
                </div>
                <template v-else-if="pdfDoc">
                  <TemplatePageCanvas
                    v-for="page in pages"
                    :key="page.pageNumber"
                    :doc="pdfDoc"
                    :page-number="page.pageNumber"
                    :page-count="pageCount"
                    :scale="scale"
                    :page-size="page.pageSize"
                    :placements="placements"
                    :selected-id="selectedId"
                    :fields="fields"
                    :seals="seals"
                    :register-placement-element="registerPlacementElement"
                    @select="selectPlacement($event)"
                    @change="patchPlacement"
                    @remove="removePlacement"
                  />
                </template>
              </div>
              <EditorSidebar
                :contract-id="contractId"
                :placements="placements"
                :selected-id="selectedId"
                :page-count="pageCount"
                :fields="fields"
                :seals="seals"
                :contracts="contracts"
                :contracts-loaded="contractsLoaded"
                :usage-text="usageText"
                :check="check"
                :preview-status-text="previewStatus.text"
                :preview-status-class="previewStatus.cls"
                :can-generate="canGenerate"
                :warnings="warnings"
                :result="result"
                @update:contractId="contractId = $event"
                @add="addPlacement"
                @select="selectPlacement($event, true)"
                @remove="removePlacement"
                @update-font-size="updateFontSize"
                @update-align="updateAlign"
                @preview-sample="previewTemplate(true)"
                @preview-contract="previewTemplate(false)"
                @generate="generate"
                @open-result="openGenerated"
                @download-result="downloadGenerated"
              />
            </div>
            <TemplatePreviewOverlay
              v-if="previewOverlay"
              :key="previewOverlayKey"
              :bytes="previewOverlay.bytes"
              :warnings="previewOverlay.warnings"
              :title="previewOverlay.title"
              :download="previewOverlay.download"
              @close="previewOverlay = null"
              @download="onPreviewDownload"
            />
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>

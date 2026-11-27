<script setup lang="ts">
// 模板页（阶段 1 迁移）：搜索、模板版本列表、导入 Word/PDF、下载原件、
// 整改重出、版本删除与批量生成入口；标定编辑器在 TemplateEditor.vue。
// 结构与 app.js renderTemplates 的输出一致（toolbar / hint / summary /
// data-table / actions 类名不变），数据走 frontend/src/api 的 typed 端点。
//
// 跨域：导入后的标定、预览与生成在本域 Vue 组件内完成；批量生成对话框由
// components/contracts 的 Vue 组件承接，经 bridge/app.ts 调用。合同台账已
// 迁到 Vue，批量生成读取 window.__tengshouContracts 的当前筛选结果（缺席时
// 退回接口全量）。引导（components/onboarding）通过
// tengshou:open-template-editor / -import 打开本域。
// 外部写入经 shell/realtime.ts 派发的 tengshou:data-changed 到达后重取列表。
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { ApiError } from "../../api/client";
import {
  deleteTemplate as deleteTemplateRequest,
  downloadTemplateSource,
  getTemplate,
  listContracts,
  listConverters,
  listTemplates,
  startTemplateImportJob,
} from "../../api/endpoints";
import type {
  Contract,
  ConverterAvailability,
  Template,
  TemplateMutationResponse,
} from "../../api/types";
import {
  errorMessage,
  openBatchGenerateDialog,
  refreshAfterWrite,
  subscribeDataChanged,
  subscribeOpenTemplateEditor,
  subscribeOpenTemplateImport,
  toast,
  type TemplateEditorAction,
} from "../../bridge/app";
import { isJobCancelled, useJob } from "../../composables/useJob";
import TemplateEditor from "./TemplateEditor.vue";
import TemplateImportModal from "./TemplateImportModal.vue";
import {
  converterLabel,
  formatDateTime,
  readFileBase64,
  saveBlob,
  templateSourceName,
} from "./format";
import type { ImportErrorState } from "./import";

/** 导入上限与迁移前一致：40MB。 */
const IMPORT_MAX_BYTES = 40 * 1024 * 1024;
const TABLE_HEADS = [
  "模板名称",
  "版本",
  "页数",
  "转换方式",
  "标定位置",
  "状态",
  "更新时间",
  "操作",
];

interface ImportPayload {
  file: File;
  /** 文件内容的 base64（不含 data: 前缀）。 */
  base64: string;
  converters: ConverterAvailability[];
}

interface EditorPayload {
  template: Template;
  action?: TemplateEditorAction;
}

const templates = ref<Template[]>([]);
const total = ref(0);
const search = ref("");
const activeQuery = ref("");
const loaded = ref(false);
const importPayload = ref<ImportPayload | null>(null);
const importError = ref<ImportErrorState | null>(null);
/* 导入任务由本域持有而不是弹窗：关闭弹窗后轮询继续，完成后按「后台完成」
   链路刷新列表；弹窗仍在时保持原有「导入后自动打开标定编辑器」。 */
const {
  job: importJob,
  running: importRunning,
  start: startImportJob,
} = useJob<TemplateMutationResponse>();
const editor = ref<EditorPayload | null>(null);
const fileInput = ref<HTMLInputElement | null>(null);

let loadSeq = 0;
let searchTimer: ReturnType<typeof setTimeout> | undefined;
let reloadTimer: ReturnType<typeof setTimeout> | undefined;
let unsubData: (() => void) | null = null;
let unsubEditor: (() => void) | null = null;
let unsubImport: (() => void) | null = null;

const summary = computed(() => {
  const familyNames = new Set(templates.value.map((item) => item.name));
  return (
    `共 ${familyNames.size} 个模板 · ${total.value} 个版本` +
    (activeQuery.value ? ` · 搜索「${activeQuery.value}」` : "")
  );
});

async function fetchData(): Promise<void> {
  const seq = ++loadSeq;
  const query = search.value.trim();
  try {
    const data = await listTemplates({
      limit: 500,
      kind: "pdf",
      ...(query ? { q: query } : {}),
    });
    if (seq !== loadSeq) {
      return;
    }
    templates.value = data.items ?? [];
    total.value = data.total ?? 0;
    activeQuery.value = query;
  } catch (error) {
    if (seq !== loadSeq) {
      return;
    }
    toast(errorMessage(error), true);
  } finally {
    if (seq === loadSeq) {
      loaded.value = true;
    }
  }
}

function scheduleFetch(delay: number): void {
  if (reloadTimer !== undefined) {
    clearTimeout(reloadTimer);
  }
  reloadTimer = setTimeout(() => {
    reloadTimer = undefined;
    void fetchData();
  }, delay);
}

watch(search, () => {
  if (searchTimer !== undefined) {
    clearTimeout(searchTimer);
  }
  searchTimer = setTimeout(() => {
    searchTimer = undefined;
    void fetchData();
  }, 250);
});

function triggerImport(): void {
  const input = fileInput.value;
  if (!input) {
    return;
  }
  input.value = "";
  input.click();
}

async function onImportFile(event: Event): Promise<void> {
  const file = (event.target as HTMLInputElement).files?.[0];
  if (!file) {
    return;
  }
  if (file.size > IMPORT_MAX_BYTES) {
    toast("模板文件过大，请压缩到 40MB 以内", true);
    return;
  }
  let base64: string;
  try {
    base64 = await readFileBase64(file);
  } catch (error) {
    toast(errorMessage(error), true);
    return;
  }
  const converterData = await listConverters().catch(() => null);
  importError.value = null;
  importPayload.value = {
    file,
    base64,
    converters: converterData?.items ?? [],
  };
}

function openEditor(template: Template, action?: TemplateEditorAction): void {
  editor.value = { template, action };
}

function describeImportError(error: unknown): ImportErrorState {
  if (error instanceof ApiError) {
    return { fieldErrors: error.fields, message: error.message };
  }
  return { fieldErrors: [], message: errorMessage(error) };
}

/** 启动后台导入任务；弹窗保持打开时展示阶段进度与错误。 */
async function startImport(name: string): Promise<void> {
  const payload = importPayload.value;
  if (!payload || importRunning.value) {
    return;
  }
  importError.value = null;
  try {
    const result = await startImportJob(() =>
      startTemplateImportJob({
        name,
        file_name: payload.file.name,
        file_base64: payload.base64,
      }),
    );
    await finishImport(result);
  } catch (error) {
    if (isJobCancelled(error)) {
      return;
    }
    const state = describeImportError(error);
    if (importPayload.value) {
      importError.value = state;
    } else {
      toast(state.message, true);
    }
  }
}

/**
 * 导入完成：弹窗仍在时沿用存量行为（toast 后自动打开标定编辑器）；
 * 已关闭（转到后台）时只提示并刷新列表——模板会自动出现在列表里。
 */
async function finishImport(result: TemplateMutationResponse): Promise<void> {
  const warnings = result.warnings ?? [];
  toast(warnings.length ? warnings[0] ?? "" : `模板已导入（v${result.item.version}）`);
  if (warnings.length > 1) {
    setTimeout(() => toast(warnings.slice(1).join("；")), 400);
  }
  const modalOpen = importPayload.value !== null;
  importPayload.value = null;
  importError.value = null;
  try {
    await refreshAfterWrite();
  } catch (error) {
    toast(errorMessage(error), true);
  }
  if (modalOpen) {
    openEditor(result.item);
  }
}

/** 关闭导入弹窗：任务继续在后台跑，明确告知完成后的去向。 */
function closeImport(): void {
  if (importRunning.value) {
    toast("转换在后台继续，完成后模板会自动出现在列表。");
  }
  importPayload.value = null;
  importError.value = null;
}

async function openEditorFromEvent(
  templateId: string,
  action?: TemplateEditorAction,
): Promise<void> {
  let template = templates.value.find((item) => item.id === templateId);
  if (!template) {
    try {
      template = await getTemplate(templateId);
    } catch (error) {
      toast(errorMessage(error), true);
      return;
    }
  }
  openEditor(template, action);
}

async function removeTemplate(template: Template): Promise<void> {
  if (
    !window.confirm(
      `确定删除模板「${template.name}」v${template.version} 吗？已被合同或 PDF 引用的版本会保留。`,
    )
  ) {
    return;
  }
  try {
    await deleteTemplateRequest(template.id);
    toast(`模板「${template.name}」v${template.version} 已删除`);
    await refreshAfterWrite();
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

async function rerender(template: Template): Promise<void> {
  try {
    const data = await listContracts({ limit: 500 });
    const contracts = (data.items ?? []).filter((contract) => {
      const pinned = contract.template_id
        ? templates.value.find((item) => item.id === contract.template_id)
        : undefined;
      return Boolean(
        pinned && pinned.name === template.name && pinned.version < template.version,
      );
    });
    if (!contracts.length) {
      toast(`没有引用「${template.name}」旧版本、需要整改重出的合同`, true);
      return;
    }
    openBatchGenerateDialog({
      title: `整改重出 · ${template.name} v${template.version} · ${contracts.length} 份合同`,
      contracts,
      templateId: template.id,
      kind: "regenerate",
    });
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

async function downloadSource(template: Template): Promise<void> {
  try {
    // 迁移前固定用「名称-v版本.ext」作为下载名（app.js downloadAsset 的
    // source 分支）；接口响应头里的 template-source.* 只是服务端默认名。
    const file = await downloadTemplateSource(template.id);
    saveBlob(file.blob, templateSourceName(template));
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

async function ledgerContracts(): Promise<Contract[]> {
  const ledger = window.__tengshouContracts;
  if (ledger) {
    return ledger.currentContracts();
  }
  const data = await listContracts({ limit: 500 });
  return data.items ?? [];
}

async function batchGenerate(): Promise<void> {
  try {
    const contracts = await ledgerContracts();
    if (!contracts.length) {
      toast("当前台账没有合同；请先切换年度或新建合同", true);
      return;
    }
    const active =
      templates.value.find((item) => item.is_active) ?? templates.value[0];
    openBatchGenerateDialog({
      title: "批量生成 PDF",
      contracts,
      templateId: active ? active.id : "",
    });
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

onMounted(() => {
  void fetchData();
  unsubData = subscribeDataChanged((detail) => {
    const entities = detail.entities;
    if (entities.length === 0 || entities.includes("template")) {
      scheduleFetch(150);
    }
  });
  unsubEditor = subscribeOpenTemplateEditor((detail) => {
    void openEditorFromEvent(detail.templateId, detail.action);
  });
  unsubImport = subscribeOpenTemplateImport(() => triggerImport());
});

onBeforeUnmount(() => {
  unsubData?.();
  unsubData = null;
  unsubEditor?.();
  unsubEditor = null;
  unsubImport?.();
  unsubImport = null;
  if (searchTimer !== undefined) {
    clearTimeout(searchTimer);
    searchTimer = undefined;
  }
  if (reloadTimer !== undefined) {
    clearTimeout(reloadTimer);
    reloadTimer = undefined;
  }
});
</script>

<template>
  <div class="toolbar">
    <input
      id="templateSearch"
      v-model="search"
      type="search"
      placeholder="搜索模板名称"
      autocomplete="off"
    >
    <span class="spacer"></span>
    <button type="button" id="importTemplateBtn" class="primary" @click="triggerImport">导入模板</button>
    <button type="button" id="templateBatchGenerateBtn" @click="batchGenerate">批量生成</button>
    <button type="button" id="refreshTemplatesBtn" @click="fetchData">刷新</button>
  </div>
  <p class="hint">
    导入您自己的 Word 合同模板（.docx）：本机有 Word 时直接用 Word 转成 PDF，否则用
    LibreOffice 或先另存为 PDF 导入。转换后以 PDF 页面显示，拖框标定字段、签名和公章位置。
    每次保存都会生成新版本，旧合同仍按旧版本重出。
  </p>
  <div id="templateSummary" class="summary">{{ loaded ? summary : "" }}</div>
  <div class="table-wrap">
    <table id="templateTable" class="data-table">
      <thead>
        <tr>
          <th v-for="head in TABLE_HEADS" :key="head">{{ head }}</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="template in templates"
          :key="template.id"
          :data-id="template.id"
          @click="openEditor(template)"
        >
          <td class="wrap">{{ template.name }}</td>
          <td><span class="badge" :class="{ active: template.is_active }">v{{ template.version }}</span></td>
          <td class="row-num">{{ template.page_count || 0 }}</td>
          <td>{{ converterLabel(template.converter) }}</td>
          <td class="row-num">{{ (template.placements || []).length }}</td>
          <td>{{ template.is_active ? "当前版本" : "历史版本" }}</td>
          <td class="row-num">{{ formatDateTime(template.updated_at) }}</td>
          <td>
            <div class="actions">
              <button type="button" data-action="edit" @click.stop="openEditor(template)">标定与生成</button>
              <button type="button" data-action="rerender" @click.stop="rerender(template)">整改重出</button>
              <button type="button" data-action="source" @click.stop="downloadSource(template)">原文件</button>
              <button type="button" class="danger" data-action="delete" @click.stop="removeTemplate(template)">删除</button>
            </div>
          </td>
        </tr>
        <tr v-if="loaded && !templates.length">
          <td class="empty" colspan="8">
            还没有 PDF 模板。点击「导入模板」选择您的 Word 合同模板（或另存好的 PDF）。
          </td>
        </tr>
      </tbody>
    </table>
  </div>
  <input
    ref="fileInput"
    type="file"
    accept=".docx,.pdf"
    hidden
    @change="onImportFile"
  >
  <TemplateImportModal
    v-if="importPayload"
    :file="importPayload.file"
    :converters="importPayload.converters"
    :job="importJob"
    :errors="importError"
    @close="closeImport"
    @submit="startImport"
  />
  <TemplateEditor
    v-if="editor"
    :template="editor.template"
    :initial-action="editor.action"
    @close="editor = null"
  />
</template>

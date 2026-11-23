<script setup lang="ts">
// Excel 导入对话框（阶段 1 收口）：完整承接迁移前 app.js 的 openImport /
// renderImport / showImportResult。
//
// 交互与文案逐字保留：模式/跳过错误/自动建档、列映射与「重新预览」提示、
// 冲突逐行决议、删除候选、逐行错误/警告、提交结果与 422 逐行错误回显。
// 数据走 typed 端点（previewExcelImport / commitExcelImport）。

import { computed, nextTick, onMounted, ref } from "vue";
import { ApiError } from "../../api/client";
import { commitExcelImport, previewExcelImport } from "../../api/endpoints";
import type {
  ImportOptions,
  ImportPreview,
  ImportResolution,
  ImportResult,
  ImportRow,
} from "../../api/types";
import { errorMessage, refreshAfterWrite, toast } from "../../bridge/app";
import ModalDialog from "../../ui/ModalDialog.vue";
import {
  fieldLabelByKey,
  importActionLabel,
  importResultToast,
} from "./format";

const props = defineProps<{ file: File }>();
const emit = defineEmits<{ close: [] }>();

interface DialogOptions {
  mode: "upsert" | "create";
  skip_errors: boolean;
  create_parties: boolean;
  apply_deletes: boolean;
  file_name: string;
}

const base64 = ref("");
const preview = ref<ImportPreview | null>(null);
const mapping = ref<Record<string, string>>({});
const options = ref<DialogOptions>({
  mode: "upsert",
  skip_errors: false,
  create_parties: true,
  apply_deletes: false,
  file_name: props.file.name,
});
const resolutions = ref<Record<string, ImportResolution>>({});
const dirty = ref(false);
const loading = ref(false);
const submitting = ref(false);
const fatalError = ref("");
const result = ref<ImportResult | null>(null);
const resultBlocked = ref(false);
const resultEl = ref<HTMLDivElement | null>(null);

const title = computed(() => `导入 Excel · ${props.file.name}`);

const conflictRows = computed<ImportRow[]>(() =>
  (preview.value?.rows ?? []).filter(
    (row) => row.row > 0 && row.action === "conflict",
  ),
);
const deleteRows = computed<ImportRow[]>(() =>
  (preview.value?.rows ?? []).filter((row) => row.row === 0),
);
const dataRows = computed<ImportRow[]>(() =>
  (preview.value?.rows ?? []).filter(
    (row) => row.row > 0 && row.action !== "conflict",
  ),
);

const summaryBadges = computed(() => {
  const summary = preview.value?.summary;
  return [
    { cls: "create", label: "新增", count: summary?.create },
    { cls: "update", label: "更新", count: summary?.update },
    { cls: "unchanged", label: "不变", count: summary?.unchanged },
    { cls: "delete", label: "删除", count: summary?.deleted },
    { cls: "conflict", label: "冲突", count: summary?.conflicts },
    { cls: "error", label: "错误", count: summary?.error },
  ];
});

const resultBadges = computed(() => {
  const item = result.value;
  return [
    { cls: "create", label: "新增", count: item?.created },
    { cls: "update", label: "更新", count: item?.updated },
    { cls: "unchanged", label: "不变", count: item?.unchanged },
    { cls: "delete", label: "删除", count: item?.deleted },
    { cls: "conflict", label: "冲突", count: item?.conflicts },
    { cls: "error", label: "错误", count: item?.errors },
  ];
});

function readFileBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const raw = String(reader.result ?? "");
      const index = raw.indexOf(",");
      resolve(index >= 0 ? raw.slice(index + 1) : raw);
    };
    reader.onerror = () => reject(new Error("读取文件失败"));
    reader.readAsDataURL(file);
  });
}

function requestOptions(includeMapping: boolean): ImportOptions {
  const request: ImportOptions = {
    mode: options.value.mode,
    skip_errors: options.value.skip_errors,
    create_parties: options.value.create_parties,
    apply_deletes: options.value.apply_deletes,
    file_name: options.value.file_name,
  };
  if (includeMapping) {
    request.mapping = { ...mapping.value };
  }
  return request;
}

function defaultResolutions(rows: ImportRow[] | undefined): Record<string, ImportResolution> {
  const next: Record<string, ImportResolution> = {};
  for (const row of rows ?? []) {
    if (row.action === "conflict") {
      // 迁移前每个冲突行的「保留系统」单选框默认选中。
      next[row.key] = "keep_system";
    }
  }
  return next;
}

async function runPreview(useCurrentMapping: boolean): Promise<void> {
  if (loading.value) {
    return;
  }
  loading.value = true;
  try {
    const response = await previewExcelImport({
      file_base64: base64.value,
      options: requestOptions(useCurrentMapping),
    });
    preview.value = response;
    mapping.value = { ...response.mapping };
    resolutions.value = defaultResolutions(response.rows);
    dirty.value = false;
    result.value = null;
    resultBlocked.value = false;
    fatalError.value = "";
  } finally {
    loading.value = false;
  }
}

onMounted(() => {
  void (async () => {
    try {
      base64.value = await readFileBase64(props.file);
    } catch (error) {
      toast(errorMessage(error), true);
      emit("close");
      return;
    }
    try {
      await runPreview(false);
    } catch (error) {
      fatalError.value = errorMessage(error);
    }
  })();
});

function rePreview(): void {
  void runPreview(true).catch((error: unknown) => {
    toast(errorMessage(error), true);
  });
}

function commitErrorResult(error: unknown): ImportResult | null {
  if (!(error instanceof ApiError)) {
    return null;
  }
  const payload = error.payload;
  if (typeof payload !== "object" || payload === null) {
    return null;
  }
  const body = payload as Record<string, unknown>;
  const value = body.result;
  if (typeof value !== "object" || value === null) {
    return null;
  }
  // 服务端响应边界：422 的 result 与 ImportResult 同构。
  return value as ImportResult;
}

async function commit(): Promise<void> {
  if (submitting.value) {
    return;
  }
  submitting.value = true;
  try {
    const request: ImportOptions = {
      ...requestOptions(true),
      apply_deletes: options.value.apply_deletes,
      resolutions: { ...resolutions.value },
    };
    const response = await commitExcelImport({
      file_base64: base64.value,
      options: request,
    });
    result.value = response;
    resultBlocked.value = false;
    toast(importResultToast(response));
    await refreshAfterWrite();
  } catch (error) {
    const blocked = commitErrorResult(error);
    if (blocked) {
      result.value = blocked;
      resultBlocked.value = true;
    }
    toast(errorMessage(error), true);
  } finally {
    submitting.value = false;
  }
  await nextTick();
  resultEl.value?.scrollIntoView({ block: "nearest" });
}

function mappingValue(index: number): string {
  return mapping.value[String(index)] ?? "";
}

function setMapping(index: number, event: Event): void {
  const select = event.target as HTMLSelectElement;
  if (select.value === "") {
    delete mapping.value[String(index)];
  } else {
    mapping.value[String(index)] = select.value;
  }
  dirty.value = true;
}

function conflictKeys(row: ImportRow): string[] {
  const system = row.system_values ?? {};
  const excel = row.excel_values ?? {};
  return Array.from(new Set([...Object.keys(system), ...Object.keys(excel)]));
}

function conflictValue(values: Record<string, string> | undefined, key: string): string {
  const value = values ? values[key] : undefined;
  return value === undefined || value === null ? "" : value;
}

function resolveName(row: ImportRow): string {
  return "resolve-" + String(row.key).replace(/[^a-zA-Z0-9_-]/g, "_");
}

function resolutionOf(key: string): ImportResolution {
  return resolutions.value[key] ?? "keep_system";
}

function setResolution(key: string, value: ImportResolution): void {
  resolutions.value[key] = value;
}

function setMode(event: Event): void {
  const select = event.target as HTMLSelectElement;
  options.value.mode = select.value === "create" ? "create" : "upsert";
}
</script>

<template>
  <ModalDialog :title="title" wide @close="emit('close')">
    <div v-if="fatalError" class="form-errors">{{ fatalError }}</div>
    <p v-else-if="!preview">正在解析并校验…</p>
    <template v-else>
      <div class="import-options">
        <label class="inline">模式
          <select id="importMode" :value="options.mode" @change="setMode">
            <option value="upsert">合并更新（编号已存在则更新）</option>
            <option value="create">仅新增（编号已存在则报错）</option>
          </select>
        </label>
        <label class="check-row"><input v-model="options.skip_errors" type="checkbox"> 跳过错误行</label>
        <label class="check-row"><input v-model="options.create_parties" type="checkbox"> 新相对方自动建档</label>
      </div>
      <p class="hint">文件：{{ file.name }} · 工作表：{{ preview.sheet }}。导入前先预览，确认列名映射与每一行的动作。</p>
      <div class="import-mapping">
        <div v-for="column in preview.columns ?? []" :key="column.index" class="map-row">
          <span>第 {{ column.index + 1 }} 列：{{ column.header || "（空表头）" }}</span>
          <select
            :data-map-index="column.index"
            :disabled="!column.header"
            :value="mappingValue(column.index)"
            @change="setMapping(column.index, $event)"
          >
            <option value="">（未映射）</option>
            <option v-for="field in preview.fields ?? []" :key="field.key" :value="field.key">
              {{ field.label }}（{{ field.key }}）
            </option>
          </select>
        </div>
      </div>
      <ul v-if="(preview.notes ?? []).length" class="import-notes">
        <li v-for="(note, index) in preview.notes ?? []" :key="index">{{ note }}</li>
      </ul>
      <p v-if="dirty" id="importDirtyHint" class="hint">列映射已修改，建议先点「重新预览」确认结果。</p>

      <div id="importResult" ref="resultEl">
        <template v-if="result">
          <div class="import-summary">
            <span
              v-for="badge in resultBadges"
              :key="badge.cls"
              class="badge"
              :class="badge.cls"
            >{{ badge.label }} {{ badge.count || 0 }}</span>
            <span v-if="result.skipped" class="badge">跳过 {{ result.skipped }}</span>
          </div>
          <div v-if="resultBlocked" class="form-errors">存在错误行，本次未写入任何数据。请修正 Excel 后重新导入，或勾选「跳过错误行」。</div>
          <div class="table-wrap">
            <table class="data-table import-table">
              <thead><tr><th>行</th><th>动作</th><th>合同编号</th><th>合同名称</th><th>问题</th></tr></thead>
              <tbody>
                <tr v-for="row in (result.rows ?? []).slice(0, 200)" :key="row.key">
                  <td class="row-num">{{ row.row || "—" }}</td>
                  <td><span class="badge" :class="row.action">{{ importActionLabel(row.action) }}</span></td>
                  <td class="mono">{{ row.contract_no || "" }}</td>
                  <td class="wrap">{{ row.title || "" }}</td>
                  <td class="wrap">
                    <ul v-if="(row.errors ?? []).length" class="row-errors">
                      <li v-for="(error, index) in row.errors ?? []" :key="index">{{ error.label || "" }}：{{ error.message }}</li>
                    </ul>
                  </td>
                </tr>
                <tr v-if="!(result.rows ?? []).length"><td class="empty" colspan="5">没有记录</td></tr>
              </tbody>
            </table>
          </div>
        </template>
      </div>

      <div class="import-summary">
        <span
          v-for="badge in summaryBadges"
          :key="badge.cls"
          class="badge"
          :class="badge.cls"
        >{{ badge.label }} {{ badge.count || 0 }}</span>
        <span class="badge">共 {{ preview.summary.total || 0 }} 行</span>
      </div>

      <template v-if="conflictRows.length">
        <h3 class="import-section-title">冲突（{{ conflictRows.length }} 行，需逐行选择）</h3>
        <div v-for="row in conflictRows" :key="row.key" class="import-conflict">
          <div class="import-conflict-head">
            <strong>第 {{ row.row }} 行 · {{ row.contract_no }} {{ row.title }}</strong>
            <span class="badge conflict">冲突</span>
          </div>
          <p class="hint">{{ row.conflict_reason || "导出后两边都有改动，需要选择保留哪一边。" }}</p>
          <div v-if="conflictKeys(row).length" class="table-wrap">
            <table class="data-table conflict-table">
              <thead><tr><th>字段</th><th>系统当前</th><th>Excel</th></tr></thead>
              <tbody>
                <tr v-for="key in conflictKeys(row)" :key="key">
                  <td>{{ fieldLabelByKey(preview, key) }}</td>
                  <td class="wrap">{{ conflictValue(row.system_values, key) }}</td>
                  <td class="wrap">{{ conflictValue(row.excel_values, key) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <div class="conflict-choice" :data-resolve-key="row.key">
            <label class="inline">
              <input
                type="radio"
                :name="resolveName(row)"
                value="keep_system"
                :checked="resolutionOf(row.key) === 'keep_system'"
                @change="setResolution(row.key, 'keep_system')"
              > 保留系统
            </label>
            <label class="inline">
              <input
                type="radio"
                :name="resolveName(row)"
                value="use_excel"
                :checked="resolutionOf(row.key) === 'use_excel'"
                @change="setResolution(row.key, 'use_excel')"
              > 采用 Excel
            </label>
          </div>
        </div>
      </template>

      <template v-if="deleteRows.length">
        <div class="import-deletes">
          <h3 class="import-section-title">删除候选（{{ deleteRows.length }} 行）</h3>
          <p class="hint">这些合同在导出快照中存在，但 Excel 里已经没有对应行。默认不删除；确认后才能勾选应用删除。</p>
          <label class="check-row"><input id="importApplyDeletes" v-model="options.apply_deletes" type="checkbox"> 应用删除（未勾选时这些行会被跳过）</label>
          <div class="table-wrap">
            <table class="data-table import-table">
              <thead><tr><th>合同编号</th><th>合同名称</th><th>相对方</th><th>处理</th></tr></thead>
              <tbody>
                <tr v-for="row in deleteRows" :key="row.key">
                  <td class="mono">{{ row.contract_no || "" }}</td>
                  <td class="wrap">{{ row.title || "" }}</td>
                  <td>{{ row.party_b || "" }}</td>
                  <td class="wrap">
                    <template v-if="row.action === 'conflict'">
                      <span class="badge conflict">系统有改动</span>
                      <div class="hint">{{ row.conflict_reason || "系统在导出后修改了该合同；默认保留系统。" }}</div>
                      <div class="conflict-choice" :data-resolve-key="row.key">
                        <label class="inline">
                          <input
                            type="radio"
                            :name="resolveName(row)"
                            value="keep_system"
                            :checked="resolutionOf(row.key) === 'keep_system'"
                            @change="setResolution(row.key, 'keep_system')"
                          > 保留系统（不删除）
                        </label>
                        <label class="inline">
                          <input
                            type="radio"
                            :name="resolveName(row)"
                            value="use_excel"
                            :checked="resolutionOf(row.key) === 'use_excel'"
                            @change="setResolution(row.key, 'use_excel')"
                          > 采用 Excel（删除）
                        </label>
                      </div>
                    </template>
                    <span v-else class="badge delete">将删除</span>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </template>

      <div class="table-wrap">
        <table class="data-table import-table">
          <thead><tr><th>行</th><th>动作</th><th>合同编号</th><th>合同名称</th><th>相对方</th><th>问题</th></tr></thead>
          <tbody>
            <tr v-for="row in dataRows.slice(0, 200)" :key="row.key">
              <td class="row-num">{{ row.row }}</td>
              <td><span class="badge" :class="row.action">{{ importActionLabel(row.action) }}</span></td>
              <td class="mono">{{ row.contract_no || "" }}</td>
              <td class="wrap">{{ row.title || "" }}</td>
              <td>{{ row.party_b || "" }}</td>
              <td class="wrap">
                <ul v-if="(row.errors ?? []).length" class="row-errors">
                  <li v-for="(error, index) in row.errors ?? []" :key="index">{{ error.label || error.field || "" }}：{{ error.message }}</li>
                </ul>
                <ul v-if="(row.warnings ?? []).length" class="row-warnings">
                  <li v-for="(warning, index) in row.warnings ?? []" :key="index">{{ warning }}</li>
                </ul>
              </td>
            </tr>
            <tr v-if="!dataRows.length"><td class="empty" colspan="6">没有数据行</td></tr>
          </tbody>
        </table>
      </div>
      <p v-if="dataRows.length > 200" class="hint">仅显示前 200 行，共 {{ dataRows.length }} 行。</p>
    </template>

    <template #foot>
      <template v-if="fatalError">
        <button type="button" @click="emit('close')">关闭</button>
      </template>
      <template v-else-if="preview">
        <button type="button" @click="emit('close')">取消</button>
        <button type="button" :disabled="loading" @click="rePreview">重新预览</button>
        <button type="button" class="primary" :disabled="submitting" @click="commit">确认导入</button>
      </template>
    </template>
  </ModalDialog>
</template>

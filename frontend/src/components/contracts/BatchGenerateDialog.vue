<script setup lang="ts">
// 批量生成对话框（阶段 1 迁移）：合同多选、模板版本选择、逐份预检
// （可生成 / 跳过原因 + 样例预览）、逐条生成与结果、失败重试、批量下载，
// 以及「保留内容变更前的旧签名重出」确认。DOM 结构与文案同存量 app.js
// openBatchGenerateDialog / runBatchPreflight / runBatchGeneration /
// renderBatchResults / retryBatchItem；预检、逐条审计与失败重试语义不变。
//
// 调度：合同台账岛经 bridge/app.ts 的 tengshou:open-batch-generate 事件
// 打开本组件（台账「批量生成」、模板页「批量生成」与「整改重出」共用）。
// 样例 PDF 复用 pdf 域的 PdfOverlay（本地 vendor pdf.js，无 CDN）。生成或
// 重试成功后经 refreshAfterWrite 广播 tengshou:data-changed，台账、模板页
// 与顶栏统计随之重取。
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { ApiError } from "../../api/client";
import {
  batchGenerateContracts,
  batchPreviewGeneration,
  downloadDocumentFile,
  getArchiveReuseSettings,
  listTemplates,
} from "../../api/endpoints";
import type {
  ApiFieldError,
  BatchGenerateItemResult,
  BatchGenerateResult,
  BatchPreviewResponse,
  Contract,
  DocumentKind,
  OverlayWarning,
  PreflightResult,
  Template,
} from "../../api/types";
import { errorMessage, refreshAfterWrite, toast } from "../../bridge/app";
import PdfOverlay from "../pdf/PdfOverlay.vue";
import { base64ToBytes } from "../pdf/pdf";
import { saveDownloadedFile } from "./download";

const props = defineProps<{
  title?: string;
  contracts: Contract[];
  templateId?: string;
  kind?: DocumentKind | "";
}>();

const emit = defineEmits<{ close: [] }>();

/** 预检状态 → 文案（app.js BATCH_STATUS_LABELS 逐字保留）。 */
const STATUS_LABELS: Record<string, string> = {
  ready: "可用",
  needs_signature: "缺签名",
  needs_resign: "需重新签名",
  missing_fields: "缺必填字段",
  missing_party: "缺相对方",
  missing_seal: "缺公章",
  blocked: "不可用",
  error: "预检失败",
};

const STALE_CONFIRM_TEXT =
  "确认保留内容变更前的旧签名重出？\n\n" +
  "仅当签名仍然有效、只是合同内容有改动时使用。每份合同都会在变更记录中单独记载此确认。";

const INITIAL_SUMMARY = `已选 ${props.contracts.length} 份合同。点击「预检 / 刷新状态」查看每份合同能否生成。`;

interface ChipState {
  text: string;
  cls: string;
}

interface ErrorState {
  fieldErrors: ApiFieldError[];
  message: string;
}

interface OverlayRequest {
  key: number;
  title: string;
  bytes: Uint8Array;
  warnings: OverlayWarning[];
}

const templates = ref<Template[]>([]);
const loaded = ref(false);
const templateID = ref("");
const selectedIds = ref<string[]>(props.contracts.map((contract) => contract.id));
const preview = ref<BatchPreviewResponse | null>(null);
const sampleBytes = ref<Uint8Array | null>(null);
const results = ref<BatchGenerateResult | null>(null);
const allowStale = ref(false);
const summaryText = ref(INITIAL_SUMMARY);
const errorState = ref<ErrorState | null>(null);
/* 状态标签保留上一次预检结果：迁移前勾选变化不清空 chip。 */
const chips = ref<Record<string, ChipState>>({});
const issues = ref<{ id: string; text: string }[]>([]);
const preflighting = ref(false);
const generating = ref(false);
const archiveNotify = ref(false); // 档案复用告知开关（Q6 修正）：预检提示随之显隐。
/* 一次生成尝试后按钮保持禁用（与迁移前的 finally 行为一致），
   重新预检或切换勾选 / 模板时恢复。 */
const generateLocked = ref(false);
const overlay = ref<OverlayRequest | null>(null);
let overlaySeq = 0;

const modalTitle = computed(
  () => props.title || `批量生成 PDF · ${props.contracts.length} 份合同`,
);
const selectedSet = computed(() => new Set(selectedIds.value));
/* 最近一次预检是否存在「需重新签名」项：决定保留旧签名开关的可见性。
   生成成功会清空 preview，但开关不能随之隐藏——失败项重试仍会按
   allowStale 实际发送 allow_stale_signature，可见性与生效状态必须一致
   （迁移前 app.js 也保留该开关可见）；仅在切换模板 / 勾选变化时复位。 */
const staleAvailable = ref(false);
const sampleDisabled = computed(() => sampleBytes.value === null);
const resultRows = computed(() => results.value?.items ?? []);

function templateByID(id: string): Template | undefined {
  return templates.value.find((template) => template.id === id);
}

function statusLabel(status: string): string {
  return STATUS_LABELS[status] ?? "不可用";
}

/** 可生成：已勾选且预检通过；或仅内容已变且用户确认保留旧签名。 */
function itemEligible(item: PreflightResult): boolean {
  if (!selectedSet.value.has(item.contract_id)) {
    return false;
  }
  if (item.ready) {
    return true;
  }
  if (!allowStale.value) {
    return false;
  }
  return (item.issues ?? []).every((issue) => issue.code === "content_changed");
}

const eligibleItems = computed<PreflightResult[]>(() => {
  if (!preview.value) {
    return [];
  }
  return (preview.value.items ?? []).filter(itemEligible);
});

const generateDisabled = computed(
  () => generating.value || generateLocked.value || eligibleItems.value.length === 0,
);

function chipFor(contractID: string): ChipState {
  return chips.value[contractID] ?? { text: "未预检", cls: "gen-chip" };
}

/** 默认模板：调用方指定且存在时优先，其次当前版本，最后第一个。 */
function defaultTemplateID(): string {
  if (props.templateId) {
    const pinned = templates.value.find((template) => template.id === props.templateId);
    if (pinned) {
      return pinned.id;
    }
  }
  const active = templates.value.find((template) => template.is_active);
  return active ? active.id : (templates.value[0]?.id ?? "");
}

async function loadTemplateOptions(): Promise<void> {
  try {
    const data = await listTemplates({ limit: 500, kind: "pdf" });
    templates.value = data.items ?? [];
  } catch (error) {
    toast(errorMessage(error), true);
    emit("close");
    return;
  }
  if (!templates.value.length) {
    toast("还没有 PDF 模板，请先在「模板」页导入", true);
    emit("close");
    return;
  }
  templateID.value = defaultTemplateID();
  loaded.value = true;
}

/** 预检完成后按可生成 / 需处理份数刷新摘要（app.js updateBatchGenerateState）。 */
function updateGenerateState(): void {
  const current = preview.value;
  if (!current) {
    return;
  }
  generateLocked.value = false;
  const eligible = eligibleItems.value.length;
  const pending = (current.items ?? []).filter(
    (item) => !itemEligible(item) && selectedSet.value.has(item.contract_id),
  ).length;
  summaryText.value =
    `可生成 ${eligible} 份 · 需处理 ${pending} 份（共 ${current.summary.total} 份）` +
    (allowStale.value ? " · 已确认保留旧签名" : "");
}

function onTemplateChange(): void {
  preview.value = null;
  sampleBytes.value = null;
  allowStale.value = false;
  staleAvailable.value = false;
  generateLocked.value = true;
  summaryText.value = "模板版本已更换，请重新预检。";
  issues.value = [];
  chips.value = {};
}

function onSelectionChange(): void {
  // 勾选变化后旧预检作废；状态标签保留上一次预检结果（与迁移前一致）。
  preview.value = null;
  allowStale.value = false;
  staleAvailable.value = false;
  generateLocked.value = true;
  summaryText.value = `已选 ${selectedIds.value.length} 份合同。勾选变化后请重新预检。`;
}

function toggleSelection(contractID: string, event: Event): void {
  const checked = (event.target as HTMLInputElement).checked;
  selectedIds.value = checked
    ? [...selectedIds.value, contractID]
    : selectedIds.value.filter((id) => id !== contractID);
  onSelectionChange();
}

function onAllowStaleChange(event: Event): void {
  const input = event.target as HTMLInputElement;
  if (input.checked && !window.confirm(STALE_CONFIRM_TEXT)) {
    input.checked = false;
    return;
  }
  allowStale.value = input.checked;
  updateGenerateState();
}

function describeError(error: unknown): ErrorState {
  return {
    fieldErrors: error instanceof ApiError ? error.fields : [],
    message: errorMessage(error),
  };
}

async function runPreflight(): Promise<void> {
  const ids = props.contracts
    .filter((contract) => selectedSet.value.has(contract.id))
    .map((contract) => contract.id);
  if (!ids.length) {
    toast("请至少勾选一份合同", true);
    return;
  }
  if (ids.length > 200) {
    toast("一次最多预检 200 份合同，请减少勾选", true);
    return;
  }
  preflighting.value = true;
  errorState.value = null;
  summaryText.value = "正在逐份预检…";
  try {
    const res = await batchPreviewGeneration(templateID.value, {
      contract_ids: ids,
      sample: true,
    });
    preview.value = res;
    sampleBytes.value = res.pdf_base64 ? base64ToBytes(res.pdf_base64) : null;
    staleAvailable.value = (res.items ?? []).some(
      (item) => item.status === "needs_resign",
    );
    const nextChips: Record<string, ChipState> = {};
    for (const item of res.items ?? []) {
      nextChips[item.contract_id] = {
        text: statusLabel(item.status),
        cls: `gen-chip ${item.ready ? "ok" : "warn"}`,
      };
    }
    chips.value = nextChips;
    issues.value = (res.items ?? [])
      .filter((item) => !item.ready)
      .map((item) => ({
        id: item.contract_id,
        text: `${item.contract_no}：${(item.issues ?? []).map((issue) => issue.message).join("；")}`,
      }));
    updateGenerateState();
    if (res.summary.blocked > 0) {
      toast(`${res.summary.blocked} 份合同预检未通过，原因已列出`, true);
    } else {
      toast("预检通过；可查看样例预览后开始批量生成");
    }
  } catch (error) {
    errorState.value = describeError(error);
  } finally {
    preflighting.value = false;
  }
}

function openSample(): void {
  const bytes = sampleBytes.value;
  if (!bytes) {
    return;
  }
  const template = templateByID(templateID.value);
  overlay.value = {
    key: (overlaySeq += 1),
    title: `样例预览 · ${template ? `${template.name} v${template.version}` : "模板"}`,
    bytes,
    warnings: preview.value?.warnings ?? [],
  };
}

async function refreshLists(): Promise<void> {
  try {
    await refreshAfterWrite();
  } catch {
    /* transient：生成结果已返回，刷新失败不覆盖结果 */
  }
}

async function runGenerate(): Promise<void> {
  if (!preview.value) {
    toast("请先预检，再开始批量生成", true);
    return;
  }
  const ready = eligibleItems.value;
  if (!ready.length) {
    toast("没有预检通过的合同", true);
    return;
  }
  const template = templateByID(templateID.value);
  const label = template ? `${template.name} v${template.version}` : "所选模板";
  const staleNote = allowStale.value
    ? "\n\n已勾选「保留内容变更前的旧签名重出」，该确认会逐份写入变更记录。"
    : "";
  if (!window.confirm(`将用「${label}」生成 ${ready.length} 份合同 PDF，继续吗？${staleNote}`)) {
    return;
  }
  generating.value = true;
  generateLocked.value = true;
  errorState.value = null;
  summaryText.value = `正在生成 ${ready.length} 份 PDF…`;
  try {
    const res = await batchGenerateContracts(templateID.value, {
      items: ready.map((item) => ({
        contract_id: item.contract_id,
        preview_sha256: item.preview_sha256,
      })),
      kind: props.kind || undefined,
      allow_stale_signature: allowStale.value,
    });
    results.value = res;
    preview.value = null;
    summaryText.value = `生成完成：成功 ${res.summary.success} 份 · 失败 ${res.summary.failed} 份`;
    await refreshLists();
  } catch (error) {
    errorState.value = describeError(error);
  } finally {
    generating.value = false;
  }
}

async function download(item: BatchGenerateItemResult): Promise<void> {
  const doc = item.document;
  if (!doc) {
    return;
  }
  try {
    const file = await downloadDocumentFile(doc.id);
    // 固定文件名 contract.pdf，与迁移前 downloadAsset 一致。
    saveDownloadedFile(file, "contract.pdf", true);
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

async function retryItem(contractID: string): Promise<void> {
  try {
    const previewRes = await batchPreviewGeneration(templateID.value, {
      contract_ids: [contractID],
      sample: false,
    });
    const item = (previewRes.items ?? [])[0];
    const eligible = Boolean(
      item &&
        (item.ready ||
          (allowStale.value &&
            (item.issues ?? []).every((issue) => issue.code === "content_changed"))),
    );
    if (!item || !eligible) {
      const reason = item
        ? (item.issues ?? []).map((issue) => issue.message).join("；")
        : "预检失败";
      toast(`仍不能生成：${reason}`, true);
      return;
    }
    const res = await batchGenerateContracts(templateID.value, {
      items: [{ contract_id: contractID, preview_sha256: item.preview_sha256 }],
      kind: props.kind || undefined,
      allow_stale_signature: allowStale.value,
    });
    const outcome = (res.items ?? [])[0];
    if (!outcome) {
      throw new Error("批次没有返回结果");
    }
    const current = results.value;
    if (current) {
      const items = (current.items ?? []).map((entry) =>
        entry.contract_id === contractID ? outcome : entry,
      );
      results.value = {
        ...current,
        items,
        summary: {
          total: items.length,
          success: items.filter((entry) => entry.success).length,
          failed: items.filter((entry) => !entry.success).length,
        },
      };
    }
    const summary = results.value?.summary;
    summaryText.value =
      `生成完成：成功 ${summary?.success ?? 0} 份 · 失败 ${summary?.failed ?? 0} 份`;
    toast(outcome.success ? "已重试成功" : `重试失败：${outcome.error_message || ""}`, !outcome.success);
    await refreshLists();
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

function onKeydown(event: KeyboardEvent): void {
  if (event.key === "Escape") {
    emit("close");
  }
}

onMounted(() => {
  document.addEventListener("keydown", onKeydown);
  void loadTemplateOptions();
  // 读取失败按「不告知」处理：预检照常进行，只是不展示来源提示。
  getArchiveReuseSettings()
    .then((settings) => (archiveNotify.value = settings.notify))
    .catch(() => (archiveNotify.value = false));
});

onBeforeUnmount(() => {
  document.removeEventListener("keydown", onKeydown);
});
</script>

<template>
  <Teleport to="body">
    <div v-if="loaded" class="modal-backdrop">
      <div class="modal wide" role="dialog" aria-modal="true">
        <div class="modal-head">
          <h2>{{ modalTitle }}</h2>
          <button type="button" class="close" aria-label="关闭" @click="emit('close')">×</button>
        </div>
        <div class="modal-body">
          <div id="batchErrors">
            <template v-if="errorState">
              <div v-if="errorState.fieldErrors.length" class="form-errors">
                请修正以下问题：
                <ul>
                  <li v-for="problem in errorState.fieldErrors" :key="problem.field">
                    {{ problem.label || problem.field }}：{{ problem.message }}
                  </li>
                </ul>
              </div>
              <div v-else class="form-errors">{{ errorState.message || "保存失败" }}</div>
            </template>
          </div>
          <div class="gen-row">
            <div class="field">
              <label for="batchTemplate">模板版本</label>
              <select id="batchTemplate" v-model="templateID" @change="onTemplateChange">
                <option v-for="template in templates" :key="template.id" :value="template.id">
                  {{ template.name }} v{{ template.version }}{{ template.is_active ? "（当前版本）" : "" }}
                </option>
              </select>
            </div>
            <div class="gen-actions">
              <button
                type="button"
                id="batchPreflight"
                :disabled="preflighting"
                @click="runPreflight"
              >
                预检 / 刷新状态
              </button>
              <button type="button" id="batchSample" :disabled="sampleDisabled" @click="openSample">
                查看样例预览
              </button>
            </div>
          </div>
          <div class="hint">
            勾选要生成的合同。预检会逐份检查必填字段、相对方档案、签名（现场手写核对内容版本，档案复用直接套印）与公章，再开始生成。
          </div>
          <div v-if="archiveNotify" id="batchArchiveReuseNotice" class="hint">
            档案复用告知：已复用档案签名/公章的合同会直接套印（来源：档案）；未复用档案资产的合同不受影响。如对方不同意，请先到合同表单取消复用，改为现场扫码手写。
          </div>
          <div id="batchContractList" class="batch-contract-list">
            <label v-for="contract in contracts" :key="contract.id" class="batch-contract-row">
              <input
                type="checkbox"
                :data-batch-select="contract.id"
                :checked="selectedSet.has(contract.id)"
                @change="toggleSelection(contract.id, $event)"
              >
              <span class="mono">{{ contract.contract_no }}</span>
              <span class="grow" :title="contract.title">{{ contract.title }}</span>
              <span :class="chipFor(contract.id).cls" :data-contract-status="contract.id">
                {{ chipFor(contract.id).text }}
              </span>
            </label>
          </div>
          <div id="batchSummary" class="tpl-status">{{ summaryText }}</div>
          <label v-show="staleAvailable" id="batchStaleWrap" class="check-row">
            <input
              id="batchAllowStale"
              type="checkbox"
              :checked="allowStale"
              @change="onAllowStaleChange"
            >
            保留内容变更前的旧签名重出（仅对签名未失效、内容已变化的合同生效，逐份写入变更记录）
          </label>
          <div id="batchIssues" class="tpl-warnings">
            <div v-for="issue in issues" :key="issue.id" class="tpl-warning">{{ issue.text }}</div>
          </div>
          <div id="batchResults">
            <template v-if="results">
              <div class="table-wrap">
                <table class="gen-history-table">
                  <thead>
                    <tr>
                      <th>合同编号</th>
                      <th>名称</th>
                      <th>状态</th>
                      <th>结果</th>
                      <th>操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="item in resultRows" :key="item.contract_id">
                      <td class="mono">{{ item.contract_no || "" }}</td>
                      <td class="wrap">{{ item.title || "" }}</td>
                      <td>
                        <span class="gen-badge" :class="item.success ? 'success' : 'failed'">
                          {{ item.success ? "成功" : "失败" }}
                        </span>
                      </td>
                      <td class="wrap">
                        <button
                          v-if="item.success && item.document"
                          type="button"
                          :data-batch-download="item.document?.id"
                          @click="download(item)"
                        >
                          下载 PDF
                        </button>
                        <span v-else class="row-errors">{{ item.error_message || "生成失败" }}</span>
                      </td>
                      <td>
                        <div class="row-actions">
                          <button
                            v-if="!item.success"
                            type="button"
                            :data-batch-retry="item.contract_id"
                            @click="retryItem(item.contract_id)"
                          >
                            重试
                          </button>
                        </div>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
              <div class="tpl-status">
                批次号 <code>{{ results.batch_id }}</code>，可在合同详情的生成历史中按批次追溯。
              </div>
            </template>
          </div>
        </div>
        <div class="modal-foot">
          <button type="button" @click="emit('close')">关闭</button>
          <button type="button" class="primary" :disabled="generateDisabled" @click="runGenerate">
            开始批量生成
          </button>
        </div>
      </div>
    </div>
    <PdfOverlay
      v-if="overlay"
      :key="overlay.key"
      :title="overlay.title"
      :bytes="overlay.bytes"
      :warnings="overlay.warnings"
      @close="overlay = null"
    />
  </Teleport>
</template>

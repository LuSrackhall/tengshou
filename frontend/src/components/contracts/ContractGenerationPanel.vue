<script setup lang="ts">
// 合同详情「生成 PDF」面板与生成历史（阶段 1 迁移）：模板版本选择、预览
// （全屏 PDF overlay）、生成、内容变更后的「保留旧签名重出」确认、失败重试、
// 生成历史与下载、渲染告警（越界 / 重叠）。DOM 结构与文案同存量
// app.js renderContractGeneration / renderGenerationHistory；预检、生成、
// 落库与审计语义不变。
//
// 跨域：扫码签名与批量生成仍归 app.js。生成成功后经 bridge/app.ts 的
// refreshAfterWrite 刷新 app.js 的模板/统计缓存并广播
// tengshou:data-changed；外部写入经同一事件到达本域后重取模板与生成历史。
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { ApiError } from "../../api/client";
import {
  downloadDocumentFile,
  generateContract,
  getContract,
  listTemplates,
  previewTemplate,
} from "../../api/endpoints";
import type {
  ApiFieldError,
  Contract,
  ContractDetail,
  OutputDocument,
  OverlayWarning,
  Template,
} from "../../api/types";
import {
  errorMessage,
  refreshAfterWrite,
  subscribeDataChanged,
  toast,
  type DataChangedDetail,
} from "../../bridge/app";
import PdfOverlay from "../pdf/PdfOverlay.vue";
import { base64ToBytes } from "../pdf/pdf";
import { saveDownloadedFile } from "./download";
import {
  fmtDate,
  generationKindLabel,
  generationStatusLabel,
  shortDigest,
} from "./format";

const props = defineProps<{
  contract: Contract;
  detail: ContractDetail;
}>();

/** 首次模板加载完成（成功或失败）后通知父组件，便于行内「生成 PDF」定位。 */
const emit = defineEmits<{ ready: [] }>();

const INITIAL_STATUS = "选择模板版本后，先「预览」确认版式与数据，再生成 PDF。";
const NO_TEMPLATE_TEXT = "还没有 PDF 模板。请先在「模板」页导入 Word 模板并完成位置标定。";
const EMPTY_HISTORY_TEXT =
  "还没有生成记录。预览后生成 PDF，这里会记录谁、何时、用哪个模板版本生成及使用的签名。";
const CHANGE_TEMPLATE_STATUS = "模板版本已更换，请重新预览。";

/** 生成失败时的下一步提示（app.js generationErrorHTML 的 hints）。 */
const GENERATION_HINTS: Record<string, string> = {
  missing_signature: "请先在合同行点「扫码签名」采集签名，再预览并生成。",
  content_changed:
    "合同内容在签名后已修改；请重新签名，或确认旧签名仍有效时选择「保留旧签名重出」。",
  missing_field: "请先补全合同或相对方档案中的必填信息。",
  missing_party: "请先为合同补齐相对方档案。",
  missing_seal: "请先在「公章」页导入或关联公章图片。",
  preview_required: "请先重新预览，再生成。",
};

const STALE_CONFIRM_TEXT =
  "合同内容在签名采集后已经修改。\n\n" +
  "重新让签名人扫码更稳妥。如果确认旧签名仍然有效，可以「保留旧签名重出」，此确认会写入变更记录。\n\n" +
  "是否保留旧签名重出？";

/** 预览态：记录生成必须回传的 preview_sha256 与所用模板版本。 */
interface PreviewState {
  templateID: string;
  sha: string;
}

interface GenerationErrorState {
  fieldErrors: ApiFieldError[];
  message: string;
  hint: string;
}

interface OverlayRequest {
  key: number;
  title: string;
  bytes: Uint8Array;
  warnings: OverlayWarning[];
}

const templates = ref<Template[]>([]);
const loaded = ref(false);
const selectedID = ref("");
const preview = ref<PreviewState | null>(null);
const status = ref(INITIAL_STATUS);
const warnings = ref<OverlayWarning[]>([]);
const generationErrors = ref<GenerationErrorState | null>(null);
const documents = ref<OutputDocument[]>(props.detail.documents ?? []);
const overlay = ref<OverlayRequest | null>(null);
const previewing = ref(false);
const generating = ref(false);

let overlaySeq = 0;
let historySeq = 0;
let unsubscribeData: (() => void) | null = null;

const generateDisabled = computed(() => preview.value === null || generating.value);

function templateByID(id: string): Template | undefined {
  return templates.value.find((template) => template.id === id);
}

/** 默认模板：合同钉住的版本优先，其次当前版本，最后第一个（app.js 同名逻辑）。 */
function defaultTemplateID(contract: Contract, list: Template[]): string {
  const pinned = contract.template_id
    ? list.find((template) => template.id === contract.template_id)
    : undefined;
  if (pinned) {
    return pinned.id;
  }
  const active = list.find((template) => template.is_active);
  return active ? active.id : list[0]?.id ?? "";
}

function templateLabel(doc: OutputDocument): string {
  const template = doc.template_id ? templateByID(doc.template_id) : undefined;
  return template
    ? `${template.name} v${doc.template_version}`
    : `v${doc.template_version || "?"}`;
}

async function loadTemplates(): Promise<void> {
  try {
    const data = await listTemplates({ limit: 500, kind: "pdf" });
    templates.value = data.items ?? [];
    if (!templates.value.some((template) => template.id === selectedID.value)) {
      selectedID.value = defaultTemplateID(props.contract, templates.value);
      preview.value = null;
      // 首次加载保持初始文案；外部删改模板导致选择失效时才提示重新预览。
      if (loaded.value) {
        status.value = CHANGE_TEMPLATE_STATUS;
        warnings.value = [];
      }
    }
    loaded.value = true;
  } catch (error) {
    if (!loaded.value) {
      loaded.value = true;
      toast(errorMessage(error), true);
    }
    /* transient（外部刷新时）：保留上一次的模板列表 */
  }
}

async function refreshHistory(): Promise<void> {
  const seq = (historySeq += 1);
  try {
    const detail = await getContract(props.contract.id);
    if (seq !== historySeq) {
      return;
    }
    documents.value = detail.documents ?? [];
  } catch {
    /* transient: keep the previous history */
  }
}

async function refreshLists(): Promise<void> {
  try {
    await refreshAfterWrite();
  } catch {
    /* transient: keep the previous lists */
  }
  await loadTemplates();
}

function describeGenerationError(error: unknown): GenerationErrorState {
  const fieldErrors = error instanceof ApiError ? error.fields : [];
  const code = error instanceof ApiError ? error.code : "";
  return {
    fieldErrors,
    message: errorMessage(error),
    hint: GENERATION_HINTS[code] ?? "",
  };
}

function onTemplateChange(): void {
  preview.value = null;
  warnings.value = [];
  status.value = CHANGE_TEMPLATE_STATUS;
}

async function runPreview(): Promise<void> {
  const templateID = selectedID.value;
  if (!templateID) {
    toast("请选择模板版本", true);
    return;
  }
  previewing.value = true;
  generationErrors.value = null;
  status.value = "正在生成预览…";
  try {
    const result = await previewTemplate(templateID, {
      contract_id: props.contract.id,
    });
    const resultWarnings = result.warnings ?? [];
    preview.value = { templateID, sha: result.preview_sha256 };
    status.value = "预览已完成。确认版式与数据无误后点击「生成 PDF」。";
    warnings.value = resultWarnings;
    const template = templateByID(templateID);
    overlay.value = {
      key: (overlaySeq += 1),
      title: `合同预览 · ${props.contract.contract_no}${
        template ? ` · ${template.name} v${template.version}` : ""
      }`,
      bytes: base64ToBytes(result.pdf_base64),
      warnings: resultWarnings,
    };
  } catch (error) {
    preview.value = null;
    status.value = "预览失败。";
    generationErrors.value = describeGenerationError(error);
  } finally {
    previewing.value = false;
  }
}

async function runGenerate(allowStaleSignature = false): Promise<void> {
  const current = preview.value;
  if (!current) {
    toast("请先点击「预览」，确认版式与数据后再生成", true);
    return;
  }
  if (selectedID.value !== current.templateID) {
    preview.value = null;
    toast("模板版本已更换，请重新预览", true);
    return;
  }
  generating.value = true;
  generationErrors.value = null;
  status.value = "正在生成 PDF…";
  try {
    const result = await generateContract(current.templateID, {
      contract_id: props.contract.id,
      preview_sha256: current.sha,
      // 单份生成不指定 kind，沿用存量界面的空串（由服务端判断初次/重出）。
      kind: "",
      allow_stale_signature: allowStaleSignature,
    });
    const doc = result.item;
    warnings.value = result.warnings ?? [];
    status.value = `已${generationKindLabel(doc.kind)} PDF：${doc.page_count} 页，模板 v${doc.template_version}。`;
    preview.value = null;
    toast(doc.kind === "regenerate" ? "已整改重出 PDF" : "已生成 PDF");
    await refreshHistory();
    await refreshLists();
  } catch (error) {
    if (
      error instanceof ApiError &&
      error.code === "content_changed" &&
      !allowStaleSignature
    ) {
      const confirmed = window.confirm(STALE_CONFIRM_TEXT);
      if (confirmed) {
        generating.value = false;
        await runGenerate(true);
        return;
      }
    }
    status.value = "生成失败。";
    generationErrors.value = describeGenerationError(error);
    await refreshHistory();
  } finally {
    generating.value = false;
  }
}

async function retry(doc: OutputDocument): Promise<void> {
  if (doc.template_id && !templateByID(doc.template_id)) {
    toast("失败记录使用的模板版本已不存在，请选择其他版本并重新预览", true);
    return;
  }
  if (doc.template_id) {
    selectedID.value = doc.template_id;
  }
  preview.value = null;
  await runPreview();
  if (preview.value) {
    toast("已重新预览；确认后点击「生成 PDF」重试");
  }
}

async function download(doc: OutputDocument): Promise<void> {
  try {
    const file = await downloadDocumentFile(doc.id);
    saveDownloadedFile(file, "contract.pdf");
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

/** 外部写入（SSE / 轮询）到达后：模板列表与生成历史自动刷新。 */
function onDataChanged(changed: DataChangedDetail): void {
  const entities = changed.entities;
  if (entities.length === 0 || entities.includes("template")) {
    void loadTemplates();
  }
  if (entities.length === 0 || entities.includes("contract")) {
    void refreshHistory();
  }
}

onMounted(() => {
  void loadTemplates();
  unsubscribeData = subscribeDataChanged(onDataChanged);
});

// 首次模板列表渲染完成后再通知父组件：DOM 已更新，行内「生成 PDF」
// 才能滚到稳定的最终位置（模板未加载时面板高度会变化）。
watch(
  loaded,
  (value) => {
    if (value) {
      emit("ready");
    }
  },
  { flush: "post" },
);

onBeforeUnmount(() => {
  unsubscribeData?.();
  unsubscribeData = null;
  // 让在途的历史请求作废。
  historySeq += 1;
});
</script>

<template>
  <div id="contractGeneration" class="gen-panel">
    <template v-if="loaded">
      <template v-if="!templates.length">
        <h3>合同 PDF 生成</h3>
        <div class="tpl-status">{{ NO_TEMPLATE_TEXT }}</div>
      </template>
      <template v-else>
        <h3>合同 PDF 生成</h3>
        <div class="gen-row">
          <div class="field">
            <label for="genTemplate">模板版本</label>
            <select id="genTemplate" v-model="selectedID" @change="onTemplateChange">
              <option v-for="template in templates" :key="template.id" :value="template.id">
                {{ template.name }} v{{ template.version }}{{ template.is_active ? "（当前版本）" : "" }}
              </option>
            </select>
          </div>
          <div class="gen-actions">
            <button type="button" id="genPreview" :disabled="previewing" @click="runPreview">预览</button>
            <button
              type="button"
              id="genGenerate"
              class="primary"
              :disabled="generateDisabled"
              @click="runGenerate(false)"
            >
              生成 PDF
            </button>
          </div>
        </div>
        <div id="genStatus" class="tpl-status">{{ status }}</div>
        <div id="genErrors">
          <template v-if="generationErrors">
            <div v-if="generationErrors.fieldErrors.length" class="form-errors">
              请修正以下问题：
              <ul>
                <li v-for="problem in generationErrors.fieldErrors" :key="problem.field">
                  {{ problem.label || problem.field }}：{{ problem.message }}
                </li>
              </ul>
            </div>
            <div v-else class="form-errors">{{ generationErrors.message || "保存失败" }}</div>
            <div v-if="generationErrors.hint" class="warning-list">{{ generationErrors.hint }}</div>
          </template>
        </div>
        <div id="genWarnings" class="tpl-warnings">
          <div
            v-for="(warning, index) in warnings"
            :key="`${warning.code}-${index}`"
            class="tpl-warning"
          >
            {{ warning.message || warning.code }}
          </div>
        </div>
        <h3>生成历史</h3>
        <div id="genHistory">
          <div v-if="!documents.length" class="tpl-status">{{ EMPTY_HISTORY_TEXT }}</div>
          <div v-else class="table-wrap">
            <table class="gen-history-table">
              <thead>
                <tr>
                  <th>时间</th>
                  <th>操作人</th>
                  <th>模板版本</th>
                  <th>内容版本</th>
                  <th>类型</th>
                  <th>状态</th>
                  <th>文件 / 失败原因</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="doc in documents" :key="doc.id">
                  <td>{{ fmtDate(doc.created_at) }}</td>
                  <td>{{ doc.operator || "—" }}</td>
                  <td>{{ templateLabel(doc) }}</td>
                  <td><code>{{ shortDigest(doc.content_sha256) }}</code></td>
                  <td>{{ generationKindLabel(doc.kind) }}</td>
                  <td>
                    <span class="gen-badge" :class="doc.status === 'failed' ? 'failed' : 'success'">
                      {{ generationStatusLabel(doc.status) }}
                    </span>
                  </td>
                  <td class="wrap">
                    <span v-if="doc.status === 'failed'" class="row-errors">
                      {{ doc.error_message || "生成失败" }}
                    </span>
                    <button v-else type="button" @click="download(doc)">下载 PDF</button>
                  </td>
                  <td>
                    <div class="row-actions">
                      <button v-if="doc.status === 'failed'" type="button" @click="retry(doc)">
                        重试
                      </button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </template>
    </template>
    <PdfOverlay
      v-if="overlay"
      :key="overlay.key"
      :title="overlay.title"
      :bytes="overlay.bytes"
      :warnings="overlay.warnings"
      @close="overlay = null"
    />
  </div>
</template>

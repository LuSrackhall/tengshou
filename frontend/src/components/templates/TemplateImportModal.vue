<script setup lang="ts">
// 导入模板弹窗：字段、转换程序可用性与错误提示逐条对应存量 app.js
// openTemplateImport。文件读取（≤40MB 校验 + base64）由 TemplatesView 完成。
//
// v0.4 起导入走后台任务（job 化）：弹窗只负责收集名称与展示真实阶段进度
// （saving → converting → parsing），启动与轮询由 TemplatesView 持有，因此
// 等待期间关闭弹窗后任务继续，完成后模板自动出现在列表。
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import type {
  ConverterAvailability,
  JobSnapshot,
  TemplateMutationResponse,
} from "../../api/types";
import ProgressPanel from "../common/ProgressPanel.vue";
import type { ImportErrorState } from "./import";

const props = defineProps<{
  file: File;
  converters: ConverterAvailability[];
  /** 当前导入任务快照（未开始为 null）。 */
  job: JobSnapshot<TemplateMutationResponse> | null;
  /** 任务失败后的错误（逐字段优先，其次整条文案）。 */
  errors: ImportErrorState | null;
}>();

const emit = defineEmits<{
  close: [];
  submit: [name: string];
}>();

const name = ref(props.file.name.replace(/\.[^.]+$/, ""));
const running = computed(() => props.job?.status === "running");

const ext = (props.file.name.match(/\.[^.]+$/) ?? [""])[0].toLowerCase();
const isPDF = ext === ".pdf";
const sizeMB = (props.file.size / 1024 / 1024).toFixed(2);

const anyConverter = computed(() =>
  props.converters.some((converter) => converter.available),
);
const showWarning = computed(() => !isPDF && !anyConverter.value);

function converterText(converter: ConverterAvailability): string {
  const detail = converter.detail ?? "";
  return converter.available
    ? `${converter.label}：可用（${detail}）`
    : `${converter.label}：不可用（${detail}）`;
}

function close(): void {
  // 等待期间也允许关闭：任务在后台继续，TemplatesView 会提示来源。
  emit("close");
}

function onKeydown(event: KeyboardEvent): void {
  if (event.key === "Escape") {
    close();
  }
}

function submit(): void {
  if (running.value) {
    return;
  }
  emit("submit", name.value.trim());
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
      <div class="modal wide" role="dialog" aria-modal="true">
        <div class="modal-head">
          <h2>导入模板 · {{ file.name }}</h2>
          <button type="button" class="close" aria-label="关闭" @click="close">×</button>
        </div>
        <div class="modal-body">
          <div id="templateImportErrors">
            <div v-if="errors && errors.fieldErrors.length" class="form-errors">
              请修正以下问题：<ul>
                <li v-for="problem in errors.fieldErrors" :key="problem.field">
                  {{ problem.label || problem.field }}：{{ problem.message }}
                </li>
              </ul>
            </div>
            <div v-else-if="errors && errors.message" class="form-errors">{{ errors.message }}</div>
          </div>
          <ProgressPanel v-if="job && job.status === 'running'" :job="job" />
          <div v-if="showWarning && !running" class="warning-list">
            本机未检测到 Microsoft Word 或 LibreOffice。Word 转换会失败；
            可以安装 LibreOffice（免费），或先用 Word / WPS 将文档另存为 PDF，再导入 PDF。
          </div>
          <div class="form-grid">
            <div class="field">
              <label for="tplImportName">模板名称<span class="req">*</span></label>
              <input
                id="tplImportName"
                v-model="name"
                type="text"
                placeholder="例如：房屋租赁合同（村委会版）"
                :disabled="running"
              >
              <span class="help">同名再导入会增加新版本；旧版本和已生成 PDF 都会保留。</span>
            </div>
            <div class="field">
              <label>文件</label>
              <div class="mono">{{ file.name }} · {{ sizeMB }} MB</div>
              <span class="help">
                {{ isPDF ? "直接作为模板 PDF 导入，不经过转换。" : "将转换为 PDF 后用于所见即所得的标定。" }}
              </span>
            </div>
            <div class="field full">
              <label>转换程序</label>
              <ul class="import-notes">
                <li v-for="converter in converters" :key="converter.name">
                  {{ converterText(converter) }}
                </li>
                <li v-if="!converters.length">未检测到转换程序</li>
              </ul>
              <span v-if="!isPDF" class="help">
                优先使用本机 Microsoft Word（Windows，保真度最高）；否则用 LibreOffice，可能出现细微排版差异。
              </span>
            </div>
          </div>
          <p v-if="running" class="hint">
            等待期间可以关闭弹窗：转换在后台继续，完成后模板会自动出现在列表。
          </p>
        </div>
        <div class="modal-foot">
          <button type="button" @click="close">{{ running ? "转到后台" : "取消" }}</button>
          <button type="button" class="primary" :disabled="running" @click="submit">
            {{ running ? "导入中…" : "导入" }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

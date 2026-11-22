<script setup lang="ts">
// 导入模板弹窗：字段、转换程序可用性与错误提示逐条对应存量 app.js
// openTemplateImport。文件读取（≤40MB 校验 + base64）由 TemplatesView 完成。
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { ApiError } from "../../api/client";
import { importTemplate } from "../../api/endpoints";
import type { ApiFieldError, ConverterAvailability, Template } from "../../api/types";
import { errorMessage, toast } from "../../bridge/app";

const props = defineProps<{
  file: File;
  /** 文件内容的 base64（不含 data: 前缀）。 */
  base64: string;
  converters: ConverterAvailability[];
}>();

const emit = defineEmits<{
  close: [];
  imported: [template: Template];
}>();

const name = ref(props.file.name.replace(/\.[^.]+$/, ""));
const saving = ref(false);
const fieldErrors = ref<ApiFieldError[]>([]);
const errorText = ref("");

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
  if (saving.value) {
    return;
  }
  emit("close");
}

function onKeydown(event: KeyboardEvent): void {
  if (event.key === "Escape") {
    close();
  }
}

async function submit(): Promise<void> {
  if (saving.value) {
    return;
  }
  saving.value = true;
  fieldErrors.value = [];
  errorText.value = "";
  try {
    const result = await importTemplate({
      name: name.value.trim(),
      file_name: props.file.name,
      file_base64: props.base64,
    });
    const warnings = result.warnings ?? [];
    toast(warnings.length ? warnings[0] ?? "" : `模板已导入（v${result.item.version}）`);
    if (warnings.length > 1) {
      setTimeout(() => toast(warnings.slice(1).join("；")), 400);
    }
    emit("imported", result.item);
  } catch (error) {
    saving.value = false;
    if (error instanceof ApiError && error.fields.length) {
      fieldErrors.value = error.fields;
    } else {
      errorText.value = errorMessage(error);
    }
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
      <div class="modal wide" role="dialog" aria-modal="true">
        <div class="modal-head">
          <h2>导入模板 · {{ file.name }}</h2>
          <button type="button" class="close" aria-label="关闭" @click="close">×</button>
        </div>
        <div class="modal-body">
          <div id="templateImportErrors">
            <div v-if="fieldErrors.length" class="form-errors">
              请修正以下问题：<ul>
                <li v-for="problem in fieldErrors" :key="problem.field">
                  {{ problem.label || problem.field }}：{{ problem.message }}
                </li>
              </ul>
            </div>
            <div v-else-if="errorText" class="form-errors">{{ errorText }}</div>
          </div>
          <div v-if="showWarning" class="warning-list">
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
        </div>
        <div class="modal-foot">
          <button type="button" @click="close">取消</button>
          <button type="button" class="primary" :disabled="saving" @click="submit">导入</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

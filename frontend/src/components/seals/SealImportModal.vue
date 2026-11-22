<script setup lang="ts">
// 导入公章图片弹窗：字段与交互逐条对应存量 app.js openSealImport。
// 文件大小校验与 base64 读取在 SealsView 里完成，打开本弹窗时图片已可预览。
import { nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { ApiError } from "../../api/client";
import { importSeal } from "../../api/endpoints";
import type { ApiFieldError, Party } from "../../api/types";
import { errorMessage, refreshAfterWrite, toast } from "../../bridge/app";
import { kindLabel } from "./format";

const props = defineProps<{
  file: File;
  /** 图片内容的 base64（不含 data: 前缀）。 */
  base64: string;
  parties: Party[];
}>();

const emit = defineEmits<{
  close: [];
}>();

const name = ref(props.file.name.replace(/\.[^.]+$/, ""));
const partyId = ref("");
const note = ref("");

const saving = ref(false);
const fieldErrors = ref<ApiFieldError[]>([]);
const errorText = ref("");
const bodyEl = ref<HTMLDivElement | null>(null);

const previewURL = `data:${props.file.type || "image/png"};base64,${props.base64}`;

function onKeydown(event: KeyboardEvent): void {
  if (event.key === "Escape") {
    emit("close");
  }
}

function showError(error: unknown): void {
  fieldErrors.value = [];
  errorText.value = "";
  if (error instanceof ApiError && error.fields.length) {
    fieldErrors.value = error.fields;
  } else {
    errorText.value = errorMessage(error);
  }
  void nextTick(() => {
    if (bodyEl.value) {
      bodyEl.value.scrollTop = 0;
    }
  });
}

async function submit(): Promise<void> {
  if (saving.value) {
    return;
  }
  saving.value = true;
  fieldErrors.value = [];
  errorText.value = "";
  try {
    await importSeal({
      name: name.value.trim(),
      party_id: partyId.value,
      note: note.value.trim(),
      file_base64: props.base64,
    });
    emit("close");
    toast("公章图片已导入");
    await refreshAfterWrite();
  } catch (error) {
    saving.value = false;
    showError(error);
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
      <div class="modal" role="dialog" aria-modal="true">
        <div class="modal-head">
          <h2>导入公章图片 · {{ file.name }}</h2>
          <button type="button" class="close" aria-label="关闭" @click="emit('close')">×</button>
        </div>
        <div ref="bodyEl" class="modal-body">
          <div id="sealImportErrors">
            <div v-if="fieldErrors.length" class="form-errors">
              请修正以下问题：<ul>
                <li v-for="problem in fieldErrors" :key="problem.field">
                  {{ problem.label || problem.field }}：{{ problem.message }}
                </li>
              </ul>
            </div>
            <div v-else-if="errorText" class="form-errors">{{ errorText }}</div>
          </div>
          <div class="form-grid">
            <div class="field">
              <label for="sealName">公章名称<span class="req">*</span></label>
              <input
                id="sealName"
                v-model="name"
                type="text"
                placeholder="例如：幸福村股份经济合作社公章"
              >
            </div>
            <div class="field">
              <label for="sealParty">所属单位（可选）</label>
              <select id="sealParty" v-model="partyId">
                <option value="">（暂不关联）</option>
                <option v-for="party in parties" :key="party.id" :value="party.id">
                  {{ party.name }}（{{ kindLabel(party.kind) }}）
                </option>
              </select>
              <span class="help">关联单位后，该单位的合同可按模板自动取用这枚公章。</span>
            </div>
            <div class="field full">
              <label for="sealNote">说明</label>
              <input id="sealNote" v-model="note" type="text" placeholder="可选，例如印章类型或保管人">
            </div>
            <div class="field full">
              <label>图片预览</label>
              <div class="seal-import-preview">
                <img :src="previewURL" alt="公章预览">
              </div>
              <span class="help">支持 PNG / JPG。PNG 保留透明底；JPG 或白底图片会自动去除白底并裁剪空白边距。</span>
            </div>
          </div>
        </div>
        <div class="modal-foot">
          <button type="button" @click="emit('close')">取消</button>
          <button type="button" class="primary" :disabled="saving" @click="submit">导入</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

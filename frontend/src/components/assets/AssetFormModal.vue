<script setup lang="ts">
// 房产新建/编辑弹窗：固定字段表单（v0.2 只做房产，不接自定义字段引擎）。
// 名称必填；面积选填，留空即清除。在租/空置由合同派生，不在表单里维护；
// 停用/启用是列表行上的独立动作。
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from "vue";
import { ApiError } from "../../api/client";
import { createAsset, updateAsset } from "../../api/endpoints";
import type { ApiFieldError, Asset, AssetInput } from "../../api/types";
import { errorMessage, refreshAfterWrite, toast } from "../../bridge/app";
import { assetControlValues, collectAssetInput } from "./form";
import { formTitle } from "./format";

const props = defineProps<{ asset: Asset | null }>();

const emit = defineEmits<{ close: [] }>();

const values = reactive(assetControlValues(props.asset));
const saving = ref(false);
const fieldErrors = ref<ApiFieldError[]>([]);
const errorText = ref("");
const bodyEl = ref<HTMLDivElement | null>(null);

const title = computed(() => formTitle(props.asset));

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
    const input: AssetInput = collectAssetInput(values);
    const result = props.asset
      ? await updateAsset(props.asset.id, input)
      : await createAsset(input);
    emit("close");
    toast("房产档案已保存");
    const warnings = result.warnings ?? [];
    if (warnings.length) {
      window.setTimeout(() => toast(warnings.join("；")), 350);
    }
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
          <h2>{{ title }}</h2>
          <button type="button" class="close" aria-label="关闭" @click="emit('close')">×</button>
        </div>
        <div ref="bodyEl" class="modal-body">
          <div id="assetErrors">
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
              <label for="f_asset_name">名称<span class="req">*</span></label>
              <input
                id="f_asset_name"
                v-model="values.name"
                type="text"
                placeholder="如：幸福里 3 号院 2 单元 501"
                autocomplete="off"
              >
            </div>
            <div class="field">
              <label for="f_asset_room_no">房号</label>
              <input id="f_asset_room_no" v-model="values.room_no" type="text" autocomplete="off">
            </div>
            <div class="field full">
              <label for="f_asset_address">地址</label>
              <input
                id="f_asset_address"
                v-model="values.address"
                type="text"
                placeholder="如：幸福路 3 号"
                autocomplete="off"
              >
            </div>
            <div class="field">
              <label for="f_asset_area">面积（㎡）</label>
              <input
                id="f_asset_area"
                v-model="values.area"
                type="number"
                step="any"
                min="0"
                placeholder="选填"
              >
            </div>
            <div class="field full">
              <label for="f_asset_note">备注</label>
              <textarea id="f_asset_note" v-model="values.note"></textarea>
            </div>
          </div>
          <p class="hint">
            在租 / 空置由「生效中且未终止的合同」自动派生，不需要手工维护；
            停用请使用列表行上的「停用」按钮。
          </p>
        </div>
        <div class="modal-foot">
          <button type="button" @click="emit('close')">取消</button>
          <button type="button" class="primary" :disabled="saving" @click="submit">保存</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

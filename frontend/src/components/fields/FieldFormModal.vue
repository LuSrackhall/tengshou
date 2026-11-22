<script setup lang="ts">
// 字段新建/编辑弹窗：字段与交互逐条对应存量 app.js openFieldForm。
//   - 字段键创建后不可修改（编辑时禁用）；
//   - 内置字段类型不可修改（类型下拉禁用），但名称/必填/默认值等仍可编辑；
//   - 选项仅单选字段展示；新建时台账/模板默认勾选；
//   - 保存后走 app.js 的整体刷新，使未迁移域的 state.fields 同步更新。
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { ApiError } from "../../api/client";
import { createField, updateField } from "../../api/endpoints";
import type { ApiFieldError, FieldDefinition, FieldEntity, FieldInput, FieldType } from "../../api/types";
import { errorMessage, refreshAfterWrite, toast } from "../../bridge/app";
import { FIELD_TYPE_LABELS, USER_FIELD_TYPES } from "./labels";

const props = defineProps<{
  /** 编辑目标；null 表示新建。 */
  field: FieldDefinition | null;
  /** 新建时归属的实体（编辑时以字段自身的 entity 为准）。 */
  entity: FieldEntity;
}>();

const emit = defineEmits<{
  close: [];
}>();

const isEdit = props.field !== null;
const isBuiltIn = props.field?.built_in ?? false;

const label = ref(props.field?.label ?? "");
const key = ref(props.field?.key ?? "");
const type = ref<FieldType>(props.field?.type ?? "text");
const sort = ref(props.field ? String(props.field.sort_order) : "");
const defaultValue = ref(props.field?.default_value ?? "");
const optionsText = ref((props.field?.options ?? []).join("\n"));
const note = ref(props.field?.note ?? "");
const required = ref(props.field?.required ?? false);
const inLedger = ref(props.field ? props.field.in_ledger : true);
const inTemplate = ref(props.field ? props.field.in_template : true);

const saving = ref(false);
const fieldErrors = ref<ApiFieldError[]>([]);
const errorText = ref("");
const bodyEl = ref<HTMLDivElement | null>(null);

const title = computed(() =>
  props.field ? `编辑字段 · ${props.field.label}` : "新建字段",
);

// app.js：可选类型排除 party/status；若当前字段正是这两类内置类型，
// 额外把它作为选中项追加到列表末尾。
const typeOptions = computed<FieldType[]>(() => {
  const current = props.field?.type;
  if (current === "party" || current === "status") {
    return [...USER_FIELD_TYPES, current];
  }
  return USER_FIELD_TYPES;
});

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

function buildInput(): FieldInput {
  const input: FieldInput = {
    entity: props.field ? props.field.entity : props.entity,
    key: key.value.trim(),
    label: label.value.trim(),
    type: type.value,
    required: required.value,
    default_value: defaultValue.value.trim(),
    options: optionsText.value
      .split(/\n+/)
      .map((item) => item.trim())
      .filter(Boolean),
    in_ledger: inLedger.value,
    in_template: inTemplate.value,
    note: note.value.trim(),
  };
  const sortRaw = sort.value.trim();
  if (sortRaw !== "") {
    input.sort_order = Number(sortRaw);
  }
  return input;
}

async function submit(): Promise<void> {
  if (saving.value) {
    return;
  }
  saving.value = true;
  fieldErrors.value = [];
  errorText.value = "";
  try {
    const input = buildInput();
    if (props.field) {
      await updateField(props.field.id, input);
    } else {
      await createField(input);
    }
    emit("close");
    toast("字段已保存");
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
          <div id="fieldErrors">
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
              <label for="fieldLabel">字段名称<span class="req">*</span></label>
              <input id="fieldLabel" v-model="label" type="text" placeholder="例如：平方数">
            </div>
            <div class="field">
              <label for="fieldKey">字段键<span class="req">*</span></label>
              <input
                id="fieldKey"
                v-model="key"
                type="text"
                class="mono"
                placeholder="area_sqm"
                :disabled="isEdit"
              >
              <span class="help">小写字母、数字、下划线；用于数据键与模板占位符，创建后不可修改。</span>
            </div>
            <div class="field">
              <label for="fieldType">类型</label>
              <select id="fieldType" v-model="type" :disabled="isEdit && isBuiltIn">
                <option v-for="option in typeOptions" :key="option" :value="option">
                  {{ FIELD_TYPE_LABELS[option] }}
                </option>
              </select>
            </div>
            <div class="field">
              <label for="fieldSort">排序</label>
              <input id="fieldSort" v-model="sort" type="number" placeholder="留空自动排到最后">
            </div>
            <div class="field">
              <label for="fieldDefault">默认值</label>
              <input id="fieldDefault" v-model="defaultValue" type="text">
            </div>
            <div v-show="type === 'select'" id="fieldOptionsWrap" class="field full">
              <label for="fieldOptions">选项（单选字段，每行一个）</label>
              <textarea id="fieldOptions" v-model="optionsText" placeholder="每行一个选项"></textarea>
            </div>
            <div class="field full">
              <label for="fieldNote">说明</label>
              <input id="fieldNote" v-model="note" type="text">
            </div>
            <div class="field full">
              <div class="check-row">
                <input id="fieldRequired" v-model="required" type="checkbox"> 必填
              </div>
              <div class="check-row">
                <input id="fieldLedger" v-model="inLedger" type="checkbox"> 出现在应用内台账表格
              </div>
              <div class="check-row">
                <input id="fieldTemplate" v-model="inTemplate" type="checkbox"> 用于模板占位符
              </div>
            </div>
          </div>
        </div>
        <div class="modal-foot">
          <button type="button" @click="emit('close')">取消</button>
          <button type="button" class="primary" :disabled="saving" @click="submit">保存</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
// 字段新建/编辑弹窗：字段与交互逐条对应存量 app.js openFieldForm。
//   - 新建时只要求填「字段名称」，字段键按名称自动生成（见 fieldKey.ts），
//     只读预览在名称下方；高级选项里可手改，默认折叠；
//   - 字段键创建后不可修改（编辑时禁用）；
//   - 内置字段类型不可修改（类型下拉禁用），但名称/必填/默认值等仍可编辑；
//   - 选项仅单选字段展示；新建时台账/模板默认勾选；
//   - 保存后调用 refreshAfterWrite() 广播变更，各域按需重取字段。
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { ApiError } from "../../api/client";
import { createField, updateField } from "../../api/endpoints";
import type { ApiFieldError, FieldDefinition, FieldEntity, FieldInput, FieldType } from "../../api/types";
import { errorMessage, refreshAfterWrite, toast } from "../../bridge/app";
import FormErrors from "../../ui/FormErrors.vue";
import { CONTRACT_GROUP_PRESETS } from "../contracts/fieldGroups";
import { FIELD_KEY_PATTERN, generateFieldKey } from "./fieldKey";
import { FIELD_TYPE_LABELS, USER_FIELD_TYPES } from "./labels";

const props = defineProps<{
  /** 编辑目标；null 表示新建。 */
  field: FieldDefinition | null;
  /** 新建时归属的实体（编辑时以字段自身的 entity 为准）。 */
  entity: FieldEntity;
  /** 同实体既有字段键（含已归档），新建时用于自动避让冲突。 */
  existingKeys: string[];
}>();

const emit = defineEmits<{
  close: [];
}>();

const isEdit = props.field !== null;
const isBuiltIn = props.field?.built_in ?? false;

// 分组只用于合同表单（te-bzqhx）；相对方表单不分组，不展示该控件。
// 内置字段的分组由界面固定，控件只读展示、不提交，避免误导。
const showGroup = computed(
  () => (props.field?.entity ?? props.entity) === "contract",
);
const group = ref((props.field?.group ?? "").trim());

const label = ref(props.field?.label ?? "");
const type = ref<FieldType>(props.field?.type ?? "text");
const sort = ref(props.field ? String(props.field.sort_order) : "");
const defaultValue = ref(props.field?.default_value ?? "");
const optionsText = ref((props.field?.options ?? []).join("\n"));
const note = ref(props.field?.note ?? "");
const required = ref(props.field?.required ?? false);
const inLedger = ref(props.field ? props.field.in_ledger : true);
const inTemplate = ref(props.field ? props.field.in_template : true);

// 字段键：新建时按名称自动生成；在高级选项里手改后固定为用户输入，
// 直到点「恢复自动生成」。编辑时始终是字段自身的键（禁用输入）。
const keyManual = ref<string | null>(null);
const advancedOpen = ref(false);

const autoKey = computed(() => generateFieldKey(label.value, props.existingKeys));

const keyValue = computed<string>({
  get: () => {
    if (isEdit) {
      return props.field?.key ?? "";
    }
    return keyManual.value ?? autoKey.value;
  },
  set: (value: string) => {
    keyManual.value = value;
  },
});

const isManualKey = computed(() => !isEdit && keyManual.value !== null);

// 预览文案区分来源：自动生成时提示「将自动生成」，手改后（或编辑态）只显示
// 「字段键：<key>」，点「恢复自动生成」回到自动文案。
const keyPreviewNote = computed(() =>
  isEdit || isManualKey.value ? "字段键：" : "字段键将自动生成：",
);

// 自动生成的键始终合法且不冲突；这里只会在手改后报错。
const keyProblem = computed(() => {
  if (isEdit) {
    return "";
  }
  const value = keyValue.value.trim();
  if (!FIELD_KEY_PATTERN.test(value)) {
    return "字段键只能使用小写字母、数字和下划线，且以字母开头（如 area_sqm）";
  }
  if (props.existingKeys.includes(value)) {
    return `字段键 ${value} 已存在`;
  }
  return "";
});

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
    key: keyValue.value.trim(),
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
  // 内置字段分组固定、相对方表单不使用分组：省略即保持原值。
  if (showGroup.value && !isBuiltIn) {
    input.group = group.value.trim();
  }
  return input;
}

async function submit(): Promise<void> {
  if (saving.value) {
    return;
  }
  // 前端先挡住明显问题：名称必填；手改的字段键必须合法且不冲突。
  errorText.value = "";
  fieldErrors.value = [];
  if (!props.field && label.value.trim() === "") {
    errorText.value = "字段名称不能为空";
    return;
  }
  if (keyProblem.value) {
    errorText.value = keyProblem.value;
    advancedOpen.value = true;
    return;
  }
  saving.value = true;
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

function resumeAutoKey(): void {
  keyManual.value = null;
}

function onAdvancedToggle(event: Event): void {
  advancedOpen.value = (event.target as HTMLDetailsElement).open;
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
          <div>
            <FormErrors :field-errors="fieldErrors" :error-text="errorText" />
          </div>
          <div class="form-grid">
            <div class="field">
              <label for="fieldLabel">字段名称<span class="req">*</span></label>
              <input id="fieldLabel" v-model="label" type="text" placeholder="例如：平方数">
              <span class="help">
                {{ keyPreviewNote }}<code class="mono" :class="{ 'key-problem': keyProblem }">{{ keyValue || "—" }}</code>
              </span>
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
            <div v-if="showGroup" class="field">
              <label for="fieldGroup">分组</label>
              <input
                id="fieldGroup"
                v-model="group"
                type="text"
                list="fieldGroupPresets"
                placeholder="留空归入「其他约定」"
                :disabled="isBuiltIn"
              >
              <datalist id="fieldGroupPresets">
                <option v-for="preset in CONTRACT_GROUP_PRESETS" :key="preset" :value="preset" />
              </datalist>
              <span class="help">
                {{ isBuiltIn
                  ? "内置字段分组固定（基本信息 / 租期 / 租金与押金），不可修改。"
                  : "合同表单按分组展示；可选预设，也可自定义名称。" }}
              </span>
            </div>
            <div class="field">
              <label for="fieldDefault">默认值</label>
              <input id="fieldDefault" v-model="defaultValue" type="text">
            </div>
            <div v-show="type === 'select'" class="field full">
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
          <details class="advanced-options" :open="advancedOpen" @toggle="onAdvancedToggle">
            <summary>高级选项</summary>
            <div class="field">
              <label for="fieldKey">字段键</label>
              <input
                id="fieldKey"
                v-model="keyValue"
                type="text"
                class="mono"
                placeholder="area_sqm"
                :disabled="isEdit"
              >
              <span v-if="isEdit" class="help">字段键创建后不可修改，用于数据键与模板占位符。</span>
              <template v-else>
                <span class="help">自动生成，一般无需修改；仅手写模板或 agent 接入时需要。</span>
                <span v-if="keyProblem" class="help key-problem">{{ keyProblem }}</span>
                <button
                  v-if="isManualKey"
                  type="button"
                  class="adv-reset"
                  @click="resumeAutoKey"
                >恢复自动生成</button>
              </template>
            </div>
          </details>
        </div>
        <div class="modal-foot">
          <button type="button" @click="emit('close')">取消</button>
          <button type="button" class="primary" :disabled="saving" @click="submit">保存</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

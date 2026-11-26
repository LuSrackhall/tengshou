<script setup lang="ts">
// 单个合同动态字段控件，覆盖 app.js fieldControlHTML 对合同表单的用法：
// text / longtext / number / money / date / select / status / bool / party。
// 与相对方表单的差异：合同字段额外显示模板占位（模板：{{...}}），
// 内置字段「合同编号」在编辑时禁用。required 星号与 help 拼接顺序保持一致。
// 金额字段（te-7c5v1）额外提供中文大写：默认随金额实时自动生成，可「改」
// 为手填覆盖，清空覆盖恢复自动。
import { computed, ref, watch } from "vue";
import type { FieldDefinition } from "../../api/types";
import { templateHintCode } from "./format";
import { moneyWords } from "./moneyWords";

const props = withDefaults(
  defineProps<{
    field: FieldDefinition;
    modelValue: string;
    readonly?: boolean;
    /** 手填大写覆盖值；空串表示自动大写。 */
    wordsValue?: string;
  }>(),
  { readonly: false, wordsValue: "" },
);

const emit = defineEmits<{
  "update:modelValue": [value: string];
  "update:wordsValue": [value: string];
}>();

const id = computed(() => `f_contract_${props.field.key}`);
const wordsId = computed(() => `f_contract_words_${props.field.key}`);
const isFull = computed(() => props.field.type === "longtext");
const options = computed(() => props.field.options ?? []);

/** 金额自动大写预览（非法/空金额为空串，界面显示占位符）。 */
const autoWords = computed(() => moneyWords(props.modelValue));
/** 手填覆盖模式：覆盖值非空即进入；清空后回到自动预览。 */
const manualWords = ref(props.wordsValue.trim() !== "");

watch(
  () => props.wordsValue,
  (value) => {
    manualWords.value = value.trim() !== "";
  },
);

function startManualWords(): void {
  // 从自动值开始改，避免用户面对空白输入框重新敲一遍。
  if (!props.wordsValue.trim()) {
    emit("update:wordsValue", autoWords.value);
  }
  manualWords.value = true;
}

function onWordsInput(event: Event): void {
  const value = (event.target as HTMLInputElement).value;
  emit("update:wordsValue", value);
  // 清空输入框视为恢复自动大写。
  manualWords.value = value.trim() !== "";
}

function restoreAutoWords(): void {
  emit("update:wordsValue", "");
  manualWords.value = false;
}

const hintCode = computed(() =>
  props.field.in_template && props.field.entity === "contract"
    ? templateHintCode(props.field)
    : "",
);

const showHelp = computed(
  () => Boolean(props.field.note) || Boolean(hintCode.value) || !props.field.built_in,
);

const inputType = computed(() => {
  switch (props.field.type) {
    case "number":
    case "money":
      return "number";
    case "date":
      return "date";
    default:
      return "text";
  }
});

const stepAttr = computed(() => {
  if (props.field.type === "number") {
    return "any";
  }
  if (props.field.type === "money") {
    return "0.01";
  }
  return undefined;
});

const minAttr = computed(() => (props.field.type === "money" ? "0" : undefined));

const boolValue = computed(() => {
  if (props.modelValue === "是" || props.modelValue === "否") {
    return props.modelValue;
  }
  return "";
});

function onInput(event: Event): void {
  const target = event.target as HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement;
  emit("update:modelValue", target.value);
}
</script>

<template>
  <div class="field" :class="{ full: isFull }" :data-key="field.key">
    <label :for="id">{{ field.label }}<span v-if="field.required" class="req">*</span></label>

    <textarea
      v-if="field.type === 'longtext'"
      :id="id"
      :value="modelValue"
      :disabled="readonly"
      @input="onInput"
    ></textarea>

    <select
      v-else-if="field.type === 'select' || field.type === 'status'"
      :id="id"
      :value="modelValue"
      :disabled="readonly"
      @change="onInput"
    >
      <option value="">{{ field.required ? "请选择" : "（空）" }}</option>
      <option v-for="option in options" :key="option" :value="option">{{ option }}</option>
    </select>

    <select
      v-else-if="field.type === 'bool'"
      :id="id"
      :value="boolValue"
      :disabled="readonly"
      @change="onInput"
    >
      <option value="">（空）</option>
      <option value="是">是</option>
      <option value="否">否</option>
    </select>

    <input
      v-else-if="field.type === 'party'"
      :id="id"
      type="text"
      :value="modelValue"
      list="partyNameList"
      placeholder="输入或选择相对方名称"
      :disabled="readonly"
      @input="onInput"
    >

    <input
      v-else
      :id="id"
      :type="inputType"
      :step="stepAttr"
      :min="minAttr"
      :value="modelValue"
      :disabled="readonly"
      @input="onInput"
    >

    <div v-if="field.type === 'money'" class="money-words">
      <template v-if="!manualWords">
        <span class="money-words-auto">大写（自动）：{{ autoWords || "—" }}</span>
        <button type="button" class="link" @click="startManualWords">改</button>
      </template>
      <template v-else>
        <label :for="wordsId">大写（手填覆盖）</label>
        <input
          :id="wordsId"
          type="text"
          :value="wordsValue"
          placeholder="清空恢复自动大写"
          :disabled="readonly"
          @input="onWordsInput"
        >
        <button type="button" class="link" @click="restoreAutoWords">恢复自动</button>
      </template>
    </div>

    <span v-if="showHelp" class="help">
      <template v-if="field.note">{{ field.note }}</template>
      <template v-if="hintCode"><template v-if="field.note"> · </template>模板：<code>{{ hintCode }}</code></template>
      <template v-if="!field.built_in"><template v-if="field.note || hintCode"> · </template>键：{{ field.key }}</template>
    </span>
  </div>
</template>

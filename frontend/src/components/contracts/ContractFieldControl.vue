<script setup lang="ts">
// 单个合同动态字段控件，覆盖 app.js fieldControlHTML 对合同表单的用法：
// text / longtext / number / money / date / select / status / bool / party。
// 与相对方表单的差异：合同字段额外显示模板占位（模板：{{...}}），
// 内置字段「合同编号」在编辑时禁用。required 星号与 help 拼接顺序保持一致。
import { computed } from "vue";
import type { FieldDefinition } from "../../api/types";
import { templateHintCode } from "./format";

const props = defineProps<{
  field: FieldDefinition;
  modelValue: string;
  readonly?: boolean;
}>();

const emit = defineEmits<{
  "update:modelValue": [value: string];
}>();

const id = computed(() => `f_contract_${props.field.key}`);
const isFull = computed(() => props.field.type === "longtext");
const options = computed(() => props.field.options ?? []);

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

    <span v-if="showHelp" class="help">
      <template v-if="field.note">{{ field.note }}</template>
      <template v-if="hintCode"><template v-if="field.note"> · </template>模板：<code>{{ hintCode }}</code></template>
      <template v-if="!field.built_in"><template v-if="field.note || hintCode"> · </template>键：{{ field.key }}</template>
    </span>
  </div>
</template>

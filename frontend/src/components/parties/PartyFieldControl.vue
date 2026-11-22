<script setup lang="ts">
// 单个动态字段控件，覆盖 app.js fieldControlHTML 对相对方表单的用法：
// text / longtext / number / money / date / select / status / bool / party。
// 内置字段与自定义字段共用；required 星号与 help（note · 键：key）保持一致。
import { computed } from "vue";
import type { FieldDefinition } from "../../api/types";

const props = defineProps<{
  field: FieldDefinition;
  modelValue: string;
}>();

const emit = defineEmits<{
  "update:modelValue": [value: string];
}>();

const id = computed(() => `f_party_${props.field.key}`);
const isFull = computed(() => props.field.type === "longtext");
const options = computed(() => props.field.options ?? []);

const help = computed(() => {
  const parts: string[] = [];
  if (props.field.note) {
    parts.push(props.field.note);
  }
  if (!props.field.built_in) {
    parts.push(`键：${props.field.key}`);
  }
  return parts.join(" · ");
});

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

    <textarea v-if="field.type === 'longtext'" :id="id" :value="modelValue" @input="onInput"></textarea>

    <select
      v-else-if="field.type === 'select' || field.type === 'status'"
      :id="id"
      :value="modelValue"
      @change="onInput"
    >
      <option value="">{{ field.required ? "请选择" : "（空）" }}</option>
      <option v-for="option in options" :key="option" :value="option">{{ option }}</option>
    </select>

    <select v-else-if="field.type === 'bool'" :id="id" :value="boolValue" @change="onInput">
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
      @input="onInput"
    >

    <input
      v-else
      :id="id"
      :type="inputType"
      :step="stepAttr"
      :min="minAttr"
      :value="modelValue"
      @input="onInput"
    >

    <span v-if="help" class="help">{{ help }}</span>
  </div>
</template>

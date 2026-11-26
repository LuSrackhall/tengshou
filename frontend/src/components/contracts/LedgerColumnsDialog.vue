<script setup lang="ts">
// 台账列设置弹层（te-iuns4）：列出全部 in_ledger 字段，勾选决定台账显示
// 哪些列。改动即时生效并只存本机（localStorage），不入库、不进审计；未勾选
// 的字段仍进编辑表单、模板与 Excel 导出。至少保留一列，避免台账只剩勾选
// 与操作列。
import { computed, ref } from "vue";
import type { FieldDefinition } from "../../api/types";
import ModalDialog from "../../ui/ModalDialog.vue";

const props = defineProps<{
  fields: FieldDefinition[];
  selectedKeys: string[];
}>();

const emit = defineEmits<{
  "update:selected-keys": [keys: string[]];
  restore: [];
  close: [];
}>();

const hint = ref("");

const checked = computed(() => new Set(props.selectedKeys));

function toggle(field: FieldDefinition, event: Event): void {
  const input = event.target as HTMLInputElement;
  if (!input.checked && checked.value.size <= 1) {
    input.checked = true;
    hint.value = "至少保留一列";
    return;
  }
  hint.value = "";
  emit(
    "update:selected-keys",
    props.fields
      .filter((item) => (item.key === field.key ? input.checked : checked.value.has(item.key)))
      .map((item) => item.key),
  );
}

function restore(): void {
  hint.value = "";
  emit("restore");
}
</script>

<template>
  <ModalDialog title="台账列设置" @close="emit('close')">
    <p class="hint">
      勾选要在合同台账中显示的列。设置只保存在本机浏览器，不影响合同表单、模板与 Excel 导出。
    </p>
    <div class="ledger-column-grid">
      <label v-for="field in fields" :key="field.key" class="check-row">
        <input
          :id="`ledgerColumn_${field.key}`"
          type="checkbox"
          :data-column-key="field.key"
          :checked="checked.has(field.key)"
          @change="toggle(field, $event)"
        >
        {{ field.label }}
      </label>
    </div>
    <p v-if="hint" class="ledger-column-hint">{{ hint }}</p>
    <template #foot>
      <button type="button" id="restoreLedgerColumnsBtn" @click="restore">恢复默认</button>
      <span class="spacer"></span>
      <button type="button" class="primary" @click="emit('close')">完成</button>
    </template>
  </ModalDialog>
</template>

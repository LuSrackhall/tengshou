<script setup lang="ts">
// 合同台账表格（从 ContractsView 拆出以守住单文件行数上限）：动态字段列、
// 状态徽标与到期提醒徽标、行操作（生成 PDF / 扫码签名 / 编辑 / 终止 / 删除）
// 以及批量勾选。数据与操作全部由父组件传入/承接。
import { computed } from "vue";
import type { Contract, FieldDefinition, Party } from "../../api/types";
import { contractFieldValue } from "./format";

const props = defineProps<{
  contracts: Contract[];
  fields: FieldDefinition[];
  parties: Party[];
  selectedIds: string[];
  loaded: boolean;
}>();

const emit = defineEmits<{
  "toggle-select": [id: string];
  "toggle-all": [checked: boolean];
  edit: [contract: Contract];
  remove: [contract: Contract];
  sign: [contract: Contract];
  generate: [contract: Contract];
  terminate: [contract: Contract];
}>();

const visibleIds = computed(() => props.contracts.map((contract) => contract.id));
const selectedVisible = computed(
  () => visibleIds.value.filter((id) => props.selectedIds.includes(id)).length,
);
const allSelected = computed(
  () => visibleIds.value.length > 0 && selectedVisible.value === visibleIds.value.length,
);
const indeterminate = computed(
  () => selectedVisible.value > 0 && selectedVisible.value < visibleIds.value.length,
);

function isSelected(id: string): boolean {
  return props.selectedIds.includes(id);
}

function cellValue(contract: Contract, field: FieldDefinition) {
  return contractFieldValue(contract, field, props.parties);
}

function cellClass(contract: Contract, field: FieldDefinition): string {
  const value = cellValue(contract, field);
  if (value.badge) {
    return "";
  }
  return [value.mono ? "mono" : "", value.money ? "money" : "", value.wrap ? "wrap" : ""]
    .filter(Boolean)
    .join(" ");
}

function expiryLabel(contract: Contract): string {
  return contract.expiry_state === "expired" ? "已到期未续签" : "即将到期";
}

function onToggleAll(event: Event): void {
  emit("toggle-all", (event.target as HTMLInputElement).checked);
}

function onToggleSelect(id: string): void {
  emit("toggle-select", id);
}
</script>

<template>
  <div class="table-wrap">
    <table id="contractTable" class="data-table">
      <template v-if="loaded">
        <tbody v-if="!fields.length">
          <tr><td class="empty">没有可显示的台账字段，请先在「字段」页新增字段。</td></tr>
        </tbody>
        <template v-else>
          <thead>
            <tr>
              <th class="select-col">
                <input
                  id="contractSelectAll"
                  type="checkbox"
                  title="全选当前台账合同"
                  aria-label="全选当前台账合同"
                  :checked="allSelected"
                  :indeterminate="indeterminate"
                  @change="onToggleAll"
                >
              </th>
              <th v-for="field in fields" :key="field.key">{{ field.label }}</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="contract in contracts"
              :key="contract.id"
              :data-id="contract.id"
              @click="emit('edit', contract)"
            >
              <td class="select-col">
                <input
                  type="checkbox"
                  :data-select="contract.id"
                  :checked="isSelected(contract.id)"
                  :aria-label="`选择合同 ${contract.contract_no}`"
                  @click.stop
                  @change="onToggleSelect(contract.id)"
                >
              </td>
              <td v-for="field in fields" :key="field.key" :class="cellClass(contract, field)">
                <span v-if="cellValue(contract, field).badge" class="badge" :class="cellValue(contract, field).badge">
                  {{ cellValue(contract, field).text }}
                </span>
                <template v-else>{{ cellValue(contract, field).text }}</template>
                <span
                  v-if="field.key === 'status' && contract.expiry_state"
                  class="badge expiry"
                  :class="contract.expiry_state"
                >{{ expiryLabel(contract) }}</span>
              </td>
              <td>
                <div class="actions">
                  <button type="button" data-action="generate" @click.stop="emit('generate', contract)">生成 PDF</button>
                  <button type="button" data-action="sign" @click.stop="emit('sign', contract)">扫码签名</button>
                  <button type="button" data-action="edit" @click.stop="emit('edit', contract)">编辑</button>
                  <button
                    v-if="contract.status === 'active'"
                    type="button"
                    data-action="terminate"
                    @click.stop="emit('terminate', contract)"
                  >终止</button>
                  <button type="button" class="danger" data-action="delete" @click.stop="emit('remove', contract)">删除</button>
                </div>
              </td>
            </tr>
            <tr v-if="!contracts.length">
              <td class="empty" :colspan="fields.length + 2">还没有合同。可以新建，或下载 Excel 模板填写后导入。</td>
            </tr>
          </tbody>
        </template>
      </template>
    </table>
  </div>
</template>

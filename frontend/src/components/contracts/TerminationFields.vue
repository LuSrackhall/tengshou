<script setup lang="ts">
// 合同「已终止」状态的附加字段（te-x54oj）：终止日期与终止原因必填，
// 备注可选。离开已终止状态后这些值不会提交，后端也会清空。
import type { TerminationReason } from "../../api/types";

defineProps<{
  terminatedOn: string;
  reason: string;
  note: string;
}>();

const emit = defineEmits<{
  "update:terminatedOn": [value: string];
  "update:reason": [value: string];
  "update:note": [value: string];
}>();

const REASONS: Array<{ value: TerminationReason; label: string }> = [
  { value: "early", label: "提前退租" },
  { value: "void", label: "作废" },
  { value: "other", label: "其他" },
];

function onTerminatedOn(event: Event): void {
  emit("update:terminatedOn", (event.target as HTMLInputElement).value);
}

function onReason(event: Event): void {
  emit("update:reason", (event.target as HTMLSelectElement).value);
}

function onNote(event: Event): void {
  emit("update:note", (event.target as HTMLInputElement).value);
}
</script>

<template>
  <div class="field" data-key="terminated_on">
    <label for="f_contract_terminated_on">终止日期<span class="req">*</span></label>
    <input
      id="f_contract_terminated_on"
      type="date"
      :value="terminatedOn"
      @input="onTerminatedOn"
    >
    <span class="help">终止是主动行为；到期只做提醒，不会自动终止或释放房产。</span>
  </div>
  <div class="field" data-key="termination_reason">
    <label for="f_contract_termination_reason">终止原因<span class="req">*</span></label>
    <select id="f_contract_termination_reason" :value="reason" @change="onReason">
      <option value="">请选择</option>
      <option v-for="item in REASONS" :key="item.value" :value="item.value">
        {{ item.label }}
      </option>
    </select>
  </div>
  <div class="field full" data-key="termination_note">
    <label for="f_contract_termination_note">终止备注</label>
    <input
      id="f_contract_termination_note"
      type="text"
      :value="note"
      placeholder="可选，例如退租原因或沟通情况"
      @input="onNote"
    >
  </div>
</template>

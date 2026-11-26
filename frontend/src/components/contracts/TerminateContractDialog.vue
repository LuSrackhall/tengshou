<script setup lang="ts">
// 台账行内「终止」对话框（te-x54oj）：终止日期与原因必填，备注可选。
// 终止是主动行为；到期只做提醒，不会自动改状态或释放房产。
import { onBeforeUnmount, onMounted, ref } from "vue";
import { terminateContract } from "../../api/endpoints";
import type { Contract, TerminationReason } from "../../api/types";
import { errorMessage, refreshAfterWrite, toast } from "../../bridge/app";
import { todayISO } from "./lease";

const props = defineProps<{ contract: Contract }>();

const emit = defineEmits<{
  close: [];
  terminated: [contract: Contract];
}>();

const REASONS: Array<{ value: TerminationReason; label: string }> = [
  { value: "early", label: "提前退租" },
  { value: "void", label: "作废" },
  { value: "other", label: "其他" },
];

const terminatedOn = ref(todayISO());
const reason = ref<TerminationReason>("early");
const note = ref("");
const saving = ref(false);
const errorText = ref("");

async function submit(): Promise<void> {
  if (saving.value) {
    return;
  }
  if (!terminatedOn.value) {
    errorText.value = "请填写终止日期";
    return;
  }
  if (!reason.value) {
    errorText.value = "请选择终止原因";
    return;
  }
  saving.value = true;
  errorText.value = "";
  try {
    const result = await terminateContract(props.contract.id, {
      terminated_on: terminatedOn.value,
      reason: reason.value,
      note: note.value.trim(),
    });
    toast(`合同 ${props.contract.contract_no} 已终止`);
    await refreshAfterWrite();
    emit("terminated", result.item);
    emit("close");
  } catch (error) {
    saving.value = false;
    errorText.value = errorMessage(error);
  }
}

function onKeydown(event: KeyboardEvent): void {
  if (event.key === "Escape") {
    emit("close");
  }
}

onMounted(() => document.addEventListener("keydown", onKeydown));
onBeforeUnmount(() => document.removeEventListener("keydown", onKeydown));
</script>

<template>
  <Teleport to="body">
    <div class="modal-backdrop">
      <div class="modal" role="dialog" aria-modal="true" id="terminateContractDialog">
        <div class="modal-head">
          <h2>终止合同 · {{ contract.contract_no }}</h2>
          <button type="button" class="close" aria-label="关闭" @click="emit('close')">×</button>
        </div>
        <div class="modal-body">
          <div v-if="errorText" class="form-errors">{{ errorText }}</div>
          <p class="help">
            终止后合同保留在台账并显示「已终止」，房产恢复空置；到期提醒不会自动终止合同。
          </p>
          <div class="form-grid">
            <div class="field">
              <label for="terminateContractDate">终止日期<span class="req">*</span></label>
              <input id="terminateContractDate" v-model="terminatedOn" type="date">
            </div>
            <div class="field">
              <label for="terminateContractReason">终止原因<span class="req">*</span></label>
              <select id="terminateContractReason" v-model="reason">
                <option v-for="item in REASONS" :key="item.value" :value="item.value">
                  {{ item.label }}
                </option>
              </select>
            </div>
            <div class="field full">
              <label for="terminateContractNote">备注</label>
              <input
                id="terminateContractNote"
                v-model="note"
                type="text"
                placeholder="可选，例如退租原因或沟通情况"
              >
            </div>
          </div>
        </div>
        <div class="modal-foot">
          <button type="button" @click="emit('close')">取消</button>
          <button type="button" class="primary" :disabled="saving" @click="submit">确认终止</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

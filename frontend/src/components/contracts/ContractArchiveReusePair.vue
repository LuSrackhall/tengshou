<script setup lang="ts">
// 合同表单的档案复用区块组（te-48hkl，Q5）：乙方程默认复用 + 出租方（甲方）
// 档案默认复用。两块的勾选与告知逻辑相同，由 ContractArchiveReuse 各自维护；
// 保存合同后按乙方 → 甲方顺序串行 apply，避免并发写同一合同的签名/公章关联。
import { ref } from "vue";
import type { Party } from "../../api/types";
import ContractArchiveReuse from "./ContractArchiveReuse.vue";

const props = defineProps<{ partyA: Party | null; partyB: Party | null }>();

const partyBRef = ref<{ apply: (contractId: string) => Promise<void> } | null>(null);
const partyARef = ref<{ apply: (contractId: string) => Promise<void> } | null>(null);

/** 保存合同成功后调用：乙方与甲方（如已选且不同）依次复用，失败只提示。
 *
 * 调用方会在弹窗关闭前启动本函数；弹窗关闭后子组件 ref 变为 null，
 * 因此先把两个 apply 句柄与是否复用甲方取下来，再串行执行。
 */
async function apply(contractId: string): Promise<void> {
  const applyB = partyBRef.value?.apply;
  const applyA = partyARef.value?.apply;
  const reusePartyA = Boolean(props.partyA && props.partyA.id !== props.partyB?.id);
  if (applyB) {
    await applyB(contractId);
  }
  if (reusePartyA && applyA) {
    await applyA(contractId);
  }
}

defineExpose({ apply });
</script>

<template>
  <ContractArchiveReuse ref="partyBRef" :party="partyB" />
  <ContractArchiveReuse
    v-if="partyA && partyA.id !== partyB?.id"
    ref="partyARef"
    :party="partyA"
    title="甲方档案复用"
    section-id="contractArchiveReuseA"
  />
</template>

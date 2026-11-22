<script setup lang="ts">
// 档案的历史合同区块（B7）：与 app.js loadPartyContracts 的 DOM/文案一致。
// 行内「查看 / 续签」交给父组件关闭弹窗后经桥打开合同表单。
import { onBeforeUnmount, onMounted, ref } from "vue";
import { listContracts } from "../../api/endpoints";
import type { Contract } from "../../api/types";
import { errorMessage, subscribeChange } from "../../bridge/app";
import { statusLabel } from "./format";

const props = defineProps<{ partyId: string }>();

const emit = defineEmits<{
  "open-contract": [contract: Contract, renew: boolean];
}>();

const items = ref<Contract[]>([]);
const total = ref(0);
const loaded = ref(false);
const errorText = ref("");
let loadSeq = 0;
let unsubscribe: (() => void) | null = null;

async function load(): Promise<void> {
  const seq = ++loadSeq;
  try {
    const result = await listContracts({ party_id: props.partyId, limit: 20 });
    if (seq !== loadSeq) {
      return;
    }
    items.value = result.items ?? [];
    total.value = result.total ?? items.value.length;
    loaded.value = true;
    errorText.value = "";
  } catch (error) {
    if (seq !== loadSeq) {
      return;
    }
    errorText.value = errorMessage(error);
  }
}

onMounted(() => {
  void load();
  unsubscribe = subscribeChange((notice) => {
    // 合同增删改或本档案变更都会改变历史合同列表；重取一次保证外部写入可见。
    if (
      notice.entity === "contract" ||
      (notice.entity === "party" && notice.entity_id === props.partyId)
    ) {
      void load();
    }
  });
});

onBeforeUnmount(() => {
  unsubscribe?.();
  unsubscribe = null;
});
</script>

<template>
  <div class="audit-block" :class="{ error: errorText !== '' }">
    <div class="audit-head">
      历史合同<template v-if="loaded && !errorText">（{{ total }}）</template>
    </div>
    <p v-if="!loaded && !errorText" class="muted">加载中…</p>
    <template v-else-if="errorText">{{ errorText }}</template>
    <ul v-else-if="items.length" class="audit-list">
      <li v-for="contract in items" :key="contract.id">
        <div class="audit-line">
          <span class="mono">{{ contract.contract_no }}</span>
          <span class="audit-summary">「{{ contract.title }}」{{ contract.year }} 年 · {{ statusLabel(contract.status) }}</span>
          <button type="button" @click.stop="emit('open-contract', contract, false)">查看</button>
          <button type="button" @click.stop="emit('open-contract', contract, true)">续签</button>
        </div>
      </li>
    </ul>
    <p v-else class="muted">该相对方还没有合同记录。保存新合同后会自动关联到这份档案。</p>
  </div>
</template>

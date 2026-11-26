<script setup lang="ts">
// 合同表单顶部的续签来源 / 续签草稿 / 档案新建提示（从 ContractFormModal 拆出）。
import { computed } from "vue";
import type { Contract, RenewalDraft } from "../../api/types";
import type { PartyRef } from "../../bridge/app";

const props = defineProps<{
  showSource: boolean;
  source: Contract | null;
  sourceMissing: boolean;
  renewal: RenewalDraft | null;
  partyB: PartyRef | null;
  reusedCount: number;
}>();

const emit = defineEmits<{
  "open-source": [];
}>();

const sourceClosed = computed(() => props.sourceMissing && !props.source);
</script>

<template>
  <div v-if="showSource" id="renewalSource" class="renewal-banner">
    <template v-if="source">续签自 <strong>{{ source.contract_no }}「{{ source.title }}」</strong>
      <button type="button" id="openSourceContract" @click="emit('open-source')">查看来源合同</button>
    </template>
    <template v-else-if="sourceClosed">续签来源合同已不存在（来源编号仍保留在本合同记录中）。</template>
    <template v-else>正在加载续签来源…</template>
  </div>

  <div v-if="renewal" class="renewal-banner">
    续签自 <strong>{{ renewal.source.contract_no }}「{{ renewal.source.title }}」</strong>：
    已带出 {{ reusedCount }} 项信息，均可修改。合同编号自动分配，签订 / 生效 / 到期日期与状态已重置；
    签名与公章不会沿用，生成 PDF 前需按新内容重新采集。
  </div>
  <div v-else-if="partyB" class="renewal-banner">
    以相对方档案「{{ partyB.name }}」新建合同：
    仅带出档案信息，不复制旧合同内容；模板与字段由你选择，保存后按正常流程采集签名。
  </div>
</template>

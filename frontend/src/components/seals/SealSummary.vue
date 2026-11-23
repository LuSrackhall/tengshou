<script setup lang="ts">
// 合同详情弹窗里的「已关联公章」摘要（公章域已迁出 app.js）。
// 合同详情表单（components/contracts/ContractFormModal.vue）直接挂载本组件；
// 结构与迁移前 sealsSummaryHTML 的输出一致。
import type { Seal } from "../../api/types";

defineProps<{ seals: Seal[] }>();
</script>

<template>
  <div class="signature-summary seal-summary">
    <strong>已关联公章 {{ seals.length }} 枚</strong>
    <ul>
      <li v-for="seal in seals" :key="seal.id">
        {{ seal.name }} · {{ seal.width_px }}×{{ seal.height_px }}px ·
        <code>{{ seal.sha256.slice(0, 12) }}</code>
      </li>
    </ul>
  </div>
</template>

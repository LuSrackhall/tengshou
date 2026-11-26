<script setup lang="ts">
// 合同表单分组区块（te-bzqhx）：可折叠、默认展开；空组由 fieldGroups
// 的分组逻辑直接省略，组内沿用 .form-grid 两列布局。
defineProps<{ title: string }>();
</script>

<template>
  <details class="form-section" open>
    <summary>{{ title }}</summary>
    <div class="form-grid"><slot /></div>
  </details>
</template>

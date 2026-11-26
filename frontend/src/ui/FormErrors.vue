<script setup lang="ts">
// 表单错误汇总：422 逐字段错误优先展示，否则显示整体错误文案。
// 合同与字段弹窗共用，类名 .form-errors 与迁移前一致。
import type { ApiFieldError } from "../api/types";

defineProps<{
  fieldErrors: ApiFieldError[];
  errorText: string;
}>();
</script>

<template>
  <div v-if="fieldErrors.length" class="form-errors">
    请修正以下问题：<ul>
      <li v-for="problem in fieldErrors" :key="problem.field">
        {{ problem.label || problem.field }}：{{ problem.message }}
      </li>
    </ul>
  </div>
  <div v-else-if="errorText" class="form-errors">{{ errorText }}</div>
</template>

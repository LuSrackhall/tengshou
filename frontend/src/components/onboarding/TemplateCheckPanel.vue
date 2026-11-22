<script setup lang="ts">
// 模板体检面板（阶段 1 迁出 app.js 的 renderOnboardingTemplateCheck）。
// 校验逻辑仍在服务端（GET /templates/{id}/check），这里只做展示。
import type { TemplateCheck } from "../../api/types";
import type { TemplateCheckState } from "./useOnboarding";

defineProps<{
  state: TemplateCheckState;
  data: TemplateCheck | null;
  error: string;
  templateName: string;
  templateVersion: number;
}>();

const emit = defineEmits<{ goFields: []; goCalibrate: [] }>();
</script>

<template>
  <div id="onboardTemplateCheck" class="onboard-checks">
    <p v-if="state === 'idle'" class="hint">导入模板后，这里会比对模板占位与字段定义，指出未知字段、缺必填字段、缺签名或公章位置。</p>
    <p v-else-if="state === 'missing'" class="hint">还没有可体检的模板；先完成第 1 步导入模板。</p>
    <p v-else-if="state === 'loading'" class="hint">正在检查「{{ templateName }}」v{{ templateVersion }}…</p>
    <div v-else-if="state === 'error'" class="form-errors">模板体检失败：{{ error }}</div>
    <template v-else-if="data">
      <p class="muted">「{{ data.name }}」v{{ data.version }} · {{ data.field_placements || 0 }} 个字段位置 · {{ data.signature_placements || 0 }} 个签名位置 · {{ data.seal_placements || 0 }} 个公章位置</p>
      <template v-if="data.issues.length">
        <div
          v-for="(issue, index) in data.issues"
          :key="index"
          class="onboard-check"
          :class="issue.severity === 'error' ? 'error' : 'warn'"
        >
          <div class="onboard-check-head">
            <span class="onboard-dot"></span>
            <strong>{{ issue.message }}</strong>
          </div>
          <p class="onboard-next">下一步：{{ issue.action }}</p>
        </div>
      </template>
      <div v-else class="onboard-check ok">
        <div class="onboard-check-head"><span class="onboard-dot"></span><strong>模板占位与字段定义一致，未发现缺失。</strong></div>
      </div>
      <div class="onboard-check-actions">
        <button type="button" id="onboardGoFields" @click="emit('goFields')">去字段页</button>
        <button type="button" id="onboardGoCalibrate" @click="emit('goCalibrate')">去模板标定</button>
      </div>
    </template>
  </div>
</template>

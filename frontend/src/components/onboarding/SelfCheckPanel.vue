<script setup lang="ts">
// 本机自检面板（阶段 1 迁出 app.js 的 renderOnboardingCheck）。
// DOM 结构、类名与文案逐条对应迁移前输出；修复入口由宿主切 tab。
import type { OnboardingCheckItem, OnboardingCheckResponse } from "../../api/types";
import { CHECK_STATUS_LABELS, checkBadgeClass, tabLabel } from "./format";
import type { CheckState } from "./useOnboarding";

defineProps<{
  state: CheckState;
  data: OnboardingCheckResponse | null;
  error: string;
}>();

const emit = defineEmits<{ openTab: [target: string] }>();

function targetLabel(item: OnboardingCheckItem): string {
  return tabLabel(item.target ?? "");
}

function openTarget(item: OnboardingCheckItem): void {
  emit("openTab", item.target ?? "");
}
</script>

<template>
  <div id="onboardCheckList" class="onboard-checks">
    <p v-if="state === 'idle'" class="hint">运行自检会检查数据目录、数据库、Word 转换、局域网签名、字段 / 模板 / 样例就绪度、备份与磁盘空间，每项失败都给下一步。</p>
    <p v-else-if="state === 'loading'" class="hint">正在自检…</p>
    <div v-else-if="state === 'error'" class="form-errors">自检失败：{{ error }}</div>
    <template v-else-if="data">
      <div
        v-for="item in data.items"
        :key="item.id"
        class="onboard-check"
        :class="item.status"
      >
        <div class="onboard-check-head">
          <span class="onboard-dot"></span>
          <strong>{{ item.label }}</strong>
          <span class="badge" :class="checkBadgeClass(item.status)">{{
            CHECK_STATUS_LABELS[item.status] ?? item.status
          }}</span>
        </div>
        <p v-if="item.detail" class="muted">{{ item.detail }}</p>
        <p v-if="item.status !== 'ok' && item.action" class="onboard-next">下一步：{{ item.action }}</p>
        <button
          v-if="item.status !== 'ok' && item.target"
          type="button"
          :data-target="item.target"
          @click="openTarget(item)"
        >前往「{{ targetLabel(item) }}」</button>
      </div>
    </template>
  </div>
</template>

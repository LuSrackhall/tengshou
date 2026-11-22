<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import {
  subscribeLiveStatus,
  type LiveStatusMode,
  type LiveStatusState,
} from "../bridge/live-status";

// 文案与旧 app.js 的 setLiveStatus 完全一致；初始值对应旧 index.html
// 里的静态文案，避免首屏出现闪烁或措辞变化。
const LABELS: Record<LiveStatusMode, string> = {
  connecting: "同步中",
  sse: "实时同步",
  poll: "轮询同步",
  offline: "同步中断",
};

const TITLES: Record<LiveStatusMode, string> = {
  connecting: "界面与本地服务的数据同步状态",
  sse: "已连接本地变更事件流，外部写入会立即显示",
  poll: "事件流不可用，已降级为 3 秒轮询；外部写入会在数秒内显示",
  offline: "暂时无法接收变更事件，正在重试",
};

const state = ref<LiveStatusState>({ mode: "connecting", detail: "" });

const label = computed(() => LABELS[state.value.mode]);
const title = computed(() => state.value.detail || TITLES[state.value.mode]);

let unsubscribe: (() => void) | null = null;

onMounted(() => {
  unsubscribe = subscribeLiveStatus((next) => {
    state.value = next;
  });
});

onBeforeUnmount(() => {
  unsubscribe?.();
  unsubscribe = null;
});
</script>

<template>
  <span class="live-status" :class="state.mode" :title="title">{{ label }}</span>
</template>

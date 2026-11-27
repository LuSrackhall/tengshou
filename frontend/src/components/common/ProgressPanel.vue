<script setup lang="ts">
// 长操作进度面板：阶段文案 + 已用秒数 + 进度条。
//
// 真实进度原则：只有服务端给出 current/total（批量 i/N、Excel 行号）才渲染
// 确定进度条；总量未知时用不确定动画，不编造百分比与剩余时间。已用秒数按
// 服务端 elapsed_ms + 本地 250ms 计时，和轮询节奏无关；超过 60 秒追加安抚
// 文案（首次运行转换程序/文件较大，最长约 3 分钟）。
import { computed, onBeforeUnmount, ref, watch } from "vue";
import type { JobSnapshot } from "../../api/types";

const props = defineProps<{ job: JobSnapshot<unknown> | null }>();

const receivedAt = ref(Date.now());
const now = ref(Date.now());
let timer: ReturnType<typeof setInterval> | undefined;

const running = computed(() => props.job?.status === "running");
const elapsedMS = computed(() => {
  const job = props.job;
  if (!job) {
    return 0;
  }
  if (job.status !== "running") {
    return job.elapsed_ms;
  }
  return job.elapsed_ms + Math.max(0, now.value - receivedAt.value);
});
const seconds = computed(() => Math.max(0, Math.floor(elapsedMS.value / 1000)));
const slow = computed(() => running.value && elapsedMS.value >= 60_000);
const total = computed(() => props.job?.total ?? 0);
const determinate = computed(() => total.value > 0);
const percent = computed(() => {
  const job = props.job;
  if (!job || job.total <= 0) {
    return 0;
  }
  return Math.min(100, Math.max(0, Math.round((job.current / job.total) * 100)));
});
const message = computed(() => {
  const current = props.job?.message ?? "";
  if (current) {
    return current;
  }
  return running.value ? "正在处理…" : "已完成";
});

function startTimer(): void {
  if (timer !== undefined) {
    return;
  }
  timer = setInterval(() => {
    now.value = Date.now();
  }, 250);
}

function stopTimer(): void {
  if (timer === undefined) {
    return;
  }
  clearInterval(timer);
  timer = undefined;
}

watch(
  () => props.job,
  () => {
    receivedAt.value = Date.now();
    now.value = receivedAt.value;
    if (running.value) {
      startTimer();
    } else {
      stopTimer();
    }
  },
  { immediate: true },
);

onBeforeUnmount(stopTimer);
</script>

<template>
  <div
    v-if="job"
    class="job-progress"
    :class="{ running, done: !running }"
    role="status"
    aria-live="polite"
    :data-job-status="job.status"
    :data-job-stage="job.stage"
    :data-job-current="job.current"
    :data-job-total="job.total"
  >
    <div class="job-progress-head">
      <span class="job-progress-message">{{ message }}</span>
      <span class="job-progress-elapsed">已用 {{ seconds }} 秒</span>
    </div>
    <div
      v-if="determinate"
      class="job-progress-bar"
      role="progressbar"
      :aria-valuemin="0"
      :aria-valuemax="job.total"
      :aria-valuenow="job.current"
    >
      <span class="job-progress-fill" :style="{ width: `${percent}%` }"></span>
    </div>
    <div v-else class="job-progress-bar indeterminate" role="progressbar" aria-valuetext="进行中">
      <span class="job-progress-fill"></span>
    </div>
    <p v-if="slow" class="hint job-progress-slow">
      比平时更久，可能首次运行转换程序或文件较大，请再等等（最长约 3 分钟）。
    </p>
  </div>
</template>

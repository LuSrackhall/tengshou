<script setup lang="ts">
// agent 操作可见层（te-h7l.50）：MCP agent 写入时用纯视觉层回放这次操作。
//
// 监听 shell/realtime.ts 已派发的 tengshou:change（detail 含 actor / entity /
// entity_id / action / summary），只处理 actor=tengshou-mcp：
//   切到实体对应的主 tab → 等目标行 [data-id="<entity_id>"] 出现（至多 3 秒）
//   → scrollIntoView → SVG 假光标从顶部徽标滑到行中心（≈600ms）→ 点击波纹
//   → 目标行高亮 2.5 秒。
//
// 视觉层约束：叠加层 pointer-events:none，不抢焦点、不改目标行 DOM 语义
// （只临时加一个高亮类）；不改 shell/realtime.ts 的派发语义。设置页可关闭
// 动画（只留徽标），prefers-reduced-motion:reduce 时跳过光标与波纹、保留
// 行高亮与徽标；document.hidden 时只更新徽标、不排队播放。
import { nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { CHANGE_EVENT } from "../../shell/events";
import {
  AGENT_ACTIVITY_TABS,
  agentActivityAnimationsEnabled,
} from "../../shell/agent-activity";
import { currentTab, switchTab } from "../../shell/tabs";
import { ACTION_LABELS, ENTITY_LABELS, MCP_ACTOR } from "../../ui/format";

interface AgentActivityItem {
  entity: string;
  entityId: string;
  summary: string;
}

const QUEUE_LIMIT = 5;
const ROW_WAIT_MS = 3000;
const ROW_POLL_MS = 100;
const CURSOR_SLIDE_MS = 600;
const RIPPLE_MS = 550;
const HIGHLIGHT_MS = 2500;
const HIGHLIGHT_CLASS = "agent-activity-highlight";
/** 光标热点（SVG 左上角路径起点）相对容器左上角的微调。 */
const CURSOR_OFFSET = -3;

const badgeEl = ref<HTMLElement | null>(null);
const cursorEl = ref<SVGSVGElement | null>(null);
const badgeSummary = ref("");
const badgeVisible = ref(false);
const playing = ref(false);
const ripple = ref<{ key: number; x: number; y: number } | null>(null);
const reducedMotion = ref(false);

const queue: AgentActivityItem[] = [];
const highlighted = new Map<HTMLElement, ReturnType<typeof setTimeout>>();

let motionQuery: MediaQueryList | null = null;
let running = false;
let disposed = false;
let rippleKey = 0;
let rippleTimer: ReturnType<typeof setTimeout> | undefined;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}

function fallbackSummary(entity: string, action: string): string {
  const entityLabel = ENTITY_LABELS[entity] ?? entity;
  const actionLabel = ACTION_LABELS[action] ?? action;
  return `${actionLabel}${entityLabel}`;
}

/** 只收 MCP 写入；其余字段缺失时宁可不播，也不猜目标行。 */
function parseItem(raw: unknown): AgentActivityItem | null {
  if (!isRecord(raw) || raw.actor !== MCP_ACTOR) {
    return null;
  }
  const entity = typeof raw.entity === "string" ? raw.entity : "";
  const entityId = typeof raw.entity_id === "string" ? raw.entity_id : "";
  if (entity === "" || entityId === "") {
    return null;
  }
  const action = typeof raw.action === "string" ? raw.action : "";
  const rawSummary = typeof raw.summary === "string" ? raw.summary.trim() : "";
  return {
    entity,
    entityId,
    summary: rawSummary || fallbackSummary(entity, action),
  };
}

function enqueue(item: AgentActivityItem): void {
  if (agentActivityAnimationsEnabled.value === false) {
    // 关闭动画后只保留徽标；不切页、不排队。
    return;
  }
  if (queue.length >= QUEUE_LIMIT) {
    // 队列上限 5：溢出时把最后一条合并成最新一条（不丢最新操作）。
    queue[QUEUE_LIMIT - 1] = item;
  } else {
    queue.push(item);
  }
  void pump();
}

function onActivityEvent(event: Event): void {
  const item = parseItem((event as CustomEvent<unknown>).detail);
  if (!item) {
    return;
  }
  // 徽标始终更新：即使页面在后台或用户关掉了动画，也留下最近一次 agent 操作。
  badgeSummary.value = item.summary;
  badgeVisible.value = true;
  if (document.hidden) {
    return;
  }
  enqueue(item);
}

async function pump(): Promise<void> {
  if (running) {
    return;
  }
  running = true;
  playing.value = true;
  try {
    while (!disposed && queue.length > 0) {
      const item = queue.shift();
      if (!item) {
        break;
      }
      await play(item);
    }
  } finally {
    running = false;
    playing.value = false;
    hideCursor();
  }
}

async function play(item: AgentActivityItem): Promise<void> {
  if (!agentActivityAnimationsEnabled.value) {
    // 播放途中被设置页关闭：立即停掉剩余队列的视觉回放。
    return;
  }
  const tab = AGENT_ACTIVITY_TABS[item.entity];
  if (!tab) {
    // 没有可定位的行（seal/template/...）：只更新徽标。
    return;
  }
  if (currentTab.value !== tab) {
    switchTab(tab);
  }
  // 等 Vue 把新 tab/hidden 状态刷到 DOM，再测量与滚动，否则会量到零尺寸。
  await nextTick();
  const row = await waitForRow(item.entityId);
  if (!row || disposed) {
    return;
  }
  row.scrollIntoView({ block: "center" });
  if (reducedMotion.value) {
    // 减少动态效果：不移动光标、不出波纹，只保留行高亮。
    highlight(row);
    return;
  }
  await moveCursorTo(row);
  if (disposed) {
    return;
  }
  showRipple(row);
  highlight(row);
  // 让相邻事件的播放与波纹错开，读起来像一次一次点击。
  await delay(RIPPLE_MS);
}

function attributeValue(value: string): string {
  return value.replace(/\\/g, "\\\\").replace(/"/g, '\\"');
}

async function waitForRow(entityId: string): Promise<HTMLElement | null> {
  const selector = `[data-id="${attributeValue(entityId)}"]`;
  const deadline = Date.now() + ROW_WAIT_MS;
  for (;;) {
    const row = document.querySelector<HTMLElement>(selector);
    if (row) {
      return row;
    }
    if (disposed || Date.now() >= deadline) {
      return null;
    }
    await delay(ROW_POLL_MS);
  }
}

function highlight(row: HTMLElement): void {
  row.classList.add(HIGHLIGHT_CLASS);
  const existing = highlighted.get(row);
  if (existing !== undefined) {
    clearTimeout(existing);
  }
  const timer = setTimeout(() => {
    row.classList.remove(HIGHLIGHT_CLASS);
    highlighted.delete(row);
  }, HIGHLIGHT_MS);
  highlighted.set(row, timer);
}

function centerOf(rect: DOMRect): { x: number; y: number } {
  return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 };
}

/**
 * 目标行可能比窗口宽（台账表格横向可滚动），行几何中心会落在视口外。
 * 取行与视口相交区域的中心，光标/波纹才落在用户看得见的位置。
 */
function visiblePointOf(row: HTMLElement): { x: number; y: number } {
  const rect = row.getBoundingClientRect();
  const left = Math.max(rect.left, 0);
  const right = Math.min(rect.right, window.innerWidth);
  const top = Math.max(rect.top, 0);
  const bottom = Math.min(rect.bottom, window.innerHeight);
  if (right <= left || bottom <= top) {
    return centerOf(rect);
  }
  return { x: (left + right) / 2, y: (top + bottom) / 2 };
}

function cursorTransform(x: number, y: number): string {
  return `translate3d(${x + CURSOR_OFFSET}px, ${y + CURSOR_OFFSET}px, 0)`;
}

async function moveCursorTo(row: HTMLElement): Promise<void> {
  const cursor = cursorEl.value;
  if (!cursor) {
    return;
  }
  const target = visiblePointOf(row);
  const start = badgeEl.value
    ? centerOf(badgeEl.value.getBoundingClientRect())
    : { x: window.innerWidth - 80, y: 24 };
  cursor.style.transition = "none";
  cursor.style.transform = cursorTransform(start.x, start.y);
  cursor.style.opacity = "1";
  // 强制一次布局，让起点先落地；否则浏览器会把起点/终点合并成一帧。
  void cursor.getBoundingClientRect();
  cursor.style.transition = `transform ${CURSOR_SLIDE_MS}ms cubic-bezier(.2, .8, .2, 1)`;
  cursor.style.transform = cursorTransform(target.x, target.y);
  await delay(CURSOR_SLIDE_MS);
}

function hideCursor(): void {
  const cursor = cursorEl.value;
  if (!cursor) {
    return;
  }
  cursor.style.transition = "opacity 160ms ease-out";
  cursor.style.opacity = "0";
}

function showRipple(row: HTMLElement): void {
  const point = visiblePointOf(row);
  ripple.value = { key: ++rippleKey, x: point.x, y: point.y };
  if (rippleTimer !== undefined) {
    clearTimeout(rippleTimer);
  }
  rippleTimer = setTimeout(() => {
    ripple.value = null;
    rippleTimer = undefined;
  }, RIPPLE_MS + 100);
}

function onMotionChange(event: MediaQueryListEvent): void {
  reducedMotion.value = event.matches;
}

onMounted(() => {
  motionQuery = window.matchMedia("(prefers-reduced-motion: reduce)");
  reducedMotion.value = motionQuery.matches;
  motionQuery.addEventListener("change", onMotionChange);
  // 直接监听 tengshou:change：bridge/app.ts 的 subscribeChange 会把实体
  // 限制在 CHANGE_ENTITIES 内且不携带 action/summary，这里需要完整 payload。
  window.addEventListener(CHANGE_EVENT, onActivityEvent);
});

onBeforeUnmount(() => {
  disposed = true;
  window.removeEventListener(CHANGE_EVENT, onActivityEvent);
  motionQuery?.removeEventListener("change", onMotionChange);
  motionQuery = null;
  queue.length = 0;
  if (rippleTimer !== undefined) {
    clearTimeout(rippleTimer);
    rippleTimer = undefined;
  }
  for (const timer of highlighted.values()) {
    clearTimeout(timer);
  }
  highlighted.clear();
  document
    .querySelectorAll<HTMLElement>(`.${HIGHLIGHT_CLASS}`)
    .forEach((row) => row.classList.remove(HIGHLIGHT_CLASS));
});
</script>

<template>
  <div class="agent-activity-overlay">
    <div
      v-show="badgeVisible"
      ref="badgeEl"
      class="agent-activity-badge"
      :class="{ playing }"
      role="status"
      aria-live="polite"
      aria-atomic="true"
      title="最近一次 agent（MCP）操作"
    >
      <span class="agent-activity-dot" aria-hidden="true"></span>
      <span>agent 操作</span>
      <span class="agent-activity-summary">{{ badgeSummary }}</span>
    </div>
    <svg
      ref="cursorEl"
      class="agent-activity-cursor"
      width="22"
      height="22"
      viewBox="0 0 24 24"
      aria-hidden="true"
    >
      <path d="M5.5 3.2 19 12l-6.4 1.4L9.4 20z" />
    </svg>
    <span
      v-if="ripple"
      :key="ripple.key"
      class="agent-activity-ripple"
      :style="{ left: `${ripple.x}px`, top: `${ripple.y}px` }"
      aria-hidden="true"
    ></span>
  </div>
</template>

// 实时事件分发：SSE 优先，桌面壳/代理阻断时降级为 3 秒轮询。
//
// 本模块接管迁移前 app.js 的调度部分，但不再维护业务缓存：收到事件后只派发
// tengshou:change 与 tengshou:data-changed，由各 Vue 域自行重取；顶栏统计由
// App.vue 监听同一事件刷新。状态仍写到 window.__tengshouLiveStatus 并派发
// tengshou:live-status，供 LiveStatusBadge 渲染。
//
// 游标与去重规则与迁移前一致：lastEventId 记录最近一条变更 id，seenEventIds
// 最多保留 500 条。

import { eventsUrl, listChangelog } from "../api/endpoints";
import { publishLiveStatus, type LiveStatusMode } from "../bridge/live-status";
import { CHANGE_EVENT, notifyDataChanged } from "./events";
import { currentTab } from "./tabs";

let eventSource: EventSource | null = null;
let pollTimer: ReturnType<typeof setInterval> | null = null;
let retryTimer: ReturnType<typeof setTimeout> | null = null;
let realtimeMode: LiveStatusMode = "connecting";
let lastEventId = "";
const seenEventIds = new Set<string>();

function setLiveStatus(mode: LiveStatusMode): void {
  realtimeMode = mode;
  publishLiveStatus(mode);
}

function requestCatchUpRefresh(): void {
  const entities = ["field", "contract", "party", "seal", "template"];
  if (currentTab.value === "settings") {
    entities.push("settings");
  }
  notifyDataChanged(entities);
}

function connectEventStream(): void {
  if (retryTimer !== null) {
    clearTimeout(retryTimer);
    retryTimer = null;
  }
  if (typeof EventSource === "undefined") {
    startEventPolling();
    return;
  }
  if (eventSource) {
    eventSource.close();
    eventSource = null;
  }
  let source: EventSource;
  try {
    source = new EventSource(eventsUrl(lastEventId === "" ? undefined : lastEventId));
  } catch {
    startEventPolling();
    return;
  }
  eventSource = source;
  source.onopen = () => {
    if (eventSource !== source) {
      return;
    }
    stopEventPolling();
    setLiveStatus("sse");
    // 补齐上次页面加载或断线期间写入的变更。
    requestCatchUpRefresh();
  };
  source.onmessage = (event) => {
    if (eventSource !== source || !event.data) {
      return;
    }
    let payload: unknown;
    try {
      payload = JSON.parse(event.data);
    } catch {
      return;
    }
    handleChangeEvent(payload, event.lastEventId);
  };
  source.onerror = () => {
    if (eventSource !== source) {
      return;
    }
    // EventSource 在 CONNECTING 时会自行重试；轮询并行启动，保证桌面
    // WebView 阻断 SSE 时更新仍能流动。
    if (realtimeMode !== "poll") {
      setLiveStatus("poll");
    }
    startEventPolling();
    if (source.readyState === 2 /* CLOSED */) {
      eventSource = null;
      retryTimer = setTimeout(connectEventStream, 10000);
    }
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

async function pollEventsOnce(): Promise<void> {
  if (document.hidden) {
    return;
  }
  try {
    const data = await listChangelog({
      limit: 200,
      after: lastEventId === "" ? undefined : lastEventId,
    });
    for (const item of data.items) {
      handleChangeEvent(item);
    }
  } catch {
    /* transient: the next tick retries */
  }
}

function startEventPolling(): void {
  if (pollTimer !== null) {
    return;
  }
  void pollEventsOnce();
  pollTimer = setInterval(() => {
    void pollEventsOnce();
  }, 3000);
}

function stopEventPolling(): void {
  if (pollTimer !== null) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
}

function handleChangeEvent(item: unknown, fallbackId = ""): void {
  if (!isRecord(item)) {
    return;
  }
  const id =
    typeof item.id === "string" && item.id !== "" ? item.id : fallbackId;
  if (id === "" || seenEventIds.has(id)) {
    return;
  }
  if (typeof item.id !== "string") {
    item.id = id;
  }
  seenEventIds.add(id);
  if (seenEventIds.size > 500) {
    const oldest = seenEventIds.values().next().value;
    if (oldest !== undefined) {
      seenEventIds.delete(oldest);
    }
  }
  lastEventId = id;
  window.dispatchEvent(new CustomEvent(CHANGE_EVENT, { detail: item }));
  const entity = typeof item.entity === "string" ? item.entity : "";
  notifyDataChanged([entity]);
}

let started = false;

export function startRealtime(): void {
  if (started) {
    return;
  }
  started = true;
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) {
      void pollEventsOnce();
    }
  });
  connectEventStream();
}

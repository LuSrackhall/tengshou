// 顶栏同步状态徽标的状态桥。
//
// app.js 退役后由 shell/realtime.ts 的调度器发布状态：写到
// window.__tengshouLiveStatus 并派发 tengshou:live-status 事件，由 Vue 的
// LiveStatusBadge 渲染。状态档位与迁移前完全一致。

export type LiveStatusMode = "connecting" | "sse" | "poll" | "offline";

export interface LiveStatusState {
  mode: LiveStatusMode;
  /** 覆盖默认 title 的补充说明（可空）。 */
  detail: string;
}

export const LIVE_STATUS_EVENT = "tengshou:live-status";

declare global {
  interface Window {
    /** 最近一次同步状态；徽标挂载时据此补一次当前值。 */
    __tengshouLiveStatus?: Partial<LiveStatusState>;
  }
}

const MODES: readonly LiveStatusMode[] = ["connecting", "sse", "poll", "offline"];

function isLiveStatusMode(value: unknown): value is LiveStatusMode {
  return typeof value === "string" && (MODES as readonly string[]).includes(value);
}

export function normalizeLiveStatus(
  raw: Partial<LiveStatusState> | null | undefined,
): LiveStatusState {
  return {
    mode: isLiveStatusMode(raw?.mode) ? raw.mode : "connecting",
    detail: typeof raw?.detail === "string" ? raw.detail : "",
  };
}

/** 发布一次同步状态：写 window 并派发事件，供徽标与开发预览使用。 */
export function publishLiveStatus(mode: LiveStatusMode, detail = ""): void {
  const state: LiveStatusState = { mode, detail };
  window.__tengshouLiveStatus = state;
  window.dispatchEvent(
    new CustomEvent<LiveStatusState>(LIVE_STATUS_EVENT, { detail: state }),
  );
}

/**
 * 订阅状态桥：订阅时立即回调一次当前状态（没有事件时回落到 connecting），
 * 返回取消订阅函数。
 */
export function subscribeLiveStatus(
  handler: (state: LiveStatusState) => void,
): () => void {
  const listener = (event: Event): void => {
    const detail = (event as CustomEvent<Partial<LiveStatusState> | undefined>).detail;
    handler(normalizeLiveStatus(detail));
  };
  window.addEventListener(LIVE_STATUS_EVENT, listener);
  handler(normalizeLiveStatus(window.__tengshouLiveStatus));
  return () => window.removeEventListener(LIVE_STATUS_EVENT, listener);
}

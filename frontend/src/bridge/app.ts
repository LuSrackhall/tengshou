// app.js ↔ Vue 迁移期桥（仅相对方域需要）。
//
// 相对方域的跨域动作只有一个：借用合同表单（「以此档案新建合同」与历史
// 合同的查看/续签），另外写入后需要触发 app.js 的整体刷新。app.js 在文件
// 末尾设置 window.__tengshouApp；Vue 侧统一从这里调用，组件不直接读 window。
//
// 实时刷新：app.js 收到 SSE / 轮询事件或本地写入完成后派发两个 window 事件：
//   - tengshou:change       一条变更（detail 是 appEvent 的紧凑子集）
//   - tengshou:data-changed 需要重取的实体列表（detail.entities）
// 迁移期新旧入口都靠它们保持自动刷新；退役时随 app.js 一起删除。

import type { ChangeEntity, Contract } from "../api/types";

export interface PartyRef {
  id: string;
  name: string;
}

export interface ContractFormOptions {
  partyB?: PartyRef | null;
  renew?: boolean;
  focusGenerate?: boolean;
}

export interface AppBridge {
  openContractForm(
    contract: Contract | null,
    options?: ContractFormOptions,
  ): Promise<void> | void;
  toast(message: string, isError?: boolean): void;
  refreshAfterWrite(): Promise<void>;
}

declare global {
  interface Window {
    /** 存量 app.js 在文件末尾设置；迁移完成后移除。 */
    __tengshouApp?: AppBridge;
  }
}

export const DATA_CHANGED_EVENT = "tengshou:data-changed";
export const CHANGE_EVENT = "tengshou:change";

export interface DataChangedDetail {
  entities: string[];
}

/** 本域关心的变更通知（appEvent 的紧凑子集）。 */
export interface AppChangeNotice {
  entity: ChangeEntity;
  entity_id: string;
  actor: string;
}

const CHANGE_ENTITIES: readonly ChangeEntity[] = [
  "contract",
  "party",
  "field",
  "seal",
  "template",
  "signature",
  "backup",
  "settings",
  "import",
];

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

export function getAppBridge(): AppBridge | null {
  return window.__tengshouApp ?? null;
}

/** 统一错误文案：Error 取 message，其余转字符串。 */
export function errorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}

/** 走 app.js 的顶栏 toast；桥缺席（开发预览页）时静默。 */
export function toast(message: string, isError = false): void {
  window.__tengshouApp?.toast(message, isError);
}

/** 本地写入后让 app.js 重载自己的缓存并派发 data-changed。 */
export async function refreshAfterWrite(): Promise<void> {
  await window.__tengshouApp?.refreshAfterWrite();
}

/** 「以此档案新建合同」：REUSE-7 语义不变，仍由 app.js 打开合同表单。 */
export function openContractFormForParty(party: PartyRef): void {
  const bridge = getAppBridge();
  if (!bridge) {
    return;
  }
  void Promise.resolve(
    bridge.openContractForm(null, { partyB: { id: party.id, name: party.name } }),
  );
}

/** 历史合同的「查看 / 续签」：关闭相对方弹窗后由 app.js 打开合同表单。 */
export function openContractFormForHistory(contract: Contract, renew: boolean): void {
  const bridge = getAppBridge();
  if (!bridge) {
    return;
  }
  Promise.resolve(bridge.openContractForm(contract, { renew })).catch((error: unknown) => {
    bridge.toast(errorMessage(error), true);
  });
}

function normalizeDataChanged(raw: unknown): DataChangedDetail {
  if (!isRecord(raw)) {
    return { entities: [] };
  }
  const entities = Array.isArray(raw.entities)
    ? raw.entities.filter((value): value is string => typeof value === "string")
    : [];
  return { entities };
}

/** 订阅 app.js 的「需要重取」通知；返回取消订阅函数。 */
export function subscribeDataChanged(
  handler: (detail: DataChangedDetail) => void,
): () => void {
  const listener = (event: Event): void => {
    handler(normalizeDataChanged((event as CustomEvent<unknown>).detail));
  };
  window.addEventListener(DATA_CHANGED_EVENT, listener);
  return () => window.removeEventListener(DATA_CHANGED_EVENT, listener);
}

function normalizeChange(raw: unknown): AppChangeNotice | null {
  if (!isRecord(raw)) {
    return null;
  }
  const { entity, entity_id: entityId, actor } = raw;
  if (typeof entity !== "string" || typeof entityId !== "string" || typeof actor !== "string") {
    return null;
  }
  if (!(CHANGE_ENTITIES as readonly string[]).includes(entity)) {
    return null;
  }
  return { entity: entity as ChangeEntity, entity_id: entityId, actor };
}

/** 订阅单条变更通知；返回取消订阅函数。 */
export function subscribeChange(
  handler: (notice: AppChangeNotice) => void,
): () => void {
  const listener = (event: Event): void => {
    const notice = normalizeChange((event as CustomEvent<unknown>).detail);
    if (notice) {
      handler(notice);
    }
  };
  window.addEventListener(CHANGE_EVENT, listener);
  return () => window.removeEventListener(CHANGE_EVENT, listener);
}

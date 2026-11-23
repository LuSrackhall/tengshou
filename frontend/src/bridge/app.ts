// Vue 域之间的跨域桥（app.js 退役后由 Vue 壳自身提供这些能力）。
//
// 跨域动作：
//   - 相对方 → 合同表单：「以此档案新建合同」与历史合同的查看/续签，经
//     tengshou:open-contract-form 由台账岛打开表单；
//   - 新手引导 → 模板导入/标定编辑器/合同表单，经 tengshou:open-* 事件；
//   - 模板页/台账 → 批量生成，经 tengshou:open-batch-generate 由台账岛渲染；
//   - 台账「导入 Excel」→ tengshou:open-excel-import，由外壳的导入宿主弹出
//     文件选择并渲染 Vue 导入对话框。
//
// 公共能力：
//   - toast() 走 ui/toast.ts 的 ToastHost；
//   - refreshAfterWrite() 派发 tengshou:data-changed，让各域重取数据；
//   - switchTab() 走 shell/tabs.ts，保持激活态与 tab-change 事件一致。
//
// 实时刷新：shell/realtime.ts 收到 SSE / 轮询事件或本地写入完成后派发两个
// window 事件：
//   - tengshou:change       一条变更（detail 是 appEvent 的紧凑子集）
//   - tengshou:data-changed 需要重取的实体列表（detail.entities）

import { showToast } from "../ui/toast";
import {
  CHANGE_EVENT,
  DATA_CHANGED_EVENT,
  OPEN_BATCH_GENERATE_EVENT,
  OPEN_CONTRACT_FORM_EVENT,
  OPEN_EXCEL_IMPORT_EVENT,
  OPEN_TEMPLATE_EDITOR_EVENT,
  OPEN_TEMPLATE_IMPORT_EVENT,
  TAB_CHANGE_EVENT,
  notifyDataChanged,
  type DataChangedDetail,
} from "../shell/events";
import { switchTab as switchTabImpl } from "../shell/tabs";
import { CHANGE_ENTITIES } from "../api/types";
import type {
  ChangeEntity,
  Contract,
  DocumentKind,
} from "../api/types";

export interface PartyRef {
  id: string;
  name: string;
}

export interface ContractFormOptions {
  partyB?: PartyRef | null;
  renew?: boolean;
  focusGenerate?: boolean;
}

/** 批量生成对话框：合同列表来自 Vue 当前筛选结果（迁移前是 state.contracts）。 */
export interface BatchGenerateOptions {
  title?: string;
  contracts: Contract[];
  templateId?: string;
  /** regenerate 用于模板整改重出；空串表示由服务端判断。 */
  kind?: DocumentKind | "";
}

/** app.js → Vue：台账岛注册的只读视图（模板页批量生成按钮使用）。 */
export interface LedgerBridge {
  currentContracts(): Contract[];
}

declare global {
  interface Window {
    /** 合同台账岛在挂载期间注册；模板页批量生成读取当前台账。 */
    __tengshouContracts?: LedgerBridge;
  }
}

export interface OpenContractFormDetail {
  contract: Contract | null;
  options?: ContractFormOptions;
}

/** 引导步骤顺带触发的样例动作（等价于迁移前 openTemplateEditor + 预览）。 */
export type TemplateEditorAction = "sample-preview" | "download-sample";

export interface OpenTemplateEditorDetail {
  templateId: string;
  action?: TemplateEditorAction;
}

/** 本域关心的变更通知（appEvent 的紧凑子集）。 */
export interface AppChangeNotice {
  entity: ChangeEntity;
  entity_id: string;
  actor: string;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

/** 统一错误文案：Error 取 message，其余转字符串。 */
export function errorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}

/** 顶栏 toast（公共实现见 ui/toast.ts）。 */
export function toast(message: string, isError = false): void {
  showToast(message, isError);
}

/** 本地写入后让各域重取自己的数据；与迁移前 refreshAfterWrite 的实体集一致。 */
export async function refreshAfterWrite(): Promise<void> {
  notifyDataChanged(["field", "party", "contract", "seal", "template"]);
}

/** 切换主 tab：更新壳的激活态并派发 tab-change。 */
export function switchTab(tab: string): void {
  switchTabImpl(tab);
}

/** 打开合同表单（新建/编辑/续签）：由台账岛监听事件后打开 Vue 弹窗。 */
export function openContractForm(
  contract: Contract | null,
  options: ContractFormOptions = {},
): void {
  window.dispatchEvent(
    new CustomEvent<OpenContractFormDetail>(OPEN_CONTRACT_FORM_EVENT, {
      detail: { contract, options },
    }),
  );
}

/** 「以此档案新建合同」：REUSE-7 语义不变，仍打开合同表单（乙方预填）。 */
export function openContractFormForParty(party: PartyRef): void {
  openContractForm(null, { partyB: { id: party.id, name: party.name } });
}

/** 历史合同的「查看 / 续签」：打开合同表单并可选续签草稿。 */
export function openContractFormForHistory(contract: Contract, renew: boolean): void {
  openContractForm(contract, { renew });
}

/** 打开批量生成对话框：由合同台账岛监听事件后渲染。 */
export function openBatchGenerateDialog(options: BatchGenerateOptions): void {
  window.dispatchEvent(
    new CustomEvent<BatchGenerateOptions>(OPEN_BATCH_GENERATE_EVENT, {
      detail: options,
    }),
  );
}

function normalizeBatchGenerate(raw: unknown): BatchGenerateOptions | null {
  if (!isRecord(raw) || !Array.isArray(raw.contracts)) {
    return null;
  }
  const contracts = raw.contracts
    .filter(isRecord)
    .map((item) => item as unknown as Contract);
  const kind =
    raw.kind === "initial" || raw.kind === "regenerate" ? raw.kind : undefined;
  return {
    contracts,
    title: typeof raw.title === "string" ? raw.title : undefined,
    templateId: typeof raw.templateId === "string" ? raw.templateId : undefined,
    kind,
  };
}

/** 订阅「打开批量生成」事件；返回取消订阅函数。 */
export function subscribeOpenBatchGenerate(
  handler: (options: BatchGenerateOptions) => void,
): () => void {
  const listener = (event: Event): void => {
    const options = normalizeBatchGenerate((event as CustomEvent<unknown>).detail);
    if (options) {
      handler(options);
    }
  };
  window.addEventListener(OPEN_BATCH_GENERATE_EVENT, listener);
  return () => window.removeEventListener(OPEN_BATCH_GENERATE_EVENT, listener);
}

/** 触发外壳的 Excel 导入文件选择（对话框由 Vue 渲染）。 */
export function openExcelImport(): void {
  window.dispatchEvent(new CustomEvent(OPEN_EXCEL_IMPORT_EVENT));
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

/** 订阅「需要重取」通知；返回取消订阅函数。 */
export function subscribeDataChanged(
  handler: (detail: DataChangedDetail) => void,
): () => void {
  const listener = (event: Event): void => {
    handler(normalizeDataChanged((event as CustomEvent<unknown>).detail));
  };
  window.addEventListener(DATA_CHANGED_EVENT, listener);
  return () => window.removeEventListener(DATA_CHANGED_EVENT, listener);
}

/** 订阅主 tab 切换（detail.tab）；返回取消订阅函数。 */
export function subscribeTabChanged(
  handler: (tab: string) => void,
): () => void {
  const listener = (event: Event): void => {
    const raw = (event as CustomEvent<unknown>).detail;
    if (!isRecord(raw) || typeof raw.tab !== "string" || raw.tab === "") {
      return;
    }
    handler(raw.tab);
  };
  window.addEventListener(TAB_CHANGE_EVENT, listener);
  return () => window.removeEventListener(TAB_CHANGE_EVENT, listener);
}

/** 订阅「打开合同表单」事件；返回取消订阅函数。 */
export function subscribeOpenContractForm(
  handler: (detail: OpenContractFormDetail) => void,
): () => void {
  const listener = (event: Event): void => {
    const raw = (event as CustomEvent<unknown>).detail;
    if (!isRecord(raw)) {
      handler({ contract: null });
      return;
    }
    const contract = isRecord(raw.contract)
      ? (raw.contract as unknown as Contract)
      : null;
    const options = isRecord(raw.options)
      ? (raw.options as unknown as ContractFormOptions)
      : undefined;
    handler({ contract, options });
  };
  window.addEventListener(OPEN_CONTRACT_FORM_EVENT, listener);
  return () => window.removeEventListener(OPEN_CONTRACT_FORM_EVENT, listener);
}

/** 打开模板标定编辑器：由模板岛监听事件后打开 Vue 全屏弹窗。 */
export function openTemplateEditor(detail: OpenTemplateEditorDetail): void {
  window.dispatchEvent(
    new CustomEvent<OpenTemplateEditorDetail>(OPEN_TEMPLATE_EDITOR_EVENT, {
      detail,
    }),
  );
}

/** 打开模板导入文件选择；导入弹窗由模板岛渲染。 */
export function openTemplateImport(): void {
  window.dispatchEvent(new CustomEvent(OPEN_TEMPLATE_IMPORT_EVENT));
}

function normalizeOpenTemplateEditor(raw: unknown): OpenTemplateEditorDetail | null {
  if (!isRecord(raw) || typeof raw.templateId !== "string" || raw.templateId === "") {
    return null;
  }
  const action =
    raw.action === "sample-preview" || raw.action === "download-sample"
      ? raw.action
      : undefined;
  return { templateId: raw.templateId, action };
}

/** 订阅「打开模板标定编辑器」事件；返回取消订阅函数。 */
export function subscribeOpenTemplateEditor(
  handler: (detail: OpenTemplateEditorDetail) => void,
): () => void {
  const listener = (event: Event): void => {
    const detail = normalizeOpenTemplateEditor(
      (event as CustomEvent<unknown>).detail,
    );
    if (detail) {
      handler(detail);
    }
  };
  window.addEventListener(OPEN_TEMPLATE_EDITOR_EVENT, listener);
  return () => window.removeEventListener(OPEN_TEMPLATE_EDITOR_EVENT, listener);
}

/** 订阅「打开模板导入」事件；返回取消订阅函数。 */
export function subscribeOpenTemplateImport(handler: () => void): () => void {
  const listener = (): void => handler();
  window.addEventListener(OPEN_TEMPLATE_IMPORT_EVENT, listener);
  return () => window.removeEventListener(OPEN_TEMPLATE_IMPORT_EVENT, listener);
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

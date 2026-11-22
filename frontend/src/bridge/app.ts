// app.js ↔ Vue 迁移期桥（相对方、合同与公章域共用）。
//
// 相对方域的跨域动作只有一个：借用合同表单（「以此档案新建合同」与历史
// 合同的查看/续签），另外写入后需要触发 app.js 的整体刷新。合同表单已迁到
// Vue（components/contracts/），因此这些入口改为派发
// tengshou:open-contract-form，由台账岛打开表单；app.js 的引导步骤同样经该
// 事件打开新建表单。
//
// 反向调用（Vue → app.js）：toast、整体刷新、扫码签名、批量生成、Excel 导入
// 与合同详情里的签名摘要仍由 app.js 实现，统一经 window.__tengshouApp 调用；
// 组件不直接读 window。合同详情里的生成面板已迁到 Vue
// （components/contracts/ContractGenerationPanel.vue）。
//
// 实时刷新：app.js 收到 SSE / 轮询事件或本地写入完成后派发两个 window 事件：
//   - tengshou:change       一条变更（detail 是 appEvent 的紧凑子集）
//   - tengshou:data-changed 需要重取的实体列表（detail.entities）
// 迁移期新旧入口都靠它们保持自动刷新；退役时随 app.js 一起删除。

import type {
  ChangeEntity,
  Contract,
  ContractDetail,
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

/** 台账批量生成：合同列表来自 Vue 当前筛选结果（迁移前是 state.contracts）。 */
export interface BatchGenerateOptions {
  title?: string;
  contracts: Contract[];
  templateId?: string;
  kind?: string;
}

export interface AppBridge {
  toast(message: string, isError?: boolean): void;
  refreshAfterWrite(): Promise<void>;
  /** 扫码签名对话框仍由 app.js 渲染。 */
  openSignatureDialog(contract: Contract): void;
  /** 批量生成对话框仍由 app.js 渲染；失败会自行 toast。 */
  openBatchGenerateDialog(options: BatchGenerateOptions): Promise<unknown> | void;
  /** 触发 app.js 的隐藏文件输入，导入 Excel 对话框仍归 app.js。 */
  openExcelImport(): void;
  /**
   * 合同详情里的签名摘要仍由 app.js 渲染（扫码签名域尚未迁移）：
   * 传入 Vue 弹窗的根节点，app.js 在其中查找宿主元素并填充。
   */
  mountContractSignatures(root: Element, detail: ContractDetail): void;
}

/** app.js → Vue：台账岛注册的只读视图（模板页批量生成按钮使用）。 */
export interface LedgerBridge {
  currentContracts(): Contract[];
}

declare global {
  interface Window {
    /** 存量 app.js 在文件末尾设置；迁移完成后移除。 */
    __tengshouApp?: AppBridge;
    /** 合同台账岛在挂载期间注册；模板页批量生成读取当前台账。 */
    __tengshouContracts?: LedgerBridge;
  }
}

export const DATA_CHANGED_EVENT = "tengshou:data-changed";
export const CHANGE_EVENT = "tengshou:change";
export const OPEN_CONTRACT_FORM_EVENT = "tengshou:open-contract-form";
// 模板域（阶段 1 迁出 app.js）：引导与引导自检仍由 app.js 渲染，需要打开
// 模板标定编辑器或导入流程；模板岛监听这两个事件后接管。
export const OPEN_TEMPLATE_EDITOR_EVENT = "tengshou:open-template-editor";
export const OPEN_TEMPLATE_IMPORT_EVENT = "tengshou:open-template-import";

export interface DataChangedDetail {
  entities: string[];
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

/** 扫码签名对话框（app.js）。 */
export function openSignatureDialog(contract: Contract): void {
  const bridge = getAppBridge();
  if (!bridge) {
    return;
  }
  bridge.openSignatureDialog(contract);
}

/** 批量生成对话框（app.js）；失败统一经 toast 呈现。 */
export function openBatchGenerateDialog(options: BatchGenerateOptions): void {
  const bridge = getAppBridge();
  if (!bridge) {
    return;
  }
  void Promise.resolve(bridge.openBatchGenerateDialog(options)).catch((error: unknown) => {
    toast(errorMessage(error), true);
  });
}

/** 触发 app.js 的 Excel 导入文件选择（对话框仍归 app.js）。 */
export function openExcelImport(): void {
  getAppBridge()?.openExcelImport();
}

/** 把合同详情里的签名摘要宿主交给 app.js 渲染。 */
export function mountContractSignatures(root: Element, detail: ContractDetail): void {
  getAppBridge()?.mountContractSignatures(root, detail);
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

// 模板标定编辑器内部状态类型（Vue 组件之间共享）。
import type {
  OutputDocument,
  OverlayWarning,
  Placement,
  TemplateCheckIssue,
} from "../../api/types";

/** 模板体检面板的展示状态。 */
export type CheckView =
  | { kind: "loading" }
  | { kind: "ok" }
  | { kind: "issues"; issues: TemplateCheckIssue[] }
  | { kind: "error"; text: string };

/** 最近一次预览（样例或真实合同），保存/改动后清空。 */
export interface PreviewState {
  sample: boolean;
  contractId: string;
  sha: string;
  bytes: Uint8Array;
}

/** 预览层的下载按钮配置。 */
export interface PreviewDownload {
  /** 内存中的 PDF（样例预览）。 */
  bytes?: Uint8Array;
  /** 已生成的输出文档 id（查接口下载）。 */
  documentId?: string;
  filename: string;
  /** true 表示样例试生成，下载后记录引导步骤并提示不写入台账。 */
  sample: boolean;
}

/** 预览层（样例预览 / 合同预览 / 生成结果）的展示状态。 */
export interface PreviewOverlayState {
  bytes: Uint8Array;
  warnings: OverlayWarning[];
  title: string;
  download: PreviewDownload | null;
}

/** 生成成功的展示信息。 */
export interface GeneratedResult {
  document: OutputDocument;
  warnings: OverlayWarning[];
}

/** 「添加位置」表单提交的值；校验在编辑器里完成。 */
export interface AddPlacementPayload {
  kind: Placement["type"];
  /** 新框所在页码（1 起），由「新增位置」表单的页码选择给出。 */
  page: number;
  field: string;
  party: string;
  sealMode: string;
  sealId: string;
}

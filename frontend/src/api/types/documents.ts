// 证件输出与生成流程契约：签名资产、输出 PDF、生成前检查、批量生成。
//
// 来源（与 Go 侧逐字段核对）：
//   internal/model/model.go          Signature / OutputDocument / DocumentSignatureSnapshot
//   internal/overlay/overlay.go      Warning
//   internal/service/documents.go    PreflightIssue / PreflightResult /
//                                    BatchPreviewSummary / BatchPreviewResult /
//                                    BatchGenerateItemResult / BatchGenerateSummary /
//                                    BatchGenerateResult
//   internal/httpapi/templateapi.go  preview / preflight / batch 响应包装

import type { ISODateTime } from "./common";
import type { Template } from "./records";

// ---------- 签名资产 ----------

/** 签名来源；目前只有局域网二维码采集。 */
export type SignatureSource = "lan_qr" | "archive";

/** 留存的签名图片及元数据（model.Signature）。 */
export interface Signature {
  id: string;
  party_id?: string;
  contract_id?: string;
  signer_name: string;
  /** 相对数据目录的图片路径。 */
  asset_path: string;
  sha256: string;
  /** 采集时合同内容的摘要；用于判断签名是否针对当前内容。 */
  content_sha256?: string;
  source?: SignatureSource;
  session_id?: string;
  width_px: number;
  height_px: number;
  captured_at: ISODateTime;
  created_at: ISODateTime;
}

// ---------- 输出 PDF ----------

export type DocumentKind = "initial" | "regenerate";
export type DocumentStatus = "success" | "failed";

/** 一次生成使用的签名快照（model.DocumentSignatureSnapshot）。 */
export interface DocumentSignatureSnapshot {
  signature_id: string;
  party_id?: string;
  signer_name?: string;
  sha256: string;
  content_sha256?: string;
}

/** 一次 PDF 生成尝试（model.OutputDocument）。 */
export interface OutputDocument {
  id: string;
  contract_id?: string;
  template_id?: string;
  template_version: number;
  /** 相对数据目录的输出文件路径；失败记录为空。 */
  file_path: string;
  sha256: string;
  page_count: number;
  content_sha256?: string;
  signatures: DocumentSignatureSnapshot[];
  operator?: string;
  kind: DocumentKind;
  status: DocumentStatus;
  error_message?: string;
  batch_id?: string;
  source_document_id?: string;
  created_at: ISODateTime;
}

// ---------- 生成前检查 ----------

/** overlay.Warning：模板标定或渲染时的非阻断提示。 */
export interface OverlayWarning {
  code: string;
  placement_id?: string;
  message: string;
}

export type PreflightStatus =
  | "ready"
  | "needs_signature"
  | "needs_resign"
  | "missing_fields"
  | "missing_party"
  | "missing_seal"
  | "blocked";

export interface PreflightIssue {
  code: string;
  message: string;
}

/** 单份合同的生成就绪报告（service.PreflightResult）。 */
export interface PreflightResult {
  contract_id: string;
  contract_no: string;
  title: string;
  template_id: string;
  template_name?: string;
  template_version: number;
  content_sha256?: string;
  /** 生成请求必须回传的预览摘要。 */
  preview_sha256?: string;
  status: PreflightStatus;
  ready: boolean;
  issues: PreflightIssue[];
  warnings: OverlayWarning[];
  signatures: DocumentSignatureSnapshot[];
  previous_documents: number;
  pinned_template_id?: string;
  pinned_template_name?: string;
  pinned_template_version?: number;
}

// ---------- 批量生成 ----------

export interface BatchPreviewSummary {
  total: number;
  ready: number;
  blocked: number;
}

/** 批量预览响应（service.BatchPreviewResult 的 HTTP 负载）。 */
export interface BatchPreviewResponse {
  template: Template;
  items: PreflightResult[];
  summary: BatchPreviewSummary;
  sample_preview_sha256?: string;
  warnings: OverlayWarning[];
  /** 仅 sample=true 时返回，样例数据 PDF 的 base64。 */
  pdf_base64?: string;
}

export interface BatchGenerateItemResult {
  contract_id: string;
  contract_no?: string;
  title?: string;
  success: boolean;
  document?: OutputDocument;
  error_code?: string;
  error_message?: string;
  warnings?: OverlayWarning[];
}

export interface BatchGenerateSummary {
  total: number;
  success: number;
  failed: number;
}

/** 批量生成结果；失败按合同逐条返回。 */
export interface BatchGenerateResult {
  batch_id: string;
  template_id: string;
  template_name?: string;
  template_version: number;
  items: BatchGenerateItemResult[];
  summary: BatchGenerateSummary;
}

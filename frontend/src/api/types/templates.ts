// 模板管理与生成响应的契约（HTTP 包装层）。
//
// 来源（与 Go 侧逐字段核对）：
//   internal/service/onboarding.go   TemplateCheckIssue / TemplateCheck
//   internal/service/templates.go    TemplateImportResult / TemplatePreviewResult /
//                                    TemplateRenderResult
//   internal/doctopdf/convert.go     Availability（转换程序可用性）
//   internal/httpapi/templateapi.go  import / mapping / preview / generate 的响应包装

import type { CheckStatus } from "./common";
import type { OutputDocument, OverlayWarning } from "./documents";
import type { Template, TemplateUsage } from "./records";

// ---------- 模板占位检查 ----------

/** 模板检查问题的严重程度：error 阻断试生成，warn 仅提示。 */
export type TemplateCheckSeverity = Extract<CheckStatus, "warn" | "error">;

export interface TemplateCheckIssue {
  code: string;
  severity: TemplateCheckSeverity;
  placement_id?: string;
  field?: string;
  message: string;
  action: string;
}

/** PDF 模板标定结果与当前字段目录的只读校验（service.TemplateCheck）。 */
export interface TemplateCheck {
  template_id: string;
  name: string;
  version: number;
  placement_count: number;
  field_placements: number;
  signature_placements: number;
  seal_placements: number;
  issues: TemplateCheckIssue[];
  ok: boolean;
}

// ---------- 导入 / 保存模板 ----------

/**
 * 导入模板或保存标定后的返回（TemplateImportResult 的 HTTP 包装）。
 * warnings 是面向用户的文本提示（例如转换器降级、字段缺失）。
 */
export interface TemplateMutationResponse {
  item: Template;
  usage: TemplateUsage;
  warnings: string[];
}

// ---------- 预览 ----------

/** 预览渲染结果；PDF 本体以 base64 返回，不落库。 */
export interface TemplatePreviewResponse {
  pdf_base64: string;
  preview_sha256: string;
  warnings: OverlayWarning[];
  page_count: number;
  sample: boolean;
  /** 样例预览时为空串。 */
  contract_id: string;
}

// ---------- 生成 ----------

/** 单份生成成功后的返回（TemplateRenderResult 的 HTTP 包装）。 */
export interface TemplateGenerateResponse {
  item: OutputDocument;
  usage: TemplateUsage;
  warnings: OverlayWarning[];
}

// ---------- 转换程序 ----------

/** 一种 Word→PDF 转换程序的可用性（doctopdf.Availability）。 */
export interface ConverterAvailability {
  name: string;
  label: string;
  available: boolean;
  detail?: string;
  hint?: string;
}

/** GET /converters 的响应包装。 */
export interface ConvertersResponse {
  items: ConverterAvailability[];
}

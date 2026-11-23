// Excel 导入契约：预览、逐行动作、冲突决议与提交结果。
//
// 来源（与 Go 侧逐字段核对）：
//   internal/service/excel.go     ImportOptions / ImportColumn / ImportRow /
//                                 ImportSummary / ImportPreview / ImportResult
//   internal/httpapi/appapi.go    importRequest、excel preview/commit 响应包装

import type { ApiFieldError } from "./common";
import type { FieldDefinition } from "./records";

/** upsert：同编号覆盖更新；create：同编号视为错误。 */
export type ImportMode = "upsert" | "create";

/**
 * 一行预览/提交结果的动作：
 *   create / update / unchanged / error  Excel 数据行；
 *   delete                               导出快照里有、文件里缺失的行；
 *   conflict                             需要 keep_system / use_excel 二选一。
 */
export type ImportRowAction =
  | "create"
  | "update"
  | "unchanged"
  | "error"
  | "delete"
  | "conflict";

/** 冲突行的逐行决议。 */
export type ImportResolution = "keep_system" | "use_excel";

/** 预览与提交共用的选项（service.ImportOptions）。 */
export interface ImportOptions {
  /** 列号（字符串）→ 字段 key；空表示由表头自动推断。 */
  mapping?: Record<string, string>;
  mode?: ImportMode;
  /** 跳过错误行，只导入合法行。 */
  skip_errors?: boolean;
  /** 未知相对方是否自动建档；默认 true。 */
  create_parties?: boolean;
  /** 导入审计里记录的文件名。 */
  file_name?: string;
  /** 是否应用快照里缺失的删除行；默认 false。 */
  apply_deletes?: boolean;
  /** 预览行 key → keep_system / use_excel；冲突行必须逐行选择。 */
  resolutions?: Record<string, ImportResolution>;
}

/** Excel 预览/提交请求体（httpapi.importRequest）。 */
export interface ExcelImportRequest {
  /** 文件内容的 base64（可带 data: 前缀）。 */
  file_base64: string;
  options: ImportOptions;
}

/** 一个表头列及解析到的字段（service.ImportColumn）。 */
export interface ImportColumn {
  index: number;
  header: string;
  field?: string;
  label?: string;
  type?: string;
  required: boolean;
}

/** 预览或提交里的一行（service.ImportRow）。 */
export interface ImportRow {
  /** 稳定的决议键：文件行为行号，删除候选为 "delete:<contract_id>"。 */
  key: string;
  /** 1 起的 Excel 行号；删除候选为 0。 */
  row: number;
  contract_id?: string;
  action: ImportRowAction;
  contract_no: string;
  title: string;
  party_b: string;
  /** 冲突行显示的差异单元格。 */
  system_values?: Record<string, string>;
  excel_values?: Record<string, string>;
  conflict_reason?: string;
  errors: ApiFieldError[];
  warnings: string[];
}

/** 预览动作计数（service.ImportSummary）。 */
export interface ImportSummary {
  total: number;
  create: number;
  update: number;
  unchanged: number;
  error: number;
  deleted: number;
  conflicts: number;
}

/** 无副作用的解析结果（service.ImportPreview）。 */
export interface ImportPreview {
  sheet: string;
  columns: ImportColumn[];
  mapping: Record<string, string>;
  rows: ImportRow[];
  summary: ImportSummary;
  fields: FieldDefinition[];
  notes: string[];
}

/** 提交结果（service.ImportResult）。 */
export interface ImportResult {
  created: number;
  updated: number;
  unchanged: number;
  errors: number;
  deleted: number;
  conflicts: number;
  skipped: number;
  rows: ImportRow[];
}

/**
 * 提交遇到 import_errors 时，422 响应体同时带有 error 与完整 result，
 * 便于界面展示逐行错误；此时 ApiError.payload 即该结构。
 */
export interface ImportCommitErrorPayload {
  error: {
    code: string;
    message: string;
  };
  result: ImportResult;
}

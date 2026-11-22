// Excel 导入 / 导出台账端点。
//
//   GET  /excel/template、/excel/ledger
//   POST /excel/preview、/excel/commit

import {
  apiUrl,
  getFile,
  post,
  type DownloadedFile,
  type RequestOptions,
} from "../client";
import type {
  ExcelImportRequest,
  ExcelLedgerQuery,
  ImportPreview,
  ImportResult,
} from "../types";

/** GET /excel/template：空白录入模板的下载地址。 */
export function excelTemplateUrl(): string {
  return apiUrl("/excel/template");
}

/** GET /excel/ledger：台账导出的下载地址（可按年度/状态/关键词筛选）。 */
export function excelLedgerUrl(query: ExcelLedgerQuery = {}): string {
  return apiUrl("/excel/ledger", query);
}

/** 下载空白录入模板（xlsx）。 */
export function downloadExcelTemplate(
  options: RequestOptions = {},
): Promise<DownloadedFile> {
  return getFile("/excel/template", options);
}

/** 下载台账（xlsx）。 */
export function downloadExcelLedger(
  query: ExcelLedgerQuery = {},
  options: RequestOptions = {},
): Promise<DownloadedFile> {
  return getFile("/excel/ledger", { ...options, query });
}

/** POST /excel/preview：解析并校验，不写库。 */
export function previewExcelImport(
  input: ExcelImportRequest,
  options: RequestOptions = {},
): Promise<ImportPreview> {
  return post("/excel/preview", input, options);
}

/** POST /excel/commit：确认导入；逐行错误见 ApiError.payload。 */
export function commitExcelImport(
  input: ExcelImportRequest,
  options: RequestOptions = {},
): Promise<ImportResult> {
  return post("/excel/commit", input, options);
}

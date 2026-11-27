// 长操作后台任务端点（同步端点的 job 化版本）。
//
//   POST /jobs/templates/import
//   POST /jobs/contracts/{id}/preview、/jobs/contracts/{id}/generate
//   POST /jobs/templates/{id}/batch-preview、/jobs/templates/{id}/batch-generate
//   POST /jobs/excel/preview、/jobs/excel/commit
//   GET  /jobs/{id}

import {
  encodePathSegment,
  get,
  post,
  type RequestOptions,
} from "../client";
import type {
  BatchGenerateInput,
  BatchGenerateResult,
  BatchPreviewInput,
  BatchPreviewResponse,
  ContractGenerateJobInput,
  ContractPreviewJobInput,
  ExcelImportRequest,
  ImportPreview,
  ImportResult,
  JobSnapshot,
  JobStartResponse,
  TemplateGenerateResponse,
  TemplateImportInput,
  TemplateMutationResponse,
  TemplatePreviewResponse,
} from "../types";

const enc = encodePathSegment;

/** POST /jobs/templates/import：模板导入（保存 → 转换 → 解析）。 */
export function startTemplateImportJob(
  input: TemplateImportInput,
  options: RequestOptions = {},
): Promise<JobStartResponse> {
  return post("/jobs/templates/import", input, options);
}

/** POST /jobs/contracts/{id}/preview：合同预览。 */
export function startContractPreviewJob(
  contractID: string,
  input: ContractPreviewJobInput,
  options: RequestOptions = {},
): Promise<JobStartResponse> {
  return post(`/jobs/contracts/${enc(contractID)}/preview`, input, options);
}

/** POST /jobs/contracts/{id}/generate：单份合同生成。 */
export function startContractGenerateJob(
  contractID: string,
  input: ContractGenerateJobInput,
  options: RequestOptions = {},
): Promise<JobStartResponse> {
  return post(`/jobs/contracts/${enc(contractID)}/generate`, input, options);
}

/** POST /jobs/templates/{id}/batch-preview：批量预检（逐份 i/N）。 */
export function startBatchPreviewJob(
  templateID: string,
  input: BatchPreviewInput,
  options: RequestOptions = {},
): Promise<JobStartResponse> {
  return post(`/jobs/templates/${enc(templateID)}/batch-preview`, input, options);
}

/** POST /jobs/templates/{id}/batch-generate：批量生成（逐份 i/N）。 */
export function startBatchGenerateJob(
  templateID: string,
  input: BatchGenerateInput,
  options: RequestOptions = {},
): Promise<JobStartResponse> {
  return post(`/jobs/templates/${enc(templateID)}/batch-generate`, input, options);
}

/** POST /jobs/excel/preview：Excel 解析与逐行校验。 */
export function startExcelPreviewJob(
  input: ExcelImportRequest,
  options: RequestOptions = {},
): Promise<JobStartResponse> {
  return post("/jobs/excel/preview", input, options);
}

/** POST /jobs/excel/commit：Excel 逐行写入。 */
export function startExcelCommitJob(
  input: ExcelImportRequest,
  options: RequestOptions = {},
): Promise<JobStartResponse> {
  return post("/jobs/excel/commit", input, options);
}

/** GET /jobs/{id}：任务快照（result 与对应同步端点响应同构）。 */
export function getJob<T = unknown>(
  id: string,
  options: RequestOptions = {},
): Promise<JobSnapshot<T>> {
  return get(`/jobs/${enc(id)}`, options);
}

// 结果类型别名：调用方给 useJob<T> 标注时可直接引用，保持与同步响应对齐。
export type TemplateImportJobResult = TemplateMutationResponse;
export type ContractPreviewJobResult = TemplatePreviewResponse;
export type ContractGenerateJobResult = TemplateGenerateResponse;
export type BatchPreviewJobResult = BatchPreviewResponse;
export type BatchGenerateJobResult = BatchGenerateResult;
export type ExcelPreviewJobResult = ImportPreview;
export type ExcelCommitJobResult = ImportResult;

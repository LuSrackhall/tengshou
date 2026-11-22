// 模板、生成、批量生成与生成记录端点。
//
//   GET/POST/PUT/DELETE /templates、/templates/import、/templates/{id}
//   GET  /templates/{id}/usage、/check、/pdf、/source
//   POST /templates/{id}/preview、/preflight、/generate
//   POST /templates/{id}/batch/preview、/batch/generate
//   GET  /documents、/documents/{id}/file、/converters

import {
  apiUrl,
  del,
  encodePathSegment,
  get,
  getFile,
  post,
  put,
  type DownloadedFile,
  type RequestOptions,
} from "../client";
import type {
  BatchGenerateInput,
  BatchGenerateResult,
  BatchPreviewInput,
  BatchPreviewResponse,
  ConvertersResponse,
  DeletedResponse,
  DocumentsQuery,
  List,
  OutputDocument,
  PreflightResult,
  Template,
  TemplateCheck,
  TemplateGenerateInput,
  TemplateGenerateResponse,
  TemplateImportInput,
  TemplateMappingInput,
  TemplateMutationResponse,
  TemplatePreflightInput,
  TemplatePreviewInput,
  TemplatePreviewResponse,
  TemplatesQuery,
  TemplateUsage,
} from "../types";

const enc = encodePathSegment;

// ---------- 模板版本 ----------

/** GET /templates：按 kind/html|pdf、关键词与分页列出模板版本。 */
export function listTemplates(
  query: TemplatesQuery = {},
  options: RequestOptions = {},
): Promise<List<Template>> {
  return get("/templates", { ...options, query });
}

/** GET /templates/{id}。 */
export function getTemplate(
  id: string,
  options: RequestOptions = {},
): Promise<Template> {
  return get(`/templates/${enc(id)}`, options);
}

/** POST /templates/import：上传 .docx（转换）或 .pdf。 */
export function importTemplate(
  input: TemplateImportInput,
  options: RequestOptions = {},
): Promise<TemplateMutationResponse> {
  return post("/templates/import", input, options);
}

/** DELETE /templates/{id}。 */
export function deleteTemplate(
  id: string,
  options: RequestOptions = {},
): Promise<DeletedResponse> {
  return del(`/templates/${enc(id)}`, options);
}

/** PUT /templates/{id}/mapping：保存标定框，生成新版本。 */
export function saveTemplateMapping(
  id: string,
  input: TemplateMappingInput,
  options: RequestOptions = {},
): Promise<TemplateMutationResponse> {
  return put(`/templates/${enc(id)}/mapping`, input, options);
}

/** GET /templates/{id}/usage：模板家族的使用范围。 */
export function getTemplateUsage(
  id: string,
  options: RequestOptions = {},
): Promise<TemplateUsage> {
  return get(`/templates/${enc(id)}/usage`, options);
}

/** GET /templates/{id}/check：标定框与字段目录的只读校验。 */
export function checkTemplate(
  id: string,
  options: RequestOptions = {},
): Promise<TemplateCheck> {
  return get(`/templates/${enc(id)}/check`, options);
}

/** 模板 PDF 地址；供 PDF.js 同源加载。 */
export function templatePdfUrl(id: string): string {
  return apiUrl(`/templates/${enc(id)}/pdf`);
}

/** 原始导入文件（.docx/.pdf）地址。 */
export function templateSourceUrl(id: string): string {
  return apiUrl(`/templates/${enc(id)}/source`);
}

/** GET /templates/{id}/pdf 的原始字节（标定编辑器交给 PDF.js 渲染）。 */
export async function fetchTemplatePdf(
  id: string,
  options: RequestOptions = {},
): Promise<Uint8Array> {
  const file = await getFile(`/templates/${enc(id)}/pdf`, options);
  return new Uint8Array(await file.blob.arrayBuffer());
}

/** GET /templates/{id}/source：下载原始导入文件。 */
export function downloadTemplateSource(
  id: string,
  options: RequestOptions = {},
): Promise<DownloadedFile> {
  return getFile(`/templates/${enc(id)}/source`, options);
}

/** GET /documents/{id}/file 的原始字节（生成结果预览）。 */
export async function fetchDocumentFile(
  id: string,
  options: RequestOptions = {},
): Promise<Uint8Array> {
  const file = await getFile(`/documents/${enc(id)}/file`, options);
  return new Uint8Array(await file.blob.arrayBuffer());
}

/** POST /templates/{id}/preview：渲染预览（样例或真实合同）。 */
export function previewTemplate(
  id: string,
  input: TemplatePreviewInput,
  options: RequestOptions = {},
): Promise<TemplatePreviewResponse> {
  return post(`/templates/${enc(id)}/preview`, input, options);
}

/** POST /templates/{id}/preflight：单份合同的生成就绪检查。 */
export function preflightGeneration(
  id: string,
  input: TemplatePreflightInput,
  options: RequestOptions = {},
): Promise<PreflightResult> {
  return post(`/templates/${enc(id)}/preflight`, input, options);
}

/** POST /templates/{id}/generate：生成并留存 PDF。 */
export function generateContract(
  id: string,
  input: TemplateGenerateInput,
  options: RequestOptions = {},
): Promise<TemplateGenerateResponse> {
  return post(`/templates/${enc(id)}/generate`, input, options);
}

/** POST /templates/{id}/batch/preview：批量就绪报告（可带样例 PDF）。 */
export function batchPreviewGeneration(
  id: string,
  input: BatchPreviewInput,
  options: RequestOptions = {},
): Promise<BatchPreviewResponse> {
  return post(`/templates/${enc(id)}/batch/preview`, input, options);
}

/** POST /templates/{id}/batch/generate：逐合同生成，失败不中断。 */
export function batchGenerateContracts(
  id: string,
  input: BatchGenerateInput,
  options: RequestOptions = {},
): Promise<BatchGenerateResult> {
  return post(`/templates/${enc(id)}/batch/generate`, input, options);
}

// ---------- 生成记录 / 转换程序 ----------

/** GET /documents：按合同/模板/批次/状态筛选输出记录。 */
export function listDocuments(
  query: DocumentsQuery = {},
  options: RequestOptions = {},
): Promise<List<OutputDocument>> {
  return get("/documents", { ...options, query });
}

/** 生成 PDF 的下载/预览地址。 */
export function documentFileUrl(id: string): string {
  return apiUrl(`/documents/${enc(id)}/file`);
}

/** GET /converters：Word→PDF 转换程序可用性。 */
export function listConverters(
  options: RequestOptions = {},
): Promise<ConvertersResponse> {
  return get("/converters", options);
}

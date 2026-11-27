// 长操作后台任务契约（internal/httpapi/jobs.go）。
//
// 启动端点与同步端点同构：请求体一致，返回 202 {"job_id"}；GET /jobs/{id}
// 返回下面的快照。轮询间隔 400ms（composables/useJob.ts）。成功时 result
// 与对应同步端点的成功响应同构；失败时 error 与同步错误信封同构，前端可
// 直接复用 ApiError 呈现。

import type { ApiFieldError } from "./common";
import type { DocumentKind } from "./documents";

export type JobStatus = "running" | "done" | "failed";

/**
 * 阶段标识（服务端 service.Progress*）：导入 saving → converting → parsing
 * → saving；单份生成 rendering → writing；批量 batch（真实 i/N）；
 * Excel parsing → writing（逐行）。
 */
export type JobStage =
  | "queued"
  | "saving"
  | "converting"
  | "parsing"
  | "rendering"
  | "writing"
  | "batch"
  | "done";

/** 任务失败时的错误信封（与同步接口的 error 对象同构）。 */
export interface JobError {
  code: string;
  message: string;
  fields: ApiFieldError[];
}

/** GET /jobs/{id} 的快照；result 与对应同步端点响应同构。 */
export interface JobSnapshot<T = unknown> {
  id: string;
  /** template_import / contract_preview / contract_generate / batch_preview /
   *  batch_generate / excel_preview / excel_commit。 */
  kind: string;
  status: JobStatus;
  stage: JobStage;
  message: string;
  /** 真实进度计数；total 为 0 表示总量未知（不确定进度条）。 */
  current: number;
  total: number;
  elapsed_ms: number;
  result?: T;
  error?: JobError;
}

/** POST /jobs/* 的 202 响应。 */
export interface JobStartResponse {
  job_id: string;
}

// ---------- 合同预览 / 生成 job 请求体 ----------

/**
 * 合同 job 用路径定位合同，模板版本放在请求体（同步端点把它放在 URL）。
 * 其余字段与同步请求体逐字段一致。
 */
export interface ContractJobInput {
  template_id: string;
  contract_id: string;
}

/** POST /jobs/contracts/{id}/preview。 */
export interface ContractPreviewJobInput extends ContractJobInput {
  sample?: boolean;
}

/** POST /jobs/contracts/{id}/generate。 */
export interface ContractGenerateJobInput extends ContractJobInput {
  /** 预览返回的摘要，必须回传。 */
  preview_sha256?: string;
  kind?: DocumentKind | "";
  source_document_id?: string;
  allow_stale_signature?: boolean;
}

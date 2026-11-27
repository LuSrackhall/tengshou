// 局域网扫码签名会话契约（桌面端 app API 视角）。
//
// 来源（与 Go 侧逐字段核对）：
//   internal/sign/session.go        Session 状态常量与字段
//   internal/httpapi/signapi.go     signSessionView / createSignSessionInput /
//                                   cancel 响应
//
// 手机签名页（/sign/{token}）由 Go 直接渲染 HTML，不属于 Vue 前端工程。

import type { ISODateTime } from "./common";

export type SignatureSessionState =
  | "pending"
  | "submitting"
  | "completed"
  | "expired";

/**
 * 会话视图（signSessionView）。一次性 token 只存在于二维码 URL 里，
 * 不会出现在该结构中。
 */
export interface SignatureSession {
  id: string;
  /** signature（默认）或 enroll；旧版服务可能省略。 */
  kind?: "signature" | "enroll";
  state: SignatureSessionState;
  contract_id: string;
  contract_no: string;
  contract_title: string;
  content_sha256: string;
  party_id: string;
  signer_name: string;
  urls: string[];
  /** urls 的第一个地址（urls 非空时才有）。 */
  url?: string;
  /** 二维码 PNG 的相对地址，可直接作为 img.src。 */
  qr_url: string;
  created_at: ISODateTime;
  expires_at: ISODateTime;
  /** 以下字段仅在 state=completed 时出现。 */
  completed_at?: ISODateTime;
  signature_id?: string;
  asset_path?: string;
  width_px?: number;
  height_px?: number;
  /** GET 时会补上：合同当前内容摘要与是否已变化。 */
  current_content_sha256?: string;
  content_changed?: boolean;
}

/** POST /signature-sessions 的请求体（createSignSessionInput）。 */
export interface CreateSignatureSessionInput {
  contract_id: string;
  /** 默认取合同乙方；必须是甲方或乙方的档案。 */
  party_id?: string;
  /** 默认取档案名称。 */
  signer_name?: string;
}

/** DELETE /signature-sessions/{id} 的响应。 */
export interface SignatureSessionCancelResponse {
  cancelled: boolean;
}

// ---------- 档案签名/签名章复用（te-48hkl） ----------

/**
 * 复用档案签名的来源：archive=档案手写签名，archive_seal=档案签名章（私章）。
 * 对应 Go model.SignatureSourceArchive / SignatureSourceArchiveSeal。
 */
export type ArchiveSignatureKind = "archive" | "archive_seal";

/** POST /contracts/{id}/signature-reuse 的请求体。 */
export interface ReusePartySignatureInput {
  party_id: string;
  /** 省略或 "archive" 表示手写签名；"archive_seal" 表示签名章。 */
  kind?: ArchiveSignatureKind;
}

// ---------- 固定自助录入入口（可打印海报，te-48hkl） ----------

/** GET/POST /enroll-entry 的返回：长期有效、可打印的录入入口。 */
export interface EnrollEntry {
  issued: boolean;
  enabled: boolean;
  /** 入口码；未生成时缺失。 */
  token?: string;
  urls: string[];
  /** urls 的第一个地址（urls 非空时才有）。 */
  url?: string;
  /** 二维码 PNG 的相对地址。 */
  qr_url: string;
}

/**
 * v0.2 局域网自助建档会话（enrollSessionView）。手机端在 /enroll/{token}
 * 填写姓名、证件号、证件照正反面与手写签名，提交即建档。
 */
export interface EnrollSession {
  id: string;
  kind: "enroll";
  state: SignatureSessionState;
  urls: string[];
  /** urls 的第一个地址（urls 非空时才有）。 */
  url?: string;
  /** 二维码 PNG 的相对地址，可直接作为 img.src。 */
  qr_url: string;
  created_at: ISODateTime;
  expires_at: ISODateTime;
  completed_at?: ISODateTime;
}

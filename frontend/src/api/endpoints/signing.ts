// 扫码签名会话与签名导出端点。
//
//   POST   /signature-sessions
//   GET    /signature-sessions/{id}
//   DELETE /signature-sessions/{id}
//   GET    /signature-sessions/{id}/qr.png
//   POST   /signatures/{id}/export
//
// 手机签名页（/sign/{token}）由 Go 直接渲染 HTML，不在前端工程内。

import {
  apiUrl,
  del,
  encodePathSegment,
  get,
  post,
  postFile,
  type DownloadedFile,
  type RequestOptions,
} from "../client";
import type {
  CreateSignatureSessionInput,
  EnrollSession,
  ItemResponse,
  Signature,
  SignatureSession,
  SignatureSessionCancelResponse,
} from "../types";

const enc = encodePathSegment;

/** POST /signature-sessions：创建一次签名采集会话。 */
export function createSignatureSession(
  input: CreateSignatureSessionInput,
  options: RequestOptions = {},
): Promise<ItemResponse<SignatureSession>> {
  return post("/signature-sessions", input, options);
}

/** GET /signature-sessions/{id}：轮询状态；会附带内容是否已变化。 */
export function getSignatureSession(
  id: string,
  options: RequestOptions = {},
): Promise<ItemResponse<SignatureSession>> {
  return get(`/signature-sessions/${enc(id)}`, options);
}

/** DELETE /signature-sessions/{id}：取消会话。 */
export function cancelSignatureSession(
  id: string,
  options: RequestOptions = {},
): Promise<SignatureSessionCancelResponse> {
  return del(`/signature-sessions/${enc(id)}`, options);
}

/** 签名会话二维码 PNG 地址；index 对应 urls 里的网卡地址。 */
export function signatureSessionQrUrl(id: string, index = 0): string {
  return apiUrl(`/signature-sessions/${enc(id)}/qr.png`, { index });
}

/** POST /signatures/{id}/export：二次确认后导出签名 PNG。 */
export function exportSignature(
  id: string,
  options: RequestOptions = {},
): Promise<DownloadedFile> {
  return postFile(`/signatures/${enc(id)}/export`, { confirm: true }, options);
}

/**
 * POST /parties/{id}/signature-sessions：为相对方档案采集长期复用的
 * 手写签名（手机扫码页 /sign/{token}）。
 */
export function createPartySignatureSession(
  partyId: string,
  options: RequestOptions = {},
): Promise<ItemResponse<SignatureSession>> {
  return post(`/parties/${enc(partyId)}/signature-sessions`, {}, options);
}

/**
 * POST /contracts/{id}/signature-reuse：显式复用档案签名到当前合同内容版本。
 * 服务端记入变更记录；同一内容版本重复调用是幂等的。
 */
export function reusePartySignature(
  contractId: string,
  partyId: string,
  options: RequestOptions = {},
): Promise<ItemResponse<Signature>> {
  return post(
    `/contracts/${enc(contractId)}/signature-reuse`,
    { party_id: partyId },
    options,
  );
}

// ---------- v0.2 局域网自助建档 ----------

/** POST /enroll-sessions：生成自助建档二维码会话。 */
export function createEnrollSession(
  options: RequestOptions = {},
): Promise<ItemResponse<EnrollSession>> {
  return post("/enroll-sessions", {}, options);
}

/** GET /enroll-sessions/{id}：轮询自助建档提交状态。 */
export function getEnrollSession(
  id: string,
  options: RequestOptions = {},
): Promise<ItemResponse<EnrollSession>> {
  return get(`/enroll-sessions/${enc(id)}`, options);
}

/** DELETE /enroll-sessions/{id}：取消未完成的自助建档会话。 */
export function cancelEnrollSession(
  id: string,
  options: RequestOptions = {},
): Promise<SignatureSessionCancelResponse> {
  return del(`/enroll-sessions/${enc(id)}`, options);
}

/** 自助建档会话二维码 PNG 地址；index 对应 urls 里的网卡地址。 */
export function enrollSessionQrUrl(id: string, index = 0): string {
  return apiUrl(`/enroll-sessions/${enc(id)}/qr.png`, { index });
}

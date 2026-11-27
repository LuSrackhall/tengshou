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
  ArchiveSignatureKind,
  CreateSignatureSessionInput,
  EnrollEntry,
  EnrollSession,
  ItemResponse,
  Party,
  ReusePartySignatureInput,
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
 * POST /contracts/{id}/signature-reuse：使用租户档案签名（或签名章）直接套印
 * 到合同。服务端记入变更记录；档案复用不绑定合同内容版本，同一合同/档案/
 * 签名资产重复调用是幂等的。
 */
export function reusePartySignature(
  contractId: string,
  partyId: string,
  kind?: ArchiveSignatureKind,
  options: RequestOptions = {},
): Promise<ItemResponse<Signature>> {
  const body: ReusePartySignatureInput = { party_id: partyId };
  if (kind) {
    body.kind = kind;
  }
  return post(`/contracts/${enc(contractId)}/signature-reuse`, body, options);
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

// ---------- 固定自助录入入口（可打印海报，te-48hkl） ----------

/** GET /enroll-entry：长期入口状态与二维码地址。 */
export function getEnrollEntry(
  options: RequestOptions = {},
): Promise<ItemResponse<EnrollEntry>> {
  return get("/enroll-entry", options);
}

/** POST /enroll-entry/rotate：换码（旧海报立即失效）。 */
export function rotateEnrollEntry(
  options: RequestOptions = {},
): Promise<ItemResponse<EnrollEntry>> {
  return post("/enroll-entry/rotate", {}, options);
}

/** POST /enroll-entry/disable：停用固定入口（旧海报立即失效）。 */
export function disableEnrollEntry(
  options: RequestOptions = {},
): Promise<ItemResponse<EnrollEntry>> {
  return post("/enroll-entry/disable", {}, options);
}

/** 固定入口二维码 PNG 地址；index 对应 urls 里的网卡地址。 */
export function enrollEntryQrUrl(index = 0): string {
  return apiUrl("/enroll-entry/qr.png", { index });
}

// ---------- 档案公章 / 签名章图片（te-48hkl） ----------

/** POST /parties/{id}/seal：上传或更换档案公章（base64 data URL）。 */
export function uploadPartySeal(
  partyId: string,
  image: string,
  options: RequestOptions = {},
): Promise<ItemResponse<Party>> {
  return post(`/parties/${enc(partyId)}/seal`, { image }, options);
}

/** 档案公章图片地址。 */
export function partySealImageUrl(partyId: string): string {
  return apiUrl(`/parties/${enc(partyId)}/seal`);
}

/** DELETE /parties/{id}/seal：停用档案公章（保留资产，不再复用）。 */
export function clearPartySeal(
  partyId: string,
  options: RequestOptions = {},
): Promise<ItemResponse<Party>> {
  return del(`/parties/${enc(partyId)}/seal`, options);
}

/** POST /parties/{id}/signature-seal：上传或更换档案签名章（私章）图片。 */
export function uploadPartySignatureSeal(
  partyId: string,
  image: string,
  options: RequestOptions = {},
): Promise<ItemResponse<Party>> {
  return post(`/parties/${enc(partyId)}/signature-seal`, { image }, options);
}

/** 档案签名章图片地址。 */
export function partySignatureSealImageUrl(partyId: string): string {
  return apiUrl(`/parties/${enc(partyId)}/signature-seal`);
}

/** DELETE /parties/{id}/signature-seal：停用档案签名章。 */
export function clearPartySignatureSeal(
  partyId: string,
  options: RequestOptions = {},
): Promise<ItemResponse<Party>> {
  return del(`/parties/${enc(partyId)}/signature-seal`, options);
}

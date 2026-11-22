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
  ItemResponse,
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

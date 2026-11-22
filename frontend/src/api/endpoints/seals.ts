// 公章端点：图片导入/替换、合同关联与图片地址。
//
//   GET/POST      /seals
//   GET/PUT/DELETE /seals/{id}
//   GET  /seals/{id}/image
//   GET  /seals/{id}/contracts
//   POST /seals/{id}/replace、/attach、/detach

import {
  apiUrl,
  del,
  encodePathSegment,
  get,
  post,
  put,
  type RequestOptions,
} from "../client";
import type {
  DeletedResponse,
  ItemResponse,
  List,
  Seal,
  SealAttachResponse,
  SealContractsResponse,
  SealDetachResponse,
  SealImportInput,
  SealsQuery,
  SealReplaceInput,
  SealUpdateInput,
} from "../types";

const enc = encodePathSegment;

/** GET /seals：分页 + 关键词 + party_id 筛选。 */
export function listSeals(
  query: SealsQuery = {},
  options: RequestOptions = {},
): Promise<List<Seal>> {
  return get("/seals", { ...options, query });
}

/** GET /seals/{id}。 */
export function getSeal(
  id: string,
  options: RequestOptions = {},
): Promise<Seal> {
  return get(`/seals/${enc(id)}`, options);
}

/** POST /seals：导入公章图片（base64）。 */
export function importSeal(
  input: SealImportInput,
  options: RequestOptions = {},
): Promise<ItemResponse<Seal>> {
  return post("/seals", input, options);
}

/** PUT /seals/{id}：更新名称/所属单位/说明。 */
export function updateSeal(
  id: string,
  input: SealUpdateInput,
  options: RequestOptions = {},
): Promise<ItemResponse<Seal>> {
  return put(`/seals/${enc(id)}`, input, options);
}

/** POST /seals/{id}/replace：替换公章图片。 */
export function replaceSealImage(
  id: string,
  input: SealReplaceInput,
  options: RequestOptions = {},
): Promise<ItemResponse<Seal>> {
  return post(`/seals/${enc(id)}/replace`, input, options);
}

/** DELETE /seals/{id}。 */
export function deleteSeal(
  id: string,
  options: RequestOptions = {},
): Promise<DeletedResponse> {
  return del(`/seals/${enc(id)}`, options);
}

/** GET /seals/{id}/contracts：已关联的合同 id，用于批量选择预填。 */
export function getSealContracts(
  id: string,
  options: RequestOptions = {},
): Promise<SealContractsResponse> {
  return get(`/seals/${enc(id)}/contracts`, options);
}

/** POST /seals/{id}/attach：把公章关联到一批合同。 */
export function attachSeal(
  id: string,
  contractIds: string[],
  options: RequestOptions = {},
): Promise<SealAttachResponse> {
  return post(
    `/seals/${enc(id)}/attach`,
    { contract_ids: contractIds },
    options,
  );
}

/** POST /seals/{id}/detach：解除一批合同关联。 */
export function detachSeal(
  id: string,
  contractIds: string[],
  options: RequestOptions = {},
): Promise<SealDetachResponse> {
  return post(
    `/seals/${enc(id)}/detach`,
    { contract_ids: contractIds },
    options,
  );
}

/** 公章图片地址；图片按 sha256 不可变，可直接作为 img.src。 */
export function sealImageUrl(id: string): string {
  return apiUrl(`/seals/${enc(id)}/image`);
}

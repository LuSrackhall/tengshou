// 台账核心端点：字段定义、相对方、合同、变更记录。
//
//   GET/POST/PUT  /fields、/fields/{id}、/fields/{id}/archive
//   GET/POST/PUT/DELETE /parties、/parties/{id}
//   GET/POST/PUT/DELETE /contracts、/contracts/{id}
//   GET /contracts/years、/contracts/{id}/renewal-draft
//   GET /changelog

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
  ChangeLog,
  ChangeLogQuery,
  Contract,
  ContractDetail,
  ContractInput,
  ContractYearsResponse,
  ContractsQuery,
  DeletedResponse,
  FieldDefinition,
  FieldEntity,
  FieldInput,
  FieldUpdateInput,
  FieldsByEntityResponse,
  FieldsQuery,
  ItemResponse,
  ItemWarningsResponse,
  List,
  PartiesQuery,
  Party,
  PartyInput,
  PartyPhotoSide,
  RenewalDraft,
  TerminationInput,
} from "../types";

const enc = encodePathSegment;

// ---------- 字段定义 ----------

/** GET /fields：按实体分组返回全量字段（含归档由 archived=1 控制）。 */
export function listFields(
  query: Pick<FieldsQuery, "archived"> = {},
  options: RequestOptions = {},
): Promise<FieldsByEntityResponse> {
  return get("/fields", { ...options, query });
}

/** GET /fields?entity=...：只取一个实体的字段。 */
export function listFieldsForEntity(
  entity: FieldEntity,
  query: Pick<FieldsQuery, "archived"> = {},
  options: RequestOptions = {},
): Promise<List<FieldDefinition>> {
  return get("/fields", { ...options, query: { ...query, entity } });
}

/** POST /fields：新建运行时字段。 */
export function createField(
  input: FieldInput,
  options: RequestOptions = {},
): Promise<ItemResponse<FieldDefinition>> {
  return post("/fields", input, options);
}

/** PUT /fields/{id}：部分更新；key 不可修改。 */
export function updateField(
  id: string,
  input: FieldUpdateInput,
  options: RequestOptions = {},
): Promise<ItemResponse<FieldDefinition>> {
  return put(`/fields/${enc(id)}`, input, options);
}

/** POST /fields/{id}/archive：归档用户字段（内置字段不可归档）。 */
export function archiveField(
  id: string,
  options: RequestOptions = {},
): Promise<ItemResponse<FieldDefinition>> {
  return post(`/fields/${enc(id)}/archive`, {}, options);
}

// ---------- 相对方 ----------

/** GET /parties：分页 + 关键词检索。 */
export function listParties(
  query: PartiesQuery = {},
  options: RequestOptions = {},
): Promise<List<Party>> {
  return get("/parties", { ...options, query });
}

/** GET /parties/{id}。 */
export function getParty(
  id: string,
  options: RequestOptions = {},
): Promise<Party> {
  return get(`/parties/${enc(id)}`, options);
}

/** POST /parties：新建档案；warnings 为重复名称等非阻断提示。 */
export function createParty(
  input: PartyInput,
  options: RequestOptions = {},
): Promise<ItemWarningsResponse<Party>> {
  return post("/parties", input, options);
}

/** PUT /parties/{id}。 */
export function updateParty(
  id: string,
  input: PartyInput,
  options: RequestOptions = {},
): Promise<ItemWarningsResponse<Party>> {
  return put(`/parties/${enc(id)}`, input, options);
}

/** DELETE /parties/{id}：被合同时返回 409。 */
export function deleteParty(
  id: string,
  options: RequestOptions = {},
): Promise<DeletedResponse> {
  return del(`/parties/${enc(id)}`, options);
}

/**
 * POST /parties/{id}/id-photos/{side}：上传或更换证件照（正面/反面）。
 * image 为 data URL；服务端压缩为 JPEG 后存入数据目录。
 */
export function uploadPartyIDPhoto(
  id: string,
  side: PartyPhotoSide,
  image: string,
  options: RequestOptions = {},
): Promise<ItemResponse<Party>> {
  return post(`/parties/${enc(id)}/id-photos/${side}`, { image }, options);
}

/** GET /parties/{id}/id-photos/{side}：证件照图片地址（服务端校验摘要）。 */
export function partyIDPhotoUrl(id: string, side: PartyPhotoSide): string {
  return apiUrl(`/parties/${enc(id)}/id-photos/${side}`);
}

/** GET /parties/{id}/signature：档案级签名 PNG 地址。 */
export function partySignatureImageUrl(id: string): string {
  return apiUrl(`/parties/${enc(id)}/signature`);
}

// ---------- 合同 ----------

/** GET /contracts：按年度/状态/关键词/相对方筛选。 */
export function listContracts(
  query: ContractsQuery = {},
  options: RequestOptions = {},
): Promise<List<Contract>> {
  return get("/contracts", { ...options, query });
}

/** GET /contracts/{id}：合同 + 关联档案/模板/签名/公章/输出 PDF。 */
export function getContract(
  id: string,
  options: RequestOptions = {},
): Promise<ContractDetail> {
  return get(`/contracts/${enc(id)}`, options);
}

/** POST /contracts：新建合同，编号留空时服务端分配。 */
export function createContract(
  input: ContractInput,
  options: RequestOptions = {},
): Promise<ItemWarningsResponse<Contract>> {
  return post("/contracts", input, options);
}

/** PUT /contracts/{id}：更新合同（编号不可变）。 */
export function updateContract(
  id: string,
  input: ContractInput,
  options: RequestOptions = {},
): Promise<ItemWarningsResponse<Contract>> {
  return put(`/contracts/${enc(id)}`, input, options);
}

/** DELETE /contracts/{id}。 */
export function deleteContract(
  id: string,
  options: RequestOptions = {},
): Promise<DeletedResponse> {
  return del(`/contracts/${enc(id)}`, options);
}

/** GET /contracts/years：跨年度合同数量。 */
export function getContractYears(
  options: RequestOptions = {},
): Promise<ContractYearsResponse> {
  return get("/contracts/years", options);
}

/** POST /contracts/{id}/terminate：终止生效中的合同（日期与原因必填）。 */
export function terminateContract(
  id: string,
  input: TerminationInput,
  options: RequestOptions = {},
): Promise<ItemResponse<Contract>> {
  return post(`/contracts/${enc(id)}/terminate`, input, options);
}

/** GET /contracts/{id}/renewal-draft：续签预填草稿（只读）。 */
export function getRenewalDraft(
  id: string,
  options: RequestOptions = {},
): Promise<RenewalDraft> {
  return get(`/contracts/${enc(id)}/renewal-draft`, options);
}

// ---------- 变更记录（审计） ----------

/** GET /changelog：after 游标模式用于实时刷新，其余为筛选列表。 */
export function listChangelog(
  query: ChangeLogQuery = {},
  options: RequestOptions = {},
): Promise<List<ChangeLog>> {
  return get("/changelog", { ...options, query });
}

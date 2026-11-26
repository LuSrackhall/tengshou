// 写入类接口的请求体与列表查询参数。
//
// 来源（与 Go 侧逐字段核对）：
//   internal/service/parties.go     PartyInput
//   internal/service/assets.go      AssetInput
//   internal/service/contracts.go   ContractInput
//   internal/service/fields.go      FieldInput（更新是部分更新）
//   internal/service/seals.go       SealInput
//   internal/service/renewal.go     RenewalDraft
//   internal/httpapi/templateapi.go templateImportRequest / preview / generate /
//                                   preflight / batch 请求结构
//   internal/httpapi/appapi.go      filter() / contracts 查询参数
//   internal/httpapi/signapi.go     createSignSessionInput
//
// 规则：Go 侧的非指针字段在这里可按可选处理（缺省即零值）；更新接口
// 只覆盖显式提交的字段。

import type {
  ChangeEntity,
  ChangeSource,
  ListQuery,
  YearCount,
} from "./common";
import type { DocumentKind, DocumentStatus } from "./documents";
import type { AssetType } from "./assets";
import type {
  ContractStatus,
  FieldDefinition,
  FieldEntity,
  FieldType,
  PartyKind,
  Placement,
  TemplateKind,
  TerminationReason,
} from "./records";

// ---------- 相对方 ----------

/** PartyInput：保存相对方档案（新建/更新共用）。 */
export interface PartyInput {
  id?: string;
  kind: PartyKind;
  name: string;
  id_number?: string;
  phone?: string;
  address?: string;
  note?: string;
  data?: Record<string, unknown>;
}

// ---------- 房产 ----------

/** AssetInput：保存房产档案（新建/更新共用）；area_sqm 不传表示清空面积。 */
export interface AssetInput {
  id?: string;
  asset_type?: AssetType;
  name: string;
  address?: string;
  room_no?: string;
  area_sqm?: number;
  note?: string;
  /** 不传时保持原启用状态（停用/启用是显式动作）。 */
  enabled?: boolean;
}

// ---------- 合同 ----------

/** ContractInput：保存合同；编号留空时由服务端分配。 */
export interface ContractInput {
  id?: string;
  contract_no?: string;
  year: number;
  title: string;
  type?: string;
  party_a_id?: string;
  party_b_id?: string;
  /** 租赁房产档案 id；留空的历史合同保持原值。 */
  asset_id?: string;
  /** 相对方名称；用于无档案时的提示。 */
  party_b?: string;
  status?: ContractStatus;
  signed_on?: string;
  valid_from?: string;
  valid_to?: string;
  lease_start?: string;
  lease_end?: string;
  /** 状态为 terminated 时必填；离开该状态时后端清空。 */
  terminated_on?: string;
  termination_reason?: TerminationReason;
  termination_note?: string;
  /** 续签起租日早于原合同到期日时，确认后置 true 重新保存。 */
  renewal_termination_confirm?: boolean;
  amount?: number;
  template_id?: string;
  /** 仅新建时生效的续签来源。 */
  source_contract_id?: string;
  data?: Record<string, unknown>;
  /** 允许依据 party_b 名称自动建档。 */
  create_party?: boolean;
}

/** TerminationInput：台账「终止」动作的请求体（POST /contracts/{id}/terminate）。 */
export interface TerminationInput {
  terminated_on: string;
  reason: TerminationReason;
  note?: string;
}

/** 续签草稿（service.RenewalDraft）；只读，不落库。 */
export interface RenewalDraft {
  source: {
    id: string;
    contract_no: string;
    title: string;
    year: number;
    lease_start?: string;
    lease_end?: string;
  };
  input: ContractInput;
  /** 已从原合同带入的字段 key，供界面展示。 */
  reused: string[];
}

// ---------- 字段定义 ----------

/** FieldInput：新建字段的完整请求体。 */
export interface FieldInput {
  entity: FieldEntity;
  key: string;
  label: string;
  type: FieldType;
  required?: boolean;
  default_value?: string;
  options?: string[];
  in_ledger?: boolean;
  in_template?: boolean;
  sort_order?: number;
  note?: string;
  archived?: boolean;
}

/** 更新字段是部分更新；key 创建后不可修改。 */
export type FieldUpdateInput = Partial<Omit<FieldInput, "key">>;

// ---------- 公章 ----------

/** POST /seals：导入公章图片。 */
export interface SealImportInput {
  name: string;
  party_id?: string;
  note?: string;
  /** 图片内容的 base64（可带 data: 前缀）。 */
  file_base64: string;
}

/** PUT /seals/{id}：更新名称/所属单位/说明（空字段保持原值）。 */
export interface SealUpdateInput {
  name?: string;
  party_id?: string;
  note?: string;
}

/** POST /seals/{id}/replace：替换图片本体。 */
export interface SealReplaceInput {
  file_base64: string;
}

/** POST /seals/{id}/attach|detach 的请求体。 */
export interface SealContractsInput {
  contract_ids: string[];
}

// ---------- 模板 ----------

/** POST /templates/import：上传 .docx 或 .pdf。 */
export interface TemplateImportInput {
  /** 留空时由文件名推导。 */
  name?: string;
  file_name: string;
  /** 文件内容的 base64（可带 data: 前缀）。 */
  file_base64: string;
}

/** PUT /templates/{id}/mapping：保存标定框并生成新版本。 */
export interface TemplateMappingInput {
  placements: Placement[];
  is_active?: boolean;
}

/** POST /templates/{id}/preview。 */
export interface TemplatePreviewInput {
  contract_id?: string;
  /** true 时使用内置样例数据，不读取真实合同。 */
  sample?: boolean;
}

/** POST /templates/{id}/preflight。 */
export interface TemplatePreflightInput {
  contract_id: string;
}

/** POST /templates/{id}/generate。 */
export interface TemplateGenerateInput {
  contract_id: string;
  /** 预览返回的摘要，必须回传。 */
  preview_sha256?: string;
  /** initial | regenerate；空串表示由服务端判断（存量界面即传空串）。 */
  kind?: DocumentKind | "";
  source_document_id?: string;
  allow_stale_signature?: boolean;
}

/** POST /templates/{id}/batch/preview。 */
export interface BatchPreviewInput {
  contract_ids?: string[];
  /** contract_ids 为空时按年度/状态筛选。 */
  year?: number;
  status?: ContractStatus;
  limit?: number;
  /** 额外渲染一份样例数据 PDF。 */
  sample?: boolean;
}

/** 批量生成里的单份合同。 */
export interface BatchGenerateItemInput {
  contract_id: string;
  preview_sha256?: string;
  source_document_id?: string;
}

/** POST /templates/{id}/batch/generate。 */
export interface BatchGenerateInput {
  items: BatchGenerateItemInput[];
  kind?: DocumentKind;
  allow_stale_signature?: boolean;
}

// ---------- 查询参数 ----------

export type PartiesQuery = ListQuery;

export type AssetStatusFilter = "enabled" | "disabled";

export type AssetsQuery = ListQuery & {
  status?: AssetStatusFilter;
};

export type ContractsQuery = ListQuery & {
  year?: number;
  status?: ContractStatus;
  party_id?: string;
};

export type DocumentsQuery = ListQuery & {
  contract_id?: string;
  template_id?: string;
  batch_id?: string;
  status?: DocumentStatus;
};

export type TemplatesQuery = ListQuery & {
  kind?: TemplateKind;
};

export type SealsQuery = ListQuery & {
  party_id?: string;
};

export type FieldsQuery = {
  /** 传 "1" 时包含已归档字段（服务端只识别字符串 "1"）。 */
  archived?: "1";
  entity?: FieldEntity;
};

export type ChangeLogQuery = {
  entity?: ChangeEntity;
  entity_id?: string;
  source?: ChangeSource;
  /** 游标模式：返回该变更 id 之后的新记录，忽略其它过滤条件。 */
  after?: string;
  limit?: number;
  offset?: number;
};

/** GET /excel/ledger 的筛选参数（与合同列表一致）。 */
export type ExcelLedgerQuery = {
  year?: number;
  status?: ContractStatus;
  q?: string;
};

// ---------- 查询响应（少量内联包装） ----------

/** GET /contracts/years。 */
export interface ContractYearsResponse {
  years: YearCount[];
}

/** GET /fields 未指定 entity 时按实体分组返回。 */
export interface FieldsByEntityResponse {
  contract: FieldDefinition[];
  party: FieldDefinition[];
}

/** GET /seals/{id}/contracts。 */
export interface SealContractsResponse {
  contract_ids: string[];
}

/** POST /seals/{id}/attach。 */
export interface SealAttachResponse {
  attached: number;
}

/** POST /seals/{id}/detach。 */
export interface SealDetachResponse {
  detached: number;
}

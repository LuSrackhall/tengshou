// 台账核心实体契约：相对方、合同、字段定义、公章、模板。
//
// 来源（与 Go 侧逐字段核对）：
//   internal/model/model.go      Party / Contract / FieldDefinition / Seal /
//                                Placement / Template / TemplateUsage / ContractDetail
//   internal/httpapi/appapi.go   GET /parties/{id}、GET /contracts/{id} 的返回包装
//
// 字段名与 Go JSON tag 完全一致；omitempty 的字段在这里是可选属性。

import type { ISODateTime } from "./common";
import type { OutputDocument, Signature } from "./documents";

// ---------- 相对方 ----------

export type PartyKind = "person" | "organization";

export interface Party {
  id: string;
  kind: PartyKind;
  name: string;
  id_number?: string;
  phone?: string;
  address?: string;
  note?: string;
  /** 用户自定义字段的值，键为字段定义的 key。 */
  data: Record<string, unknown>;
  /** v0.2 档案升级：证件照（压缩后的 JPG）相对数据目录的路径与摘要。 */
  id_front_path?: string;
  id_front_sha256?: string;
  id_back_path?: string;
  id_back_sha256?: string;
  /** 档案级签名资产 id；一次采集后可复用于后续合同（需显式确认）。 */
  signature_id?: string;
  created_at: ISODateTime;
  updated_at: ISODateTime;
}

/** 证件照面别（对应 Go model.PartyPhotoFront / PartyPhotoBack）。 */
export type PartyPhotoSide = "front" | "back";

// ---------- 合同 ----------

export type ContractStatus = "draft" | "active" | "archived";

export interface Contract {
  id: string;
  contract_no: string;
  year: number;
  title: string;
  type?: string;
  template_id?: string;
  party_a_id?: string;
  party_b_id?: string;
  status: ContractStatus;
  /** 日期均为 YYYY-MM-DD。 */
  signed_on?: string;
  valid_from?: string;
  valid_to?: string;
  amount: number;
  /** 用户自定义字段的值，键为字段定义的 key。 */
  data: Record<string, unknown>;
  /** 续签草稿指向被续签的合同。 */
  source_contract_id?: string;
  created_at: ISODateTime;
  updated_at: ISODateTime;
}

/** 合同详情（model.ContractDetail）：合同 + 关联档案/模板/签名/公章/输出。 */
export interface ContractDetail {
  contract: Contract;
  party_a?: Party;
  party_b?: Party;
  template?: Template;
  signatures: Signature[];
  seals: Seal[];
  documents: OutputDocument[];
}

// ---------- 字段定义 ----------

export type FieldEntity = "contract" | "party";

export type FieldType =
  | "text"
  | "longtext"
  | "number"
  | "money"
  | "date"
  | "select"
  | "bool"
  | "image"
  | "party"
  | "status";

export interface FieldDefinition {
  id: string;
  entity: FieldEntity;
  key: string;
  label: string;
  type: FieldType;
  required: boolean;
  default_value?: string;
  options?: string[];
  in_ledger: boolean;
  in_template: boolean;
  built_in: boolean;
  archived: boolean;
  sort_order: number;
  note?: string;
  created_at: ISODateTime;
  updated_at: ISODateTime;
}

// ---------- 公章 ----------

export interface Seal {
  id: string;
  party_id?: string;
  name: string;
  /** 相对数据目录的图片路径；图片本体不进数据库。 */
  asset_path: string;
  sha256: string;
  mime: string;
  width_px: number;
  height_px: number;
  note?: string;
  contract_count: number;
  created_at: ISODateTime;
  updated_at: ISODateTime;
}

// ---------- 模板 ----------

export type TemplateKind = "html" | "pdf";
export type PlacementType = "field" | "signature" | "seal";
export type PlacementParty = "party_a" | "party_b";
export type SealMode = "contract" | "seal";

/** PDF 模板上的一个标定框；坐标为 PDF 点，从页面左上角起算。 */
export interface Placement {
  id: string;
  /** 1 起的页码。 */
  page: number;
  x: number;
  y: number;
  w: number;
  h: number;
  type: PlacementType;
  /** 字段绑定的值键，如 contract.contract_no / party_b.name。 */
  field?: string;
  party?: PlacementParty;
  seal_mode?: SealMode;
  seal_id?: string;
  /** 0 表示默认字号（10.5pt）。 */
  font_size?: number;
  /** left（默认）/ center / right。 */
  align?: string;
}

export interface Template {
  id: string;
  name: string;
  version: number;
  kind?: TemplateKind;
  html?: string;
  variables: string[];
  is_active: boolean;
  /** 原始 .docx 资产路径（相对数据目录）。 */
  source_path?: string;
  /** 转换后的模板 PDF 资产路径（相对数据目录）。 */
  pdf_path?: string;
  page_count?: number;
  /** word | libreoffice。 */
  converter?: string;
  source_sha256?: string;
  pdf_sha256?: string;
  placements?: Placement[];
  created_at: ISODateTime;
  updated_at: ISODateTime;
}

/** 模板家族的使用范围（model.TemplateUsage）。 */
export interface TemplateUsage {
  template_id: string;
  name: string;
  version: number;
  latest_version: number;
  family_versions: number;
  version_contracts: number;
  family_contracts: number;
  other_version_contracts: number;
  documents: number;
}

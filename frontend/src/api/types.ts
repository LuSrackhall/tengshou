// 腾手 App API 契约类型（阶段 0 首批）。
//
// 来源（与 Go 侧逐字段核对）：
//   internal/model/model.go
//   internal/httpapi/appapi.go（bootstrap）
//   internal/httpapi/onboarding.go
//   internal/service/onboarding.go
//
// 契约变更时先改这份类型，再改调用点；禁止用 any 兜底。
// Go 的 int / float64 对应 number；time.Time 对应 RFC3339 字符串。

export type ISODateTime = string;

// ---------- 通用 ----------

export interface List<T> {
  items: T[];
  total: number;
  limit: number;
  offset: number;
}

export interface ApiErrorBody {
  error: {
    code: string;
    message: string;
  };
}

export interface Stats {
  parties: number;
  contracts: number;
  templates: number;
  signatures: number;
  seals: number;
  documents: number;
  fields: number;
}

export interface YearCount {
  year: number;
  count: number;
}

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
  created_at: ISODateTime;
  updated_at: ISODateTime;
}

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

// ---------- 新手引导 ----------

export type OnboardingStepID =
  | "template_import"
  | "template_calibrate"
  | "sample_preview"
  | "trial_pdf"
  | "contract_create";

/** 五步引导的一行；数据驱动的步骤每次读取时重算。 */
export interface OnboardingStep {
  id: OnboardingStepID;
  label: string;
  done: boolean;
  done_at?: string;
  detail?: string;
  action: OnboardingStepID;
  /** true 表示完成状态来自数据库，false 表示来自 config.json 记录。 */
  data_backed: boolean;
}

export interface OnboardingFacts {
  templates: number;
  calibrated_templates: number;
  contracts: number;
  parties: number;
  fields: number;
  active_template_id?: string;
  active_template_name?: string;
  active_template_version?: number;
  active_template_placements: number;
}

export interface Onboarding {
  /** 数据目录里既无模板也无合同时为 true。 */
  first_run: boolean;
  completed: boolean;
  skipped: boolean;
  completed_at?: string;
  skipped_at?: string;
  show_guide: boolean;
  steps: OnboardingStep[];
  facts: OnboardingFacts;
}

// ---------- 引导接口（GET /app/api/v1/bootstrap） ----------

export interface Bootstrap {
  version: string;
  schema_version: number;
  operator: string;
  current_year: number;
  years: YearCount[];
  stats: Stats;
  mcp_write_enabled: boolean;
  onboarding: Onboarding;
  now: ISODateTime;
}

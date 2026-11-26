// 通用 API 契约：分页、错误信封、列表查询、审计/变更记录。
//
// 来源（与 Go 侧逐字段核对）：
//   internal/model/model.go          List / Stats / YearCount / ChangeLog / FieldChange
//   internal/httpapi/handlers.go     writeError 的 {"error":{code,message}} 信封
//   internal/service/service.go      FieldError / ValidationErrors
//   internal/httpapi/appapi.go       filter() 支持的 list 查询参数

export type ISODateTime = string;

/** 分页集合信封（model.List[T]）。 */
export interface List<T> {
  items: T[];
  total: number;
  limit: number;
  offset: number;
}

/** 校验错误里的单个字段问题（service.FieldError）。 */
export interface ApiFieldError {
  field: string;
  label: string;
  message: string;
}

/** 错误信封；fields 只在 422 校验错误时出现。 */
export interface ApiErrorBody {
  error: {
    code: string;
    message: string;
    fields?: ApiFieldError[];
  };
}

/** {"item": T} 的最简包装。 */
export interface ItemResponse<T> {
  item: T;
}

/** 保存类接口的 {"item": T, "warnings": string[]} 包装。 */
export interface ItemWarningsResponse<T> {
  item: T;
  warnings: string[];
}

/** 删除类接口统一返回 {"deleted": true}。 */
export interface DeletedResponse {
  deleted: boolean;
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

/** 列表接口共用参数（filter()：limit / offset / q）。 */
export type ListQuery = {
  limit?: number;
  offset?: number;
  q?: string;
};

/** 引导自检 / 模板检查共用的状态档位。 */
export type CheckStatus = "ok" | "warn" | "error";

// ---------- 变更记录（审计） ----------

/** 变更实体清单：运行时过滤（SSE/筛选项）与类型共用的单一来源。 */
export const CHANGE_ENTITIES = [
  "contract",
  "party",
  "asset",
  "field",
  "seal",
  "template",
  "signature",
  "backup",
  "settings",
  "import",
] as const;

export type ChangeEntity = (typeof CHANGE_ENTITIES)[number];

export type ChangeAction =
  | "create"
  | "update"
  | "delete"
  | "archive"
  | "import"
  | "export"
  | "restore"
  | "generate";

/** 变更来源筛选：agent 为 MCP 写入，human 为人工操作。 */
export type ChangeSource = "agent" | "human";

/** 一条字段级差异（model.FieldChange）。 */
export interface FieldChange {
  field: string;
  label: string;
  old: string;
  new: string;
}

/** 变更记录行（model.ChangeLog，append-only）。 */
export interface ChangeLog {
  id: string;
  entity: ChangeEntity;
  entity_id: string;
  action: ChangeAction;
  actor: string;
  summary: string;
  changes: FieldChange[];
  created_at: ISODateTime;
}

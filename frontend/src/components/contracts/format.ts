// 合同域的展示格式化：与存量 app.js 的同名逻辑保持逐字一致。
// 迁移只换渲染框架，不改文案、金额/日期格式与状态标签。

import type {
  Contract,
  DocumentKind,
  DocumentStatus,
  FieldDefinition,
  Party,
} from "../../api/types";

/** 变更动作标签（app.js ACTION_LABELS，保留完整表）。 */
export const ACTION_LABELS: Record<string, string> = {
  create: "新建",
  update: "修改",
  delete: "删除",
  archive: "归档",
  import: "导入",
  export: "导出",
  restore: "恢复",
  generate: "生成",
};

/** MCP 写入使用的操作者标识（service.MCPActor）。 */
export const MCP_ACTOR = "tengshou-mcp";

export function statusLabel(status: string): string {
  return status === "active" ? "生效" : status === "archived" ? "归档" : "草稿";
}

export function money(value: unknown): string {
  const amount = Number(value || 0);
  return (
    "¥" +
    amount.toLocaleString("zh-CN", {
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    })
  );
}

/** 与 app.js fmtDate 一致：无效日期原样返回。 */
export function fmtDate(iso: string | undefined): string {
  if (!iso) {
    return "";
  }
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) {
    return iso;
  }
  return date.toLocaleString("zh-CN", { hour12: false });
}

/** 动态字段值展示（app.js formatDynamic）。 */
export function formatDynamic(field: FieldDefinition, value: unknown): string {
  if (value === null || value === undefined || value === "") {
    return "—";
  }
  switch (field.type) {
    case "money":
      return money(value);
    case "number":
      return Number(value).toLocaleString("zh-CN", { maximumFractionDigits: 6 });
    case "bool":
      return value === true ? "是" : value === false ? "否" : String(value);
    default:
      return String(value);
  }
}

/** 相对方名称回显；找不到时按迁移前的行为返回空串。 */
export function partyName(parties: Party[], id: string | undefined): string {
  if (!id) {
    return "";
  }
  const party = parties.find((item) => item.id === id);
  return party ? party.name : "";
}

/** 生成类型标签（app.js generationKindLabel）。 */
export function generationKindLabel(kind: DocumentKind): string {
  return kind === "regenerate" ? "整改重出" : "初次生成";
}

/** 生成状态标签（app.js generationStatusLabel）。 */
export function generationStatusLabel(status: DocumentStatus): string {
  return status === "failed" ? "失败" : "成功";
}

/** 生成记录的内容版本摘要（app.js shortDigest）。 */
export function shortDigest(sha: string | undefined): string {
  return String(sha ?? "").slice(0, 12);
}

/** 台账单元格：内置列走一级字段，其余读 data（app.js contractFieldValue）。 */
export interface ContractCellValue {
  text: string;
  mono?: boolean;
  money?: boolean;
  wrap?: boolean;
  badge?: string;
}

export function contractFieldValue(
  contract: Contract,
  field: FieldDefinition,
  parties: Party[],
): ContractCellValue {
  switch (field.key) {
    case "contract_no":
      return { text: contract.contract_no, mono: true };
    case "year":
      return { text: String(contract.year || "") };
    case "title":
      return { text: contract.title, wrap: true };
    case "type":
      return { text: contract.type || "—" };
    case "party_b":
      return { text: partyName(parties, contract.party_b_id) || "—" };
    case "amount":
      return { text: money(contract.amount), money: true };
    case "signed_on":
      return { text: contract.signed_on || "—" };
    case "valid_from":
      return { text: contract.valid_from || "—" };
    case "valid_to":
      return { text: contract.valid_to || "—" };
    case "status":
      return { badge: contract.status, text: statusLabel(contract.status) };
  }
  const value = contract.data ? contract.data[field.key] : null;
  return { text: formatDynamic(field, value) };
}

/**
 * 合同表单里的模板占位提示（app.js templateHint 的 code 部分）。
 * 只有出现在模板里的合同字段才展示；其余返回空串。
 */
export function templateHintCode(field: FieldDefinition): string {
  if (!field.in_template || field.entity !== "contract") {
    return "";
  }
  const core: Record<string, string> = {
    contract_no: ".Contract.ContractNo",
    year: ".Contract.Year",
    title: ".Contract.Title",
    type: ".Contract.Type",
    amount: ".Contract.Amount",
    signed_on: ".Contract.SignedOn",
    valid_from: ".Contract.ValidFrom",
    valid_to: ".Contract.ValidTo",
    status: ".Contract.Status",
  };
  if (core[field.key]) {
    return `{{${core[field.key]}}}`;
  }
  if (field.key === "party_b") {
    return "{{.PartyB.Name}}";
  }
  return `{{.Data.${field.key}}}`;
}

export function isAgentActor(actor: string): boolean {
  return actor === MCP_ACTOR;
}

export function actorLabel(actor: string): string {
  return isAgentActor(actor) ? "Agent（MCP）" : "人工";
}

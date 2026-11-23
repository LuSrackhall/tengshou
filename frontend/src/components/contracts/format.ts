// 合同域的展示格式化：共享函数已集中到 ui/format，这里重导出原有名字并
// 保留本域专用的单元格/占位逻辑；文案与格式不变。

import type {
  Contract,
  DocumentKind,
  DocumentStatus,
  FieldDefinition,
  Party,
} from "../../api/types";
import { formatDynamic, money, statusLabel } from "../../ui/format";

export { formatDynamic, money, statusLabel };

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

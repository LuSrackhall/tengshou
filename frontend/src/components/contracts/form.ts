// 合同表单的取值与提交，逐条对应存量 app.js 的
// contractControlValue / collectContractInput / openContractCreateForm 的 seed 构造：
//   - 控件值统一按字符串处理（input / textarea / select 的 value 语义）；
//   - 新建时内置「年度」取当前年度、「状态」默认草稿；
//   - 续签草稿：带出原合同值，编号/日期/状态重置（REUSE-7 语义不变）；
//   - 提交：内置字段直接映射到一级字段，自定义字段进 data，空串照旧提交。

import type { ContractInput, FieldDefinition, RenewalDraft } from "../../api/types";
import { statusLabel } from "./format";

/** 表单初值与提交所需的合同片段；Contract 与续签草稿都满足。 */
export interface ContractSeed {
  contract_no?: string;
  year?: number;
  title?: string;
  type?: string;
  party_a_id?: string;
  party_b_id?: string;
  template_id?: string;
  amount?: number;
  signed_on?: string;
  valid_from?: string;
  valid_to?: string;
  status?: string;
  data?: Record<string, unknown>;
}

/** 续签草稿 → 新建表单的 seed（与 app.js openContractCreateForm 一致）。 */
export function renewalSeed(draft: RenewalDraft): ContractSeed {
  const input = draft.input;
  return {
    contract_no: "",
    year: input.year,
    title: input.title,
    type: input.type,
    party_a_id: input.party_a_id ?? "",
    party_b_id: input.party_b_id ?? "",
    template_id: input.template_id ?? "",
    amount: input.amount ?? 0,
    signed_on: "",
    valid_from: "",
    valid_to: "",
    status: "draft",
    data: input.data ?? {},
  };
}

/** 存储值（bool 为 true/false）转成 select 能匹配的「是 / 否」。 */
function rawToControlValue(field: FieldDefinition, value: unknown): string {
  if (value === null || value === undefined) {
    return "";
  }
  if (field.type === "bool") {
    if (value === true || value === "是") {
      return "是";
    }
    if (value === false || value === "否") {
      return "否";
    }
    return "";
  }
  return String(value);
}

/** 编辑/新建时的控件初始值（app.js contractControlValue）。 */
export function contractControlValue(
  field: FieldDefinition,
  seed: ContractSeed | null,
  partyBName: string,
  currentYear: number | null,
): string {
  if (!seed) {
    if (field.built_in) {
      if (field.key === "year") {
        return currentYear === null ? "" : String(currentYear);
      }
      if (field.key === "status") {
        return "草稿";
      }
      if (field.key === "party_b") {
        return partyBName || "";
      }
      return field.default_value ?? "";
    }
    return field.default_value ?? "";
  }
  switch (field.key) {
    case "contract_no":
      return seed.contract_no ?? "";
    case "year":
      return seed.year === undefined ? "" : String(seed.year);
    case "title":
      return seed.title ?? "";
    case "type":
      return seed.type ?? "";
    case "party_b":
      return partyBName || "";
    case "amount":
      // 与迁移前 contract.amount || "" 一致：0 也回填空串（审计 RR-1）。
      return seed.amount ? String(seed.amount) : "";
    case "signed_on":
      return seed.signed_on ?? "";
    case "valid_from":
      return seed.valid_from ?? "";
    case "valid_to":
      return seed.valid_to ?? "";
    case "status":
      return statusLabel(seed.status ?? "");
  }
  return rawToControlValue(field, seed.data ? seed.data[field.key] : undefined);
}

/** 打开表单时补充尚无值的字段；已有输入保持不变。 */
export function syncContractValues(
  values: Record<string, string>,
  fields: FieldDefinition[],
  seed: ContractSeed | null,
  partyBName: string,
  currentYear: number | null,
): void {
  for (const field of fields) {
    if (!(field.key in values)) {
      values[field.key] = contractControlValue(field, seed, partyBName, currentYear);
    }
  }
}

/**
 * 表单值转 ContractInput（app.js collectContractInput）。
 * party_a_id / template_id 沿用 seed；party_b 名称与档案一致时才回填 party_b_id。
 */
export function collectContractInput(
  fields: FieldDefinition[],
  values: Record<string, string>,
  seed: ContractSeed | null,
  partyBID: string,
  partyBName: string,
): ContractInput {
  const input: ContractInput = {
    data: {},
    party_a_id: seed?.party_a_id ?? "",
    template_id: seed?.template_id ?? "",
    year: 0,
    title: "",
  };
  const data: Record<string, unknown> = {};
  for (const field of fields) {
    const value = (values[field.key] ?? "").trim();
    if (!field.built_in) {
      data[field.key] = value;
      continue;
    }
    switch (field.key) {
      case "contract_no":
        input.contract_no = value;
        break;
      case "year":
        input.year = Number(value) || 0;
        break;
      case "title":
        input.title = value;
        break;
      case "type":
        input.type = value;
        break;
      case "party_b":
        input.party_b = value;
        break;
      case "amount":
        input.amount = Number(value) || 0;
        break;
      case "signed_on":
        input.signed_on = value;
        break;
      case "valid_from":
        input.valid_from = value;
        break;
      case "valid_to":
        input.valid_to = value;
        break;
      case "status":
        input.status = value as ContractInput["status"];
        break;
    }
  }
  input.data = data;
  if (partyBID && input.party_b && input.party_b === partyBName) {
    input.party_b_id = partyBID;
  }
  return input;
}

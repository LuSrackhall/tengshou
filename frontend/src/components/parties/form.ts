// 相对方表单的取值与提交，逐条对应存量 app.js 的
// partyControlValue / collectPartyInput：
//   - 控件值统一按字符串处理（input / textarea / select 的 value 语义）；
//   - 新建时内置「类型」默认「个人」，其余取字段默认值；
//   - 提交：内置字段直接映射到一级字段，自定义字段进 data，空串照旧提交
//     （服务端负责裁剪与校验，行为与迁移前一致）。

import type { FieldDefinition, Party, PartyInput, PartyKind } from "../../api/types";
import { kindLabel } from "./format";

/** 编辑/新建时的控件初始值。 */
export function partyControlValue(field: FieldDefinition, party: Party | null): string {
  if (!party) {
    return field.built_in && field.key === "kind" ? "个人" : (field.default_value ?? "");
  }
  switch (field.key) {
    case "name":
      return party.name;
    case "kind":
      return kindLabel(party.kind);
    case "id_number":
      return party.id_number ?? "";
    case "phone":
      return party.phone ?? "";
    case "address":
      return party.address ?? "";
    case "note":
      return party.note ?? "";
  }
  const value = party.data ? party.data[field.key] : undefined;
  return rawToControlValue(field, value);
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

/**
 * 表单值转 PartyInput。服务端 normalizeKind 接受「个人/单位」与
 * person/organization，并同样把空值归为 person；这里提交机器值以满足
 * 契约类型，存储结果与迁移前完全相同。
 */
export function collectPartyInput(
  fields: FieldDefinition[],
  values: Record<string, string>,
): PartyInput {
  const input: PartyInput = { kind: "person", name: "", data: {} };
  const data: Record<string, unknown> = {};
  for (const field of fields) {
    const value = (values[field.key] ?? "").trim();
    if (!field.built_in) {
      data[field.key] = value;
      continue;
    }
    switch (field.key) {
      case "name":
        input.name = value;
        break;
      case "kind":
        input.kind = kindValue(value);
        break;
      case "id_number":
        input.id_number = value;
        break;
      case "phone":
        input.phone = value;
        break;
      case "address":
        input.address = value;
        break;
      case "note":
        input.note = value;
        break;
    }
  }
  input.data = data;
  return input;
}

function kindValue(label: string): PartyKind {
  return label.trim() === "单位" ? "organization" : "person";
}

/** 打开表单时补充尚无值的字段；已有输入保持不变（外部定义变更不覆盖）。 */
export function syncControlValues(
  values: Record<string, string>,
  fields: FieldDefinition[],
  party: Party | null,
): void {
  for (const field of fields) {
    if (!(field.key in values)) {
      values[field.key] = partyControlValue(field, party);
    }
  }
}

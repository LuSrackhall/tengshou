// 相对方域的展示格式化：与存量 app.js 的同名逻辑保持逐字一致。
// 迁移只换渲染框架，不改文案、金额/日期格式与状态标签。

import type { FieldDefinition, Party, PartyKind } from "../../api/types";

export function statusLabel(status: string): string {
  return status === "active" ? "生效" : status === "archived" ? "归档" : "草稿";
}

export function kindLabel(kind: PartyKind | string): string {
  return kind === "organization" ? "单位" : "个人";
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

/** 台账单元格值：内置列走一级字段，其余读 data（app.js partyFieldValue）。 */
export function partyFieldValue(party: Party, field: FieldDefinition): string {
  switch (field.key) {
    case "name":
      return party.name;
    case "kind":
      return kindLabel(party.kind);
    case "id_number":
      return party.id_number || "—";
    case "phone":
      return party.phone || "—";
    case "address":
      return party.address || "—";
    case "note":
      return party.note || "—";
  }
  const value = party.data ? party.data[field.key] : null;
  return formatDynamic(field, value);
}

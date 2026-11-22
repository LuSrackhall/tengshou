// 相对方域的展示格式化：与存量 app.js 的同名逻辑保持逐字一致。
// 迁移只换渲染框架，不改文案、金额/日期格式与状态标签。

import type { FieldDefinition, Party, PartyKind } from "../../api/types";

/** 变更动作标签（app.js ACTION_LABELS 的相对方子集，保留完整表）。 */
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

export function kindLabel(kind: PartyKind | string): string {
  return kind === "organization" ? "单位" : "个人";
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

export function isAgentActor(actor: string): boolean {
  return actor === MCP_ACTOR;
}

export function actorLabel(actor: string): string {
  return isAgentActor(actor) ? "Agent（MCP）" : "人工";
}

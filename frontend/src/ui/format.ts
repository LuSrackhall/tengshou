// 各域（变更记录、数据安全、设置、合同/相对方审计历史等）共用的展示格式化。
// 与迁移前 app.js 的同名逻辑逐字一致：只换渲染框架，不改文案与格式。

import type { ChangeLog, FieldDefinition, PartyKind } from "../api/types";

export const ENTITY_LABELS: Record<string, string> = {
  contract: "合同",
  party: "相对方",
  asset: "房产",
  field: "字段",
  seal: "公章",
  template: "模板",
  signature: "签名",
  backup: "备份",
  settings: "设置",
  import: "导入",
  migration: "迁移",
};

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

export function isAgentActor(actor: string): boolean {
  return actor === MCP_ACTOR;
}

export function isAgentEntry(entry: ChangeLog): boolean {
  return isAgentActor(entry.actor);
}

export function actorLabel(actor: string): string {
  return isAgentActor(actor) ? "Agent（MCP）" : "人工";
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

// 以下展示格式化原在各域 format.ts 逐字重复，集中于此作为唯一事实源；
// 与迁移前 app.js 的同名逻辑逐字一致（不改文案与格式）。

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

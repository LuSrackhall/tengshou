// 外壳域（变更记录、数据安全、设置）共用的展示格式化。
// 与迁移前 app.js 的同名逻辑逐字一致：只换渲染框架，不改文案与格式。

import type { ChangeLog } from "../api/types";

export const ENTITY_LABELS: Record<string, string> = {
  contract: "合同",
  party: "相对方",
  field: "字段",
  seal: "公章",
  template: "模板",
  signature: "签名",
  backup: "备份",
  settings: "设置",
  import: "导入",
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

export function isAgentEntry(entry: ChangeLog): boolean {
  return entry.actor === MCP_ACTOR;
}

export function actorLabel(actor: string): string {
  return actor === MCP_ACTOR ? "Agent（MCP）" : "人工";
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

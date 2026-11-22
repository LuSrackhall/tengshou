// 字段管理域的展示常量：与存量 app.js 的 TYPE_LABELS 逐字一致。
// 迁移只换渲染框架，不改类型标签、可选类型集合与实体名称。

import type { FieldEntity, FieldType } from "../../api/types";

/** 字段类型的中文标签（app.js TYPE_LABELS）。 */
export const FIELD_TYPE_LABELS: Record<FieldType, string> = {
  text: "文本",
  longtext: "长文本",
  number: "数字",
  money: "金额",
  date: "日期",
  select: "单选",
  bool: "是/否",
  image: "图片路径",
  party: "相对方",
  status: "状态",
};

/**
 * 新建/编辑时可选的类型（app.js 在 openFieldForm 里排除了 party/status）：
 * 这两种是内置字段专用类型，仅内置字段编辑时作为当前值展示。
 */
export const USER_FIELD_TYPES: FieldType[] = [
  "text",
  "longtext",
  "number",
  "money",
  "date",
  "select",
  "bool",
  "image",
];

export function entityLabel(entity: FieldEntity): string {
  return entity === "contract" ? "合同" : "相对方";
}

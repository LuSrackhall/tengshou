// Excel 导入对话框的展示格式化（与迁移前 app.js 的文案逐字一致）。

import type {
  ImportPreview,
  ImportResult,
  ImportRowAction,
} from "../../api/types";

export const IMPORT_ACTION_LABELS: Record<string, string> = {
  create: "新增",
  update: "更新",
  unchanged: "不变",
  error: "错误",
  delete: "删除",
  conflict: "冲突",
};

export function importActionLabel(action: ImportRowAction | string): string {
  return IMPORT_ACTION_LABELS[action] ?? action;
}

export function fieldLabelByKey(preview: ImportPreview, key: string): string {
  const field = (preview.fields ?? []).find((item) => item.key === key);
  return field ? field.label : key;
}

export function importResultToast(result: ImportResult): string {
  const parts = [
    `新增 ${result.created || 0} 条`,
    `更新 ${result.updated || 0} 条`,
  ];
  if (result.deleted) {
    parts.push(`删除 ${result.deleted} 条`);
  }
  if (result.skipped) {
    parts.push(`跳过 ${result.skipped} 条`);
  }
  return `导入完成：${parts.join("，")}`;
}

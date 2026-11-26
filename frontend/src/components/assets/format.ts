// 房产档案域的展示格式化：租赁状态与启用状态的文案、面积与选填字段展示。
// 与后端派生语义一致：在租/空置只展示，不在前端推断。

import type { Asset, LeaseStatus } from "../../api/types";

export function leaseStatusLabel(status: LeaseStatus): string {
  return status === "rented" ? "在租" : "空置";
}

export function enabledLabel(enabled: boolean): string {
  return enabled ? "启用" : "停用";
}

/** 面积展示：未填时显示占位符；保留用户输入的小数位。 */
export function areaLabel(area: number | undefined): string {
  if (area === undefined || area === null || Number.isNaN(area)) {
    return "—";
  }
  return `${area.toLocaleString("zh-CN", { maximumFractionDigits: 2 })} ㎡`;
}

/** 选填文本列：空值统一显示 —，与相对方台账一致。 */
export function optionalText(value: string | undefined): string {
  const text = (value ?? "").trim();
  return text === "" ? "—" : text;
}

/** 是否命中的当前搜索/筛选（供空态文案区分「还没有」与「没有匹配」）。 */
export function hasActiveFilter(query: string, status: string): boolean {
  return query.trim() !== "" || status !== "";
}

/** 编辑弹窗标题。 */
export function formTitle(asset: Asset | null): string {
  return asset ? `编辑房产 · ${asset.name}` : "新建房产";
}

// 主 tab 的共享状态与切换入口。
//
// 取代迁移前 app.js 的 switchTab：更新激活态、派发 tengshou:tab-change，
// 已迁移域据此在重新显示时刷新（例如新手引导会重跑自检与模板体检）。
// 引导步骤的深链经 bridge/app.ts 的 switchTab() 落到这里。

import { ref } from "vue";
import { TAB_CHANGE_EVENT } from "./events";

export const currentTab = ref("contracts");

export interface TabDefinition {
  id: string;
  label: string;
}

/** 与迁移前 index.html 的 #tabs 按钮顺序、文案一致。 */
export const TABS: readonly TabDefinition[] = [
  { id: "contracts", label: "合同台账" },
  { id: "onboarding", label: "新手引导" },
  { id: "templates", label: "模板" },
  { id: "seals", label: "公章" },
  { id: "parties", label: "相对方档案" },
  { id: "assets", label: "房产档案" },
  { id: "fields", label: "字段" },
  { id: "changelog", label: "变更记录" },
  { id: "data", label: "数据安全" },
  { id: "settings", label: "设置" },
];

export function switchTab(tab: string): void {
  currentTab.value = tab;
  window.dispatchEvent(
    new CustomEvent(TAB_CHANGE_EVENT, { detail: { tab } }),
  );
}

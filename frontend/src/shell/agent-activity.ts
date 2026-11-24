// 「agent 操作可见层」的共享设置与实体映射（te-h7l.50）。
//
// 交互回放由 components/shell/AgentActivityOverlay.vue 承担：它监听
// shell/realtime.ts 已派发的 tengshou:change，过滤 actor=tengshou-mcp 后
// 切页、移动模拟光标并高亮目标行。本模块只放跨组件共享的两样东西：
//
//   1. 设置页「显示 agent 操作动画」开关（默认开启，localStorage 持久化）；
//   2. 变更实体 → 主 tab 的映射（与设置页/引导页的 tab id 一致）。

import { ref } from "vue";

/** 设置页「显示 agent 操作动画」的 localStorage 键。 */
export const AGENT_ACTIVITY_ANIMATIONS_KEY = "tengshou.agentActivityAnimations";

function readStoredEnabled(): boolean {
  try {
    // 未写入过（首次启动）或存储不可用时按默认开启处理；只有显式关过才为 false。
    return window.localStorage.getItem(AGENT_ACTIVITY_ANIMATIONS_KEY) !== "0";
  } catch {
    return true;
  }
}

/** 是否播放切页/光标/波纹/高亮；关闭后只保留顶部「agent 操作」徽标。 */
export const agentActivityAnimationsEnabled = ref(readStoredEnabled());

/** 更新开关并持久化；localStorage 不可用（隐私模式等）时仅本次会话生效。 */
export function setAgentActivityAnimationsEnabled(enabled: boolean): void {
  agentActivityAnimationsEnabled.value = enabled;
  try {
    window.localStorage.setItem(
      AGENT_ACTIVITY_ANIMATIONS_KEY,
      enabled ? "1" : "0",
    );
  } catch {
    /* 存储不可用：保留内存中的设置 */
  }
}

/**
 * 变更实体 → 主 tab。未列出的实体（seal/template/signature/backup/import）
 * 按需求不在回放范围，只更新徽标、不做切页与光标回放。
 */
export const AGENT_ACTIVITY_TABS: Readonly<Record<string, string>> = {
  contract: "contracts",
  // 预留映射：当前变更词表不会产生 document 实体，本分支暂不会命中。
  document: "contracts",
  party: "parties",
  field: "fields",
  settings: "settings",
};

// 新手引导的展示文案与格式化：与迁移前 app.js 的同名逻辑逐字一致。
// 迁移只换渲染框架，不改文案、步骤顺序、完成判定与日期格式。

import type { Onboarding } from "../../api/types";
import { TABS } from "../../shell/tabs";
import { fmtDate } from "../../ui/format";

/** 自检「前往」按钮的 tab 名称；文案唯一事实源是 shell/tabs.ts 的 TABS。 */
export const TAB_LABELS: Record<string, string> = Object.fromEntries(
  TABS.map((tab) => [tab.id, tab.label]),
);

export function tabLabel(tab: string): string {
  return TAB_LABELS[tab] ?? tab;
}

/** 自检状态文案（app.js CHECK_STATUS_LABELS）。 */
export const CHECK_STATUS_LABELS: Record<string, string> = {
  ok: "通过",
  warn: "需留意",
  error: "需处理",
};

/** 自检状态对应的 badge 类（app.js renderOnboardingCheck）。 */
export function checkBadgeClass(status: string): string {
  if (status === "ok") {
    return "active";
  }
  if (status === "warn") {
    return "draft";
  }
  return "error";
}

/** 迁移前 renderOnboarding 的进度文案。 */
export function progressText(data: Onboarding): string {
  const done = data.steps.filter((step) => step.done).length;
  return `${done} / ${data.steps.length} 步已完成`;
}

/** 迁移前 renderOnboarding 的横幅文案，判定顺序与原文一致。 */
export function bannerText(data: Onboarding): string {
  const steps = data.steps;
  const done = steps.filter((step) => step.done).length;
  if (data.completed) {
    return `引导已完成${data.completed_at ? "（" + fmtDate(data.completed_at) + "）" : ""}；仍可从本页重新查看。`;
  }
  if (data.skipped) {
    return `已跳过引导${data.skipped_at ? "（" + fmtDate(data.skipped_at) + "）" : ""}；需要时点「重新打开引导」。`;
  }
  if (data.first_run) {
    return "这是第一次打开腾手：按下面五步完成第一份模板与试生成。";
  }
  if (steps.length && done === steps.length) {
    return "五步都已完成，可以继续正常使用。";
  }
  return `已完成 ${done} / ${steps.length} 步；点击任一步继续。`;
}

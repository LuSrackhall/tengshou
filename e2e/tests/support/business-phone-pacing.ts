// 手机页操作的人速节奏（te-fp48w）。
//
// 用户要在主录像的画中画里看清手机上的录入过程，报告模式（非教程）下手机侧
// 片段需要足够长（目标 ≥8 秒）：填写逐字输入、选类型/上传后停顿、结果页停留。
// 教程录制模式已有 launchOptions.slowMo=500 的全局慢速（8.9s / 15.2s），这里
// 全部不生效，避免重复减速；业务断言与操作顺序不变。
import type { Locator, Page } from "@playwright/test";
import { isTutorialMode } from "./tutorial-visuals";

/** 报告模式启用人速节奏；教程模式由 slowMo 负责放慢。 */
export function phonePacingEnabled(): boolean {
  return !isTutorialMode();
}

/** 人速停顿；教程模式直接返回。 */
export async function phonePause(page: Page, ms: number): Promise<void> {
  if (!phonePacingEnabled()) {
    return;
  }
  await page.waitForTimeout(ms);
}

/** 像人一样逐字输入；教程模式退回 fill（slowMo 已让操作可见）。 */
export async function phoneType(
  locator: Locator,
  text: string,
  delayMs = 90,
): Promise<void> {
  if (!phonePacingEnabled()) {
    await locator.fill(text);
    return;
  }
  await locator.fill("");
  await locator.click();
  await locator.pressSequentially(text, { delay: delayMs });
}

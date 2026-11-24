// 用例共用的小工具：控制台错误收集与业务步骤截图。
import type { Page, TestInfo } from "@playwright/test";

/** 收集页面的 console error 与未捕获异常；用例末尾断言为空。 */
export function trackConsoleErrors(page: Page): string[] {
  const errors: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error") {
      errors.push(message.text());
    }
  });
  page.on("pageerror", (error) => {
    errors.push(`未捕获异常：${error.message}`);
  });
  return errors;
}

/** 关键业务步骤截图，直接附到 Playwright HTML 报告。 */
export async function shot(
  page: Page,
  testInfo: TestInfo,
  name: string,
): Promise<void> {
  await testInfo.attach(name, {
    body: await page.screenshot({ fullPage: true }),
    contentType: "image/png",
  });
}

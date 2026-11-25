// 业务场景动作共用的小工具：page.request 需要绝对地址，用当前页面
// origin 拼 app API 路径。
import type { Page } from "@playwright/test";

export function appUrl(page: Page, path: string): string {
  return new URL(path, page.url()).toString();
}

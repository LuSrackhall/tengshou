// 场景 B：MCP 跨层联动（agent 操作可见层）。
//
// 业务含义：客户抱怨「看不到 agent 在操作」。本场景在页面打开时，以真实
// stdio 管道调用 tengshou_create_field（--api-url 指向同一 serve），断言：
//   1. 界面自动切到「字段」页；
//   2. 新字段行可见；
//   3. agent 操作可见层出现——顶部「agent 操作」徽标、假光标滑入、目标行高亮。
import { expect, test } from "./support/app-fixture";
import { callMCPTool } from "./support/mcp";
import { shot, trackConsoleErrors } from "./support/ui";

test.describe("场景 B：MCP 跨层联动", () => {
  test("MCP 新建字段时界面自动切页、高亮并显示 agent 操作可见层", async ({
    app,
    page,
  }, testInfo) => {
    const consoleErrors = trackConsoleErrors(page);
    const stamp = Date.now().toString(36);
    const fieldLabel = `MCP 字段 ${stamp}`;
    const fieldKey = `mcp_field_${stamp}`;

    await test.step("打开界面并等待实时事件通道就绪", async () => {
      const response = await page.goto(`${app.baseURL}/app/`, {
        waitUntil: "domcontentloaded",
      });
      expect(response?.status()).toBe(200);
      await expect(page.locator("#tabs")).toBeVisible();

      // 先停在合同页，稍后验证 agent 写入会让界面自动切页。
      await page.locator('#tabs button[data-tab="contracts"]').click();
      await expect(
        page.locator('#tabs button[data-tab="contracts"]'),
      ).toHaveClass(/active/);

      // SSE 或 3 秒轮询任一就绪即可；避免写入落在事件流建立之前。
      await expect(page.locator(".live-status")).toHaveClass(/sse|poll/, {
        timeout: 20_000,
      });
      await shot(page, testInfo, "01-等待 agent 操作");
    });

    await test.step("通过 stdio 调用 tengshou_create_field", async () => {
      const text = await callMCPTool(app, "tengshou_create_field", {
        entity: "contract",
        key: fieldKey,
        label: fieldLabel,
        type: "text",
        in_ledger: true,
        in_template: true,
      });
      expect(text).toContain(fieldKey);
    });

    await test.step("界面自动切到字段页并播放可见层", async () => {
      await expect(page.locator('#tabs button[data-tab="fields"]')).toHaveClass(
        /active/,
        { timeout: 15_000 },
      );

      const row = page.locator(
        `#view-fields tr[data-id]:has-text("${fieldLabel}")`,
      );
      await expect(row).toBeVisible({ timeout: 15_000 });

      const badge = page.locator(".agent-activity-badge");
      await expect(badge).toBeVisible();
      await expect(badge).toContainText("agent 操作");
      await expect(badge).toContainText(fieldLabel);

      // 假光标滑入后 opacity=1；随后目标行高亮 2.5 秒。
      await expect(page.locator(".agent-activity-cursor")).toHaveCSS(
        "opacity",
        "1",
        { timeout: 15_000 },
      );
      await expect(row).toHaveClass(/agent-activity-highlight/, {
        timeout: 15_000,
      });
      await shot(page, testInfo, "02-agent-操作可见层");
    });

    expect(
      consoleErrors,
      `浏览器控制台出现错误：\n${consoleErrors.join("\n")}`,
    ).toEqual([]);
  });
});

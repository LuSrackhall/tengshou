// 场景 A：合同主流程（最小但完整可用）。
//
// 业务含义：用户从「导入自己的模板」开始，在界面里新建字段、新建相对方档案、
// 新建并保存合同草稿；服务端预检通过后预览确认版式，生成 PDF 并下载。
// 全程只驱动发布形态的 tengshou-cli serve（go:embed 的界面资源），
// 同时断言浏览器控制台没有错误。
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { expect, test } from "./support/app-fixture";
import { repoRoot } from "./support/app";
import { shot, trackConsoleErrors } from "./support/ui";

test.describe("场景 A：合同主流程", () => {
  test("导入模板 → 新建字段 → 新建档案 → 新建合同草稿 → 预检 → 生成并下载 PDF", async ({
    app,
    page,
  }, testInfo) => {
    const consoleErrors = trackConsoleErrors(page);
    const stamp = Date.now().toString(36);
    const templateName = `E2E 模板 ${stamp}`;
    const fieldLabel = `E2E 平方数 ${stamp}`;
    const fieldKey = `e2e_area_${stamp}`;
    const partyName = `E2E 相对方 ${stamp}`;
    const contractTitle = `E2E 房屋租赁合同 ${stamp}`;

    await test.step("打开发布形态界面", async () => {
      const response = await page.goto(`${app.baseURL}/app/`, {
        waitUntil: "domcontentloaded",
      });
      expect(response?.status()).toBe(200);
      await expect(page.locator("#tabs")).toBeVisible();
      await expect(page.locator(".live-status")).toBeVisible();
      await shot(page, testInfo, "01-界面已打开");
    });

    let templateId = "";
    await test.step("准备可生成的模板：导入 PDF 模板", async () => {
      await page.locator('#tabs button[data-tab="templates"]').click();
      await expect(page.locator("#view-templates")).toHaveClass(/active/);
      await page
        .locator('#view-templates input[type="file"]')
        .setInputFiles(join(repoRoot(), "e2e", "fixtures", "template.pdf"));

      const importDialog = page
        .getByRole("dialog")
        .filter({ has: page.getByRole("heading", { name: /导入模板/ }) });
      await expect(importDialog).toBeVisible();
      await importDialog.locator("#tplImportName").fill(templateName);
      await importDialog.getByRole("button", { name: "导入", exact: true }).click();
      await expect(importDialog).toBeHidden();

      // 导入成功后自动打开标定编辑器；本场景只验证「可生成」，直接关闭。
      const editorClose = page.locator("#tplCloseBtn");
      await expect(editorClose).toBeVisible({ timeout: 30_000 });
      await editorClose.click();

      const templateRow = page.locator(
        `#templateTable tr[data-id]:has-text("${templateName}")`,
      );
      await expect(templateRow).toBeVisible();
      templateId = (await templateRow.getAttribute("data-id")) ?? "";
      expect(templateId).not.toBe("");
      await shot(page, testInfo, "02-模板已导入");
    });

    await test.step("界面新建字段", async () => {
      await page.locator('#tabs button[data-tab="fields"]').click();
      await expect(page.locator("#view-fields")).toHaveClass(/active/);
      await page
        .locator("#view-fields")
        .getByRole("button", { name: "新建字段", exact: true })
        .click();

      const fieldDialog = page
        .getByRole("dialog")
        .filter({ has: page.getByRole("heading", { name: "新建字段", exact: true }) });
      await expect(fieldDialog).toBeVisible();
      await fieldDialog.locator("#fieldLabel").fill(fieldLabel);
      await fieldDialog.locator("#fieldKey").fill(fieldKey);
      await fieldDialog
        .getByRole("button", { name: "保存", exact: true })
        .click();
      await expect(fieldDialog).toBeHidden();

      await expect(
        page.locator(`#view-fields tr[data-id]:has-text("${fieldLabel}")`),
      ).toBeVisible();
      await shot(page, testInfo, "03-字段已新建");
    });

    await test.step("界面新建相对方档案", async () => {
      await page.locator('#tabs button[data-tab="parties"]').click();
      await expect(page.locator("#view-parties")).toHaveClass(/active/);
      await page.locator("#newPartyBtn").click();

      const partyDialog = page
        .getByRole("dialog")
        .filter({ has: page.getByRole("heading", { name: "新建相对方", exact: true }) });
      await expect(partyDialog).toBeVisible();
      await partyDialog.locator("#f_party_name").fill(partyName);
      await partyDialog.locator("#f_party_phone").fill("13800000000");
      await partyDialog
        .getByRole("button", { name: "保存", exact: true })
        .click();
      await expect(partyDialog).toBeHidden();

      await expect(
        page.locator(`#partyTable tr[data-id]:has-text("${partyName}")`),
      ).toBeVisible();
      await shot(page, testInfo, "04-档案已新建");
    });

    let contractId = "";
    await test.step("界面新建合同并保存草稿", async () => {
      await page.locator('#tabs button[data-tab="contracts"]').click();
      await expect(page.locator("#view-contracts")).toHaveClass(/active/);
      await page.locator("#newContractBtn").click();

      const contractDialog = page
        .getByRole("dialog")
        .filter({ has: page.getByRole("heading", { name: "新建合同", exact: true }) });
      await expect(contractDialog).toBeVisible();
      await expect(contractDialog.locator("#f_contract_status")).toHaveValue("草稿");
      await contractDialog.locator("#f_contract_title").fill(contractTitle);
      await contractDialog.locator("#f_contract_party_b").fill(partyName);
      await contractDialog
        .getByRole("button", { name: "保存", exact: true })
        .click();
      await expect(contractDialog).toBeHidden();

      const contractRow = page.locator(
        `#contractTable tr[data-id]:has-text("${contractTitle}")`,
      );
      await expect(contractRow).toBeVisible();
      contractId = (await contractRow.getAttribute("data-id")) ?? "";
      expect(contractId).not.toBe("");
      await shot(page, testInfo, "05-合同已保存为草稿");
    });

    await test.step("预检通过（服务端就绪检查）", async () => {
      const response = await page.request.post(
        `${app.baseURL}/app/api/v1/templates/${templateId}/preflight`,
        {
          headers: { "X-Tengshou-App": "1" },
          data: { contract_id: contractId },
        },
      );
      expect(response.ok()).toBeTruthy();
      const preflight = (await response.json()) as {
        ready?: boolean;
        status?: string;
        issues?: unknown[];
      };
      expect(
        preflight.ready,
        `预检未通过：${JSON.stringify(preflight.issues ?? [])}`,
      ).toBe(true);
      expect(preflight.status).toBe("ready");
    });

    await test.step("在生成面板预览、生成 PDF 并下载", async () => {
      const contractRow = page.locator(
        `#contractTable tr[data-id]:has-text("${contractTitle}")`,
      );
      await contractRow.click();
      const editDialog = page
        .getByRole("dialog")
        .filter({ has: page.getByRole("heading", { name: /编辑合同/ }) });
      await expect(editDialog).toBeVisible();

      const panel = page.locator("#contractGeneration");
      await expect(panel.locator("#genPreview")).toBeEnabled({ timeout: 30_000 });
      await expect(panel.locator("#genTemplate option:checked")).toContainText(
        templateName,
      );

      await panel.locator("#genPreview").click();
      await expect(page.locator(".pdf-overlay")).toBeVisible({ timeout: 60_000 });
      await expect(panel.locator("#genStatus")).toContainText("预览已完成");
      await page.locator("#pdfOverlayClose").click();
      await expect(page.locator(".pdf-overlay")).toHaveCount(0);
      await shot(page, testInfo, "06-预览通过");

      await expect(panel.locator("#genGenerate")).toBeEnabled();
      await panel.locator("#genGenerate").click();
      await expect(panel.locator("#genStatus")).toContainText("已初次生成 PDF", {
        timeout: 60_000,
      });
      await expect(panel.locator("#genHistory")).toContainText("成功");

      const downloadButton = panel
        .locator("#genHistory")
        .getByRole("button", { name: "下载 PDF" })
        .first();
      await expect(downloadButton).toBeVisible({ timeout: 30_000 });
      const downloadPromise = page.waitForEvent("download");
      await downloadButton.click();
      const download = await downloadPromise;
      const savedPath = testInfo.outputPath("生成的合同.pdf");
      await download.saveAs(savedPath);

      const bytes = await readFile(savedPath);
      expect(bytes.byteLength).toBeGreaterThan(500);
      expect(bytes.subarray(0, 5).toString("latin1")).toBe("%PDF-");
      await testInfo.attach("生成的合同 PDF", {
        path: savedPath,
        contentType: "application/pdf",
      });
      await shot(page, testInfo, "07-PDF-已生成并下载");
    });

    expect(
      consoleErrors,
      `浏览器控制台出现错误：\n${consoleErrors.join("\n")}`,
    ).toEqual([]);
  });
});

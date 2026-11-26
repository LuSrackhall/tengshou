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
  test("导入模板 → 新建字段 → 新建三方档案 → 三方选合同 → 预检 → 生成并下载 PDF", async ({
    app,
    page,
  }, testInfo) => {
    const consoleErrors = trackConsoleErrors(page);
    const stamp = Date.now().toString(36);
    const templateName = `E2E 模板 ${stamp}`;
    const fieldLabel = `E2E 平方数 ${stamp}`;
    const fieldKey = `e2e_area_${stamp}`;
    const partyName = `E2E 相对方 ${stamp}`;
    const partyAName = `E2E 甲方单位 ${stamp}`;
    const assetName = `E2E 房产 ${stamp}`;
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
      // 字段键默认自动生成；本场景绑定固定英文键，展开高级选项手填。
      await fieldDialog.locator("details.advanced-options summary").click();
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
      await partyDialog.locator("#f_party_id_number").fill(`E2E${stamp}`);
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

    await test.step("界面新建甲方（单位）与租赁房产档案", async () => {
      await page.locator('#tabs button[data-tab="parties"]').click();
      await page.locator("#newPartyBtn").click();
      const partyADialog = page
        .getByRole("dialog")
        .filter({ has: page.getByRole("heading", { name: "新建相对方", exact: true }) });
      await expect(partyADialog).toBeVisible();
      await partyADialog.locator("#f_party_kind").selectOption("单位");
      await partyADialog.locator("#f_party_name").fill(partyAName);
      await partyADialog.locator("#f_party_id_number").fill(`E2EA${stamp}`);
      await partyADialog.getByRole("button", { name: "保存", exact: true }).click();
      await expect(partyADialog).toBeHidden();
      await expect(
        page.locator(`#partyTable tr[data-id]:has-text("${partyAName}")`),
      ).toBeVisible();

      await page.locator('#tabs button[data-tab="assets"]').click();
      await expect(page.locator("#view-assets")).toHaveClass(/active/);
      await page.locator("#newAssetBtn").click();
      const assetDialog = page
        .getByRole("dialog")
        .filter({ has: page.getByRole("heading", { name: "新建房产", exact: true }) });
      await expect(assetDialog).toBeVisible();
      await assetDialog.locator("#f_asset_name").fill(assetName);
      await assetDialog.locator("#f_asset_address").fill("E2E 幸福路 1 号");
      await assetDialog.locator("#f_asset_room_no").fill("501");
      await assetDialog.getByRole("button", { name: "保存", exact: true }).click();
      await expect(assetDialog).toBeHidden();
      await expect(
        page.locator(`#assetTable tr[data-id]:has-text("${assetName}")`),
      ).toBeVisible();
      await shot(page, testInfo, "04-甲方与房产档案已新建");
    });

    // 租期默认值的期望：起租日 = 今天，到期日 = 今天+1 年−1 天。在测试内
    // 独立计算（本地时区），不引用被测实现 lease.ts。
    const now = new Date();
    const isoLocal = (d: Date): string =>
      `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(
        d.getDate(),
      ).padStart(2, "0")}`;
    const expectedLeaseStart = isoLocal(now);
    const leaseEndDate = new Date(now.getFullYear() + 1, now.getMonth(), now.getDate());
    leaseEndDate.setDate(leaseEndDate.getDate() - 1);
    const expectedLeaseEnd = isoLocal(leaseEndDate);

    let contractId = "";
    await test.step("界面三方选择建合同并保存", async () => {
      await page.locator('#tabs button[data-tab="contracts"]').click();
      await expect(page.locator("#view-contracts")).toHaveClass(/active/);
      await page.locator("#newContractBtn").click();

      const contractDialog = page
        .getByRole("dialog")
        .filter({ has: page.getByRole("heading", { name: "新建合同", exact: true }) });
      await expect(contractDialog).toBeVisible();
      await expect(contractDialog.locator("#f_contract_status")).toHaveValue("草稿");
      await contractDialog.locator("#f_contract_title").fill(contractTitle);
      // 甲方：固定单位档案默认带出（本地无最近使用时取第一个单位档案），可切换。
      await expect(
        contractDialog.locator("#f_contract_party_a option:checked"),
      ).toContainText(`${partyAName}（单位）`);
      await contractDialog
        .locator("#f_contract_party_a")
        .selectOption({ label: `${partyName}（个人）` });
      await expect(
        contractDialog.locator("#f_contract_party_a option:checked"),
      ).toContainText(`${partyName}（个人）`);
      await contractDialog
        .locator("#f_contract_party_a")
        .selectOption({ label: `${partyAName}（单位）` });
      // 乙方：从租户档案搜索选择，选中即带出证件号。
      await contractDialog.locator("#f_contract_party_b").fill(partyName);
      await contractDialog
        .locator(`#partyBPickerList button[data-party-id]:has-text("${partyName}")`)
        .click();
      await expect(
        contractDialog.locator('.picker-field[data-key="party_b"] .picker-selected'),
      ).toContainText(`E2E${stamp}`);
      // 资产：从房产档案选择；在租/空置标记来自后端派生。
      await contractDialog.locator("#f_contract_asset").fill(assetName);
      await contractDialog
        .locator(`#assetPickerList button[data-asset-id]:has-text("${assetName}")`)
        .click();
      await expect(
        contractDialog.locator('.picker-field[data-key="asset"] .picker-selected'),
      ).toContainText("空置");
      // 租期默认一年：起租日 = 今天，到期日 = 一年后的前一天（含首尾）。
      // 期望值在测试内独立计算，不引用被测实现（lease.ts）。
      await expect(contractDialog.locator("#f_contract_lease_start")).toHaveValue(
        expectedLeaseStart,
      );
      await expect(contractDialog.locator("#f_contract_lease_end")).toHaveValue(
        expectedLeaseEnd,
      );
      await contractDialog.locator("#f_contract_lease_start").fill("2026-12-01");
      await contractDialog.locator("#f_contract_lease_end").fill("2027-11-30");
      // 三方选定后合同生效，房产应派生为在租。
      await contractDialog.locator("#f_contract_status").selectOption("生效");
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
      const detail = (await (
        await page.request.get(
          `${app.baseURL}/app/api/v1/contracts/${contractId}`,
        )
      ).json()) as {
        contract: { asset_id?: string; lease_start?: string; lease_end?: string };
        asset?: { name: string; lease_status: string };
      };
      expect(detail.contract.asset_id ?? "").not.toBe("");
      expect(detail.contract.lease_start).toBe("2026-12-01");
      expect(detail.contract.lease_end).toBe("2027-11-30");
      expect(detail.asset?.name).toBe(assetName);
      expect(detail.asset?.lease_status).toBe("rented");
      await shot(page, testInfo, "05-三方合同已保存且房产在租");
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

// 场景 A2：存量档案缺证件号的一次性补录提示（v0.2 需求 1）。
//
// 业务含义：合同录入按名称自动建立的档案可以没有证件号（保持旧流程），但选中
// 这类档案时要提示一次补录；同一浏览器对同一档案只提示一次，补录后长期有效。
test.describe("场景 A2：缺证件号档案的补录提示", () => {
  test("合同表单选中无证件号档案时只提示一次，刷新后不再提示", async ({
    app,
    page,
  }, testInfo) => {
    const consoleErrors = trackConsoleErrors(page);
    const stamp = Date.now().toString(36);
    const partyName = `E2E 无证件号 ${stamp}`;
    const contractTitle = `E2E 补录提示合同 ${stamp}`;
    const toast = page.locator("#toast");

    await test.step("新建引用新名称的合同（按名称自动建档，无证件号）", async () => {
      // 名称为新建档的兼容路径仍在服务端（Excel/接口），用接口造出
      // 「有档案但无证件号」的存量形态，再从界面选中它验证补录提示。
      const response = await page.request.post(
        `${app.baseURL}/app/api/v1/contracts`,
        {
          headers: { "X-Tengshou-App": "1" },
          data: {
            year: new Date().getFullYear(),
            title: contractTitle,
            party_b: partyName,
          },
        },
      );
      expect(response.ok()).toBeTruthy();
      await page.goto(`${app.baseURL}/app/`, { waitUntil: "domcontentloaded" });
      await page.locator('#tabs button[data-tab="contracts"]').click();
      await expect(page.locator("#view-contracts")).toHaveClass(/active/);
      await expect(
        page.locator(`#contractTable tr[data-id]:has-text("${contractTitle}")`),
      ).toBeVisible();
      await shot(page, testInfo, "01-合同已用无证件号档案建立");
    });

    let contractId = "";
    await test.step("打开合同即提示补录并记录去重标记", async () => {
      const row = page.locator(
        `#contractTable tr[data-id]:has-text("${contractTitle}")`,
      );
      contractId = (await row.getAttribute("data-id")) ?? "";
      expect(contractId).not.toBe("");
      await row.click();
      const dialog = page
        .getByRole("dialog")
        .filter({ has: page.getByRole("heading", { name: /编辑合同/ }) });
      await expect(dialog).toBeVisible();
      await expect(toast).toHaveClass(/show/, { timeout: 10_000 });
      await expect(toast).toContainText("还没有证件号");
      // 去重按档案 id 落在 localStorage，同一浏览器只有一条标记。
      const marked = await page.evaluate(
        () =>
          Object.keys(localStorage).filter((key) =>
            key.startsWith("tengshou:party-id-backfill:"),
          ).length,
      );
      expect(marked).toBe(1);
      await shot(page, testInfo, "02-补录提示出现");
      // 等提示自然消失，避免把残留 toast 误判为第二次提示。
      await expect(toast).not.toHaveClass(/show/, { timeout: 15_000 });
      await dialog.getByRole("button", { name: "取消" }).click();
      await expect(dialog).toBeHidden();
    });

    await test.step("刷新并再次打开同一合同：不再重复提示", async () => {
      await page.reload({ waitUntil: "domcontentloaded" });
      await page.locator('#tabs button[data-tab="contracts"]').click();
      await expect(page.locator("#view-contracts")).toHaveClass(/active/);
      const row = page.locator(`#contractTable tr[data-id="${contractId}"]`);
      await expect(row).toBeVisible();
      await row.click();
      const dialog = page
        .getByRole("dialog")
        .filter({ has: page.getByRole("heading", { name: /编辑合同/ }) });
      await expect(dialog).toBeVisible();
      // 给 watcher 的 immediate 回调留出时间：不应再出现补录提示。
      await page.waitForTimeout(1200);
      const secondPrompt = await toast.evaluate(
        (el) =>
          el.classList.contains("show") &&
          (el.textContent ?? "").includes("还没有证件号"),
      );
      expect(secondPrompt).toBe(false);
      await shot(page, testInfo, "03-再次打开不再提示");
      await dialog.getByRole("button", { name: "取消" }).click();
      await expect(dialog).toBeHidden();
    });

    expect(
      consoleErrors,
      `浏览器控制台出现错误：\n${consoleErrors.join("\n")}`,
    ).toEqual([]);
  });
});

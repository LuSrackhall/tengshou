// 场景 E：档案签名直接生成合同 + 现场手写回归（v0.2 D3=B）。
//
// 业务含义：出租方在租户档案里采集过一次长期签名后，新合同不必再让租户扫码：
// 签名对话框里点「使用租户档案签名」直接套印，生成记录里签名来源为 archive，
// 合同内容修改也不会要求重新确认；没有档案签名时，原扫码手写路径（一次性
// 二维码 + 手机页）仍可完成签署，生成记录来源为 lan_qr。
// 全程只驱动发布形态 tengshou-cli serve，不访问公网。
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { expect, test } from "./support/business-fixture";
import { appUrl } from "./support/business-api";
import { repoRoot } from "./support/app";
import { shot, trackConsoleErrors } from "./support/ui";

const APP_HEADERS = { "X-Tengshou-App": "1" };

/** 在手机页画布上手写一笔并提交。 */
async function signOnPhone(page: import("@playwright/test").Page, padSelector: string, submitName: string): Promise<void> {
  const pad = page.locator(padSelector);
  await pad.scrollIntoViewIfNeeded();
  const box = await pad.boundingBox();
  expect(box).not.toBeNull();
  if (!box) {
    return;
  }
  const startX = box.x + 40;
  const endX = box.x + box.width - 40;
  const midY = box.y + box.height / 2;
  await page.mouse.move(startX, midY);
  await page.mouse.down();
  for (let i = 1; i <= 24; i += 1) {
    const x = startX + ((endX - startX) * i) / 24;
    const y = midY + Math.sin(i / 1.7) * (box.height / 5);
    await page.mouse.move(x, y, { steps: 3 });
  }
  await page.mouse.up();
  const submit = page.getByRole("button", { name: submitName, exact: true });
  await expect(submit).toBeEnabled();
  await submit.click();
}

test.describe("场景 E：档案签名复用与现场手写", () => {
  test("使用租户档案签名直接生成合同；扫码手写路径回归", async ({
    app,
    page,
    context,
  }, testInfo) => {
    const consoleErrors = trackConsoleErrors(page);
    const stamp = Date.now().toString(36);
    const templateName = `E2E 签名模板 ${stamp}`;
    const tenantName = `E2E 档案签名租户 ${stamp}`;
    const onSiteName = `E2E 现场签署租户 ${stamp}`;

    await test.step("打开发布形态界面并准备带签名位的模板", async () => {
      const response = await page.goto(`${app.baseURL}/app/`, {
        waitUntil: "domcontentloaded",
      });
      expect(response?.status()).toBe(200);
      await expect(page.locator("#tabs")).toBeVisible();

      const pdf = await readFile(
        join(repoRoot(), "e2e", "fixtures", "template.pdf"),
      );
      const importRes = await page.request.post(
        appUrl(page, "/app/api/v1/templates/import"),
        {
          headers: APP_HEADERS,
          data: {
            name: templateName,
            file_name: "signature-template.pdf",
            file_base64: pdf.toString("base64"),
          },
        },
      );
      expect(importRes.ok()).toBeTruthy();
      const imported = (await importRes.json()) as { item: { id: string } };
      const mappingRes = await page.request.put(
        appUrl(page, `/app/api/v1/templates/${imported.item.id}/mapping`),
        {
          headers: APP_HEADERS,
          data: {
            placements: [
              {
                id: "no",
                page: 1,
                x: 110,
                y: 150,
                w: 220,
                h: 22,
                type: "field",
                field: "contract.contract_no",
              },
              {
                id: "sig",
                page: 1,
                x: 300,
                y: 600,
                w: 140,
                h: 60,
                type: "signature",
                party: "party_b",
              },
            ],
          },
        },
      );
      expect(mappingRes.ok()).toBeTruthy();
      const mapped = (await mappingRes.json()) as { item: { id: string } };
      expect(mapped.item.id).not.toBe("");
      await shot(page, testInfo, "01-模板与签名位已准备");
    });

    // 模板映射保存会生成新版本；从模板表里取当前最新版本 id。
    const templateRow = page
      .locator(`#templateTable tr[data-id]:has-text("${templateName}")`)
      .first();
    await page.locator('#tabs button[data-tab="templates"]').click();
    await expect(templateRow).toBeVisible();
    const templateId = (await templateRow.getAttribute("data-id")) ?? "";
    expect(templateId).not.toBe("");

    let tenantID = "";
    let tenantContractID = "";
    let onSiteContractID = "";
    await test.step("接口准备两份租户与合同", async () => {
      const createParty = async (name: string, idNumber: string) => {
        const res = await page.request.post(appUrl(page, "/app/api/v1/parties"), {
          headers: APP_HEADERS,
          data: { kind: "person", name, id_number: idNumber },
        });
        expect(res.ok()).toBeTruthy();
        return ((await res.json()) as { item: { id: string } }).item.id;
      };
      tenantID = await createParty(
        tenantName,
        `33010219800101${stamp.slice(-6).padEnd(6, "0")}`,
      );
      const onSiteID = await createParty(
        onSiteName,
        `33010219800202${stamp.slice(-6).padEnd(6, "0")}`,
      );
      const createContract = async (title: string, partyId: string) => {
        const res = await page.request.post(appUrl(page, "/app/api/v1/contracts"), {
          headers: APP_HEADERS,
          data: { year: 2026, title, party_b_id: partyId, amount: 1200 },
        });
        expect(res.ok()).toBeTruthy();
        return ((await res.json()) as { item: { id: string } }).item.id;
      };
      tenantContractID = await createContract(`E2E 档案复用合同 ${stamp}`, tenantID);
      onSiteContractID = await createContract(`E2E 现场手写合同 ${stamp}`, onSiteID);
    });

    await test.step("档案页采集长期签名", async () => {
      await page.locator('#tabs button[data-tab="parties"]').click();
      await expect(page.locator("#view-parties")).toHaveClass(/active/);
      const row = page.locator(`#partyTable tr[data-id]:has-text("${tenantName}")`);
      await expect(row).toBeVisible();
      await row.click();
      const archiveDialog = page.getByRole("dialog").filter({
        has: page.getByRole("heading", { name: `编辑相对方 · ${tenantName}` }),
      });
      await expect(archiveDialog).toBeVisible();
      await archiveDialog
        .getByRole("button", { name: /扫码采集|重新采集/ })
        .click();
      const signDialog = page.getByRole("dialog").filter({
        has: page.getByRole("heading", { name: `档案签名 · ${tenantName}` }),
      });
      await expect(signDialog).toBeVisible();
      await expect(signDialog.locator("#partySignQrImg")).toBeVisible({
        timeout: 15_000,
      });
      const urlText =
        (await signDialog.locator(".sign-url").textContent())?.trim() ?? "";
      const token = urlText.split("/sign/")[1] ?? "";
      expect(token).toHaveLength(64);

      const phone = await context.newPage();
      const phoneErrors = trackConsoleErrors(phone);
      await phone.goto(`${app.baseURL}/sign/${token}`, {
        waitUntil: "domcontentloaded",
      });
      await expect(phone.locator("#signCard")).toBeVisible();
      await signOnPhone(phone, "#pad", "提交签名");
      await expect(
        phone.getByRole("heading", { name: "签名已提交" }),
      ).toBeVisible({ timeout: 20_000 });
      await expect(signDialog).toBeHidden({ timeout: 20_000 });
      await expect(page.locator("#toast")).toContainText("档案签名已保存");
      await expect(archiveDialog.locator('img[alt="档案签名"]')).toBeVisible();
      expect(phoneErrors).toEqual([]);
      await phone.close();
      await archiveDialog.getByRole("button", { name: "取消" }).click();
      await expect(archiveDialog).toBeHidden();
      await shot(page, testInfo, "02-档案签名已采集");
    });

    await test.step("合同签名对话框直接使用租户档案签名", async () => {
      await page.locator('#tabs button[data-tab="contracts"]').click();
      await expect(page.locator("#view-contracts")).toHaveClass(/active/);
      const row = page.locator(
        `#contractTable tr[data-id="${tenantContractID}"]`,
      );
      await expect(row).toBeVisible();
      await row.getByRole("button", { name: "扫码签名" }).click();
      const dialog = page.getByRole("dialog").filter({
        has: page.getByRole("heading", { name: /扫码签名/ }),
      });
      await expect(dialog).toBeVisible();
      const reuseBtn = dialog.getByRole("button", { name: "使用租户档案签名" });
      await expect(reuseBtn).toBeVisible();
      await reuseBtn.click();
      await expect(dialog.locator(".sign-state.ok")).toContainText(
        "已使用档案签名（来源：档案复用",
      );
      await expect(dialog).toContainText("来源：档案复用");
      await shot(page, testInfo, "03-已使用租户档案签名");
      await dialog.locator(".modal-foot").getByRole("button", { name: "关闭" }).click();
      await expect(dialog).toBeHidden();
    });

    await test.step("预检通过并生成 PDF，生成记录来源为档案复用", async () => {
      const preflightRes = await page.request.post(
        appUrl(page, `/app/api/v1/templates/${templateId}/preflight`),
        { headers: APP_HEADERS, data: { contract_id: tenantContractID } },
      );
      expect(preflightRes.ok()).toBeTruthy();
      const preflight = (await preflightRes.json()) as {
        ready: boolean;
        status: string;
        signatures: Array<{ source?: string; content_sha256?: string }>;
      };
      expect(preflight.ready, JSON.stringify(preflight)).toBe(true);
      expect(preflight.signatures[0]?.source).toBe("archive");
      expect(preflight.signatures[0]?.content_sha256 ?? "").toBe("");

      const previewRes = await page.request.post(
        appUrl(page, `/app/api/v1/templates/${templateId}/preview`),
        { headers: APP_HEADERS, data: { contract_id: tenantContractID } },
      );
      expect(previewRes.ok()).toBeTruthy();
      const preview = (await previewRes.json()) as { preview_sha256: string };
      const generateRes = await page.request.post(
        appUrl(page, `/app/api/v1/templates/${templateId}/generate`),
        {
          headers: APP_HEADERS,
          data: {
            contract_id: tenantContractID,
            preview_sha256: preview.preview_sha256,
          },
        },
      );
      expect(generateRes.ok()).toBeTruthy();
      const generated = (await generateRes.json()) as {
        item: {
          signatures: Array<{ source?: string; content_sha256?: string }>;
        };
      };
      expect(generated.item.signatures[0]?.source).toBe("archive");
      expect(generated.item.signatures[0]?.content_sha256 ?? "").toBe("");
      await shot(page, testInfo, "04-档案签名已套印生成");
    });

    await test.step("修改合同内容后仍可直接套印，无需重新确认", async () => {
      const editRes = await page.request.put(
        appUrl(page, `/app/api/v1/contracts/${tenantContractID}`),
        {
          headers: APP_HEADERS,
          data: {
            year: 2026,
            title: `E2E 档案复用合同（改名）${stamp}`,
            party_b_id: tenantID,
            amount: 1200,
          },
        },
      );
      expect(editRes.ok()).toBeTruthy();
      const preflightRes = await page.request.post(
        appUrl(page, `/app/api/v1/templates/${templateId}/preflight`),
        { headers: APP_HEADERS, data: { contract_id: tenantContractID } },
      );
      const preflight = (await preflightRes.json()) as {
        ready: boolean;
        issues: Array<{ code: string }>;
      };
      expect(preflight.ready, JSON.stringify(preflight.issues)).toBe(true);
      expect(preflight.issues.some((issue) => issue.code === "content_changed")).toBe(
        false,
      );
    });

    await test.step("现场手写路径回归：扫码签名后生成 PDF", async () => {
      await page.locator('#tabs button[data-tab="contracts"]').click();
      const row = page.locator(
        `#contractTable tr[data-id="${onSiteContractID}"]`,
      );
      await expect(row).toBeVisible();
      await row.getByRole("button", { name: "扫码签名" }).click();
      const dialog = page.getByRole("dialog").filter({
        has: page.getByRole("heading", { name: /扫码签名/ }),
      });
      await expect(dialog).toBeVisible();
      await expect(dialog.getByRole("button", { name: "生成签名二维码" })).toBeVisible();
      await dialog.getByRole("button", { name: "生成签名二维码" }).click();
      await expect(dialog.locator("#signQrImg")).toBeVisible({ timeout: 15_000 });
      const urlText =
        (await dialog.locator(".sign-url").textContent())?.trim() ?? "";
      const token = urlText.split("/sign/")[1] ?? "";
      expect(token).toHaveLength(64);

      const phone = await context.newPage();
      const phoneErrors = trackConsoleErrors(phone);
      await phone.goto(`${app.baseURL}/sign/${token}`, {
        waitUntil: "domcontentloaded",
      });
      await expect(phone.locator("#signCard")).toBeVisible();
      await signOnPhone(phone, "#pad", "提交签名");
      await expect(
        phone.getByRole("heading", { name: "签名已提交" }),
      ).toBeVisible({ timeout: 20_000 });
      await expect(dialog).toContainText("签名已保存", { timeout: 20_000 });
      expect(phoneErrors).toEqual([]);
      await phone.close();
      await dialog.locator(".modal-foot").getByRole("button", { name: "关闭" }).click();
      await expect(dialog).toBeHidden();

      const preflightRes = await page.request.post(
        appUrl(page, `/app/api/v1/templates/${templateId}/preflight`),
        { headers: APP_HEADERS, data: { contract_id: onSiteContractID } },
      );
      const preflight = (await preflightRes.json()) as {
        ready: boolean;
        signatures: Array<{ source?: string }>;
      };
      expect(preflight.ready, JSON.stringify(preflight)).toBe(true);
      expect(preflight.signatures[0]?.source).toBe("lan_qr");

      const previewRes = await page.request.post(
        appUrl(page, `/app/api/v1/templates/${templateId}/preview`),
        { headers: APP_HEADERS, data: { contract_id: onSiteContractID } },
      );
      expect(previewRes.ok()).toBeTruthy();
      const preview = (await previewRes.json()) as { preview_sha256: string };
      const generateRes = await page.request.post(
        appUrl(page, `/app/api/v1/templates/${templateId}/generate`),
        {
          headers: APP_HEADERS,
          data: {
            contract_id: onSiteContractID,
            preview_sha256: preview.preview_sha256,
          },
        },
      );
      expect(generateRes.ok()).toBeTruthy();
      const generated = (await generateRes.json()) as {
        item: { signatures: Array<{ source?: string }> };
      };
      expect(generated.item.signatures[0]?.source).toBe("lan_qr");
      await shot(page, testInfo, "05-现场手写签名已生成");
    });

    expect(
      consoleErrors,
      `浏览器控制台出现错误：\n${consoleErrors.join("\n")}`,
    ).toEqual([]);
  });
});

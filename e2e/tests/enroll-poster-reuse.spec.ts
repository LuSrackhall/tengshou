// 场景 F：贴墙二维码 → 手机自助建档（含签名章/公章）→ 合同默认复用 →
// 生成 PDF，以及设置页「复用告知提示」开关的两种形态（te-48hkl，Q6 修正）。
//
// 业务含义：出租方打印长期二维码海报贴墙；企业租户用手机扫码，不上手写也
// 可以上传签名章（私章）与公章。档案建好后，建合同选择该档案时默认勾选
// 「复用档案签名章」「使用档案公章」，保存即套印。设置页开关只控制是否
// 展示「将复用…（来源：档案）」与回退提示：开启时展示；关闭时不展示但仍
// 静默复用。取消勾选则回到现场扫码手写的回退路径。
// 全程只驱动发布形态 tengshou-cli serve，不访问公网。
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { expect, test } from "./support/business-fixture";
import { appUrl } from "./support/business-api";
import { repoRoot } from "./support/app";
import { createParty } from "./support/business-records";
import { shot, trackConsoleErrors } from "./support/ui";

const APP_HEADERS = { "X-Tengshou-App": "1" };
const ONE_PIXEL_PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=",
  "base64",
);

async function sealPNG(): Promise<Buffer> {
  return readFile(join(repoRoot(), "e2e", "fixtures", "contracts", "出租方公章.png"));
}

/** 导入带乙方签名位与公章位的模板，返回模板 id。 */
async function prepareTemplate(page: import("@playwright/test").Page, name: string): Promise<string> {
  const pdf = await readFile(join(repoRoot(), "e2e", "fixtures", "template.pdf"));
  const importRes = await page.request.post(appUrl(page, "/app/api/v1/templates/import"), {
    headers: APP_HEADERS,
    data: {
      name,
      file_name: "archive-reuse-template.pdf",
      file_base64: pdf.toString("base64"),
    },
  });
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
          {
            id: "seal",
            page: 1,
            x: 460,
            y: 600,
            w: 110,
            h: 110,
            type: "seal",
            party: "party_b",
            seal_mode: "contract",
          },
        ],
      },
    },
  );
  expect(mappingRes.ok()).toBeTruthy();
  // 映射保存会生成新版本行（新 id）。必须用接口返回的版本 id：模板表由
  // SSE 触发重取，界面列表可能仍是导入时那一版（无签名位），读表会拿到
  // 旧版本 id，导致预检/生成没有签名快照（Windows 曾偶发 source undefined）。
  const mapped = (await mappingRes.json()) as { item: { id: string } };
  expect(mapped.item.id).not.toBe("");
  return mapped.item.id;
}

async function generateWithTemplate(
  page: import("@playwright/test").Page,
  templateId: string,
  contractId: string,
): Promise<{ signatures: Array<{ source?: string }>; ready: boolean; issues?: unknown[] }> {
  const preflightRes = await page.request.post(
    appUrl(page, `/app/api/v1/templates/${templateId}/preflight`),
    { headers: APP_HEADERS, data: { contract_id: contractId } },
  );
  expect(preflightRes.ok()).toBeTruthy();
  const preflight = (await preflightRes.json()) as {
    ready: boolean;
    issues?: unknown[];
    signatures: Array<{ source?: string }>;
  };
  expect(preflight.ready, JSON.stringify(preflight)).toBe(true);
  const previewRes = await page.request.post(
    appUrl(page, `/app/api/v1/templates/${templateId}/preview`),
    { headers: APP_HEADERS, data: { contract_id: contractId } },
  );
  expect(previewRes.ok()).toBeTruthy();
  const preview = (await previewRes.json()) as { preview_sha256: string };
  const generateRes = await page.request.post(
    appUrl(page, `/app/api/v1/templates/${templateId}/generate`),
    {
      headers: APP_HEADERS,
      data: { contract_id: contractId, preview_sha256: preview.preview_sha256 },
    },
  );
  expect(generateRes.ok()).toBeTruthy();
  const generated = (await generateRes.json()) as {
    item: { signatures: Array<{ source?: string }> };
  };
  return { ...preflight, signatures: generated.item.signatures };
}

test.describe("场景 F：自助录入海报与档案复用", () => {
  test("贴墙二维码 → 手机自助建档（签名章+公章）→ 合同默认复用 → 生成 PDF", async ({
    app,
    page,
    context,
  }, testInfo) => {
    const consoleErrors = trackConsoleErrors(page);
    const stamp = Date.now().toString(36);
    const templateName = `E2E 档案复用模板 ${stamp}`;
    const tenantName = `海报企业租户 ${stamp}`;
    const idNumber = `91330100ENROLL${stamp.slice(-6)}`;
    const contractTitle = `E2E 档案复用合同 ${stamp}`;

    let templateId = "";
    await test.step("准备带签名位与公章位的模板", async () => {
      const response = await page.goto(`${app.baseURL}/app/`, {
        waitUntil: "domcontentloaded",
      });
      expect(response?.status()).toBe(200);
      await expect(page.locator("#tabs")).toBeVisible();
      templateId = await prepareTemplate(page, templateName);
      await shot(page, testInfo, "01-模板已准备");
    });

    let enrollToken = "";
    await test.step("租户档案页打印海报：生成固定二维码", async () => {
      await page.locator('#tabs button[data-tab="parties"]').click();
      await expect(page.locator("#view-parties")).toHaveClass(/active/);
      await page.locator("#enrollPosterBtn").click();
      const dialog = page.getByRole("dialog").filter({
        has: page.getByRole("heading", { name: "自助录入海报（可打印）" }),
      });
      await expect(dialog).toBeVisible();
      await expect(dialog.locator(".poster-url code")).toContainText("/enroll/", {
        timeout: 15_000,
      });
      const url = (await dialog.locator(".poster-url code").textContent())?.trim() ?? "";
      const firstToken = url.split("/enroll/")[1] ?? "";
      expect(firstToken).toHaveLength(64);
      const qr = dialog.locator("#enrollPosterSheet img");
      await expect(qr).toBeVisible();
      const firstQrSrc = (await qr.getAttribute("src")) ?? "";
      expect(firstQrSrc).toContain(firstToken);
      await shot(page, testInfo, "02-可打印海报与固定二维码");

      // 换码：旧海报立即失效；二维码地址带新 token 版本，打印出来就是新码。
      page.once("dialog", (confirm) => confirm.accept());
      await dialog.locator("#posterRotateBtn").click();
      await expect
        .poll(
          async () =>
            (await dialog.locator(".poster-url code").textContent())?.trim() ?? "",
        )
        .not.toBe(url);
      const rotatedUrl = (await dialog.locator(".poster-url code").textContent())?.trim() ?? "";
      enrollToken = rotatedUrl.split("/enroll/")[1] ?? "";
      expect(enrollToken).toHaveLength(64);
      expect(enrollToken).not.toBe(firstToken);
      await expect
        .poll(async () => (await qr.getAttribute("src")) ?? "")
        .toContain(enrollToken);
      const oldPoster = await page.request.get(`${app.baseURL}/enroll/${firstToken}`);
      expect(oldPoster.status()).toBe(410);
      await shot(page, testInfo, "02b-换码后二维码刷新且旧海报失效");
      await dialog.locator(".modal-head button.close").click();
      await expect(dialog).toBeHidden();
    });

    await test.step("手机页自助建档：上传证件照、签名章与公章，不手写", async () => {
      const phone = await context.newPage();
      const phoneErrors = trackConsoleErrors(phone);
      const response = await phone.goto(`${app.baseURL}/enroll/${enrollToken}`, {
        waitUntil: "domcontentloaded",
      });
      expect(response?.status()).toBe(200);
      await expect(phone.locator("#enrollCard")).toBeVisible();
      // 单位租户选择「单位」类型：名称标签切换为「单位名称」，提交 kind=organization。
      await phone.locator("#enrollKind").selectOption("organization");
      await expect(phone.locator("#enrollNameLabel")).toHaveText("单位名称");
      await phone.locator("#enrollName").fill(tenantName);
      await phone.locator("#enrollIDNumber").fill(idNumber);
      await phone.locator("#enrollPhone").fill("13800003333");
      await phone
        .locator("#enrollIdFront")
        .setInputFiles({ name: "front.png", mimeType: "image/png", buffer: ONE_PIXEL_PNG });
      await phone
        .locator("#enrollIdBack")
        .setInputFiles({ name: "back.png", mimeType: "image/png", buffer: ONE_PIXEL_PNG });
      const seal = await sealPNG();
      await phone
        .locator("#enrollSignatureSeal")
        .setInputFiles({ name: "signature-seal.png", mimeType: "image/png", buffer: seal });
      await phone
        .locator("#enrollSeal")
        .setInputFiles({ name: "seal.png", mimeType: "image/png", buffer: seal });
      await expect(phone.locator("#enrollSignatureSealPreview")).toBeVisible();
      await expect(phone.locator("#enrollSealPreview")).toBeVisible();
      await expect(phone.locator("#enrollSubmitBtn")).toBeEnabled();
      await phone.locator("#enrollSubmitBtn").click();
      await expect(phone.getByRole("heading", { name: "档案已提交" })).toBeVisible({
        timeout: 20_000,
      });
      await expect(phone.locator("#enrollAgainBtn")).toBeVisible();
      await shot(phone, testInfo, "03-手机侧提交成功（可为下一位继续）");
      expect(phoneErrors).toEqual([]);
      await phone.close();
    });

    let tenantID = "";
    await test.step("桌面收到档案，签名章与公章来源为手机自助录入", async () => {
      const row = page.locator(`#partyTable tr[data-id]:has-text("${tenantName}")`);
      await expect(row).toBeVisible({ timeout: 20_000 });
      // 「类型」列显示「单位」，不再默认显示「个人」。
      await expect(row).toContainText("单位");
      tenantID = (await row.getAttribute("data-id")) ?? "";
      expect(tenantID).not.toBe("");
      const detail = (await (
        await page.request.get(appUrl(page, `/app/api/v1/parties/${tenantID}`))
      ).json()) as {
        kind?: string;
        signature_id?: string;
        signature_seal_id?: string;
        signature_seal_source?: string;
        seal_asset_id?: string;
        seal_source?: string;
      };
      expect(detail.kind).toBe("organization");
      expect(detail.signature_id ?? "").toBe("");
      expect(detail.signature_seal_id ?? "").not.toBe("");
      expect(detail.seal_asset_id ?? "").not.toBe("");
      expect(detail.signature_seal_source).toBe("lan_enroll");
      expect(detail.seal_source).toBe("lan_enroll");
      await shot(page, testInfo, "04-档案已含签名章与公章");
    });

    let contractID = "";
    await test.step("建合同选择该档案：默认勾选复用签名章与公章", async () => {
      await page.locator('#tabs button[data-tab="contracts"]').click();
      await expect(page.locator("#view-contracts")).toHaveClass(/active/);
      await page.locator("#newContractBtn").click();
      const dialog = page.getByRole("dialog").filter({
        has: page.getByRole("heading", { name: "新建合同", exact: true }),
      });
      await expect(dialog).toBeVisible();
      await dialog.locator("#f_contract_title").fill(contractTitle);
      const partyB = dialog.locator('.picker-field[data-key="party_b"]');
      await partyB.locator("#f_contract_party_b").fill(tenantName);
      await partyB
        .locator(`#partyBPickerList button[data-party-id="${tenantID}"]`)
        .click();
      await expect(partyB.locator(".picker-selected")).toContainText(tenantName);

      const reuse = dialog.locator("#contractArchiveReuse");
      await expect(reuse).toBeVisible({ timeout: 15_000 });
      // 告知开关默认开启：展示来源与回退提示。
      await expect(reuse).toContainText("将复用档案签名（来源：档案）");
      await expect(reuse).toContainText("将复用档案签名（来源：档案）与档案公章（来源：档案）");
      await expect(reuse).toContainText("如对方不同意，请取消勾选并改用现场扫码手写");
      const checkboxes = reuse.locator('input[type="checkbox"]');
      await expect(checkboxes).toHaveCount(2);
      await expect(checkboxes.nth(0)).toBeChecked();
      await expect(checkboxes.nth(1)).toBeChecked();
      await shot(page, testInfo, "05-合同表单默认勾选档案复用");
      await dialog.getByRole("button", { name: "保存", exact: true }).click();
      await expect(dialog).toBeHidden();

      const row = page.locator(`#contractTable tr[data-id]:has-text("${contractTitle}")`);
      await expect(row).toBeVisible();
      contractID = (await row.getAttribute("data-id")) ?? "";
      expect(contractID).not.toBe("");

      await expect
        .poll(async () => {
          const res = await page.request.get(
            appUrl(page, `/app/api/v1/contracts/${contractID}`),
          );
          const detail = (await res.json()) as {
            signatures?: Array<{ source?: string }>;
            seals?: Array<{ id: string }>;
          };
          return {
            signatures: (detail.signatures ?? []).map((item) => item.source),
            seals: (detail.seals ?? []).length,
          };
        })
        .toEqual({ signatures: ["archive_seal"], seals: 1 });
      await shot(page, testInfo, "06-合同详情已复用签名章与公章");
    });

    await test.step("预检与生成 PDF：签名来源为档案签名章复用", async () => {
      const result = await generateWithTemplate(page, templateId, contractID);
      expect(result.signatures[0]?.source).toBe("archive_seal");
      await shot(page, testInfo, "07-已生成带签名章与公章的 PDF");
    });

    await test.step("设置页关闭告知：静默复用仍生效（Q6 修正）", async () => {
      page.once("dialog", (dialog) => dialog.accept());
      await page.locator('#tabs button[data-tab="settings"]').click();
      const toggle = page.locator("#archiveReuseToggle");
      await expect(toggle).toBeVisible();
      await expect(toggle).toBeChecked();
      await toggle.uncheck();
      await expect(toggle).not.toBeChecked();
      await expect(page.locator("#view-settings")).toContainText("风险提示");
      await shot(page, testInfo, "08-设置页关闭档案复用告知");

      await page.locator('#tabs button[data-tab="contracts"]').click();
      await page.locator("#newContractBtn").click();
      const dialog = page.getByRole("dialog").filter({
        has: page.getByRole("heading", { name: "新建合同", exact: true }),
      });
      await expect(dialog).toBeVisible();
      await dialog.locator("#f_contract_title").fill(`${contractTitle}（静默复用）`);
      const partyB = dialog.locator('.picker-field[data-key="party_b"]');
      await partyB.locator("#f_contract_party_b").fill(tenantName);
      await partyB
        .locator(`#partyBPickerList button[data-party-id="${tenantID}"]`)
        .click();
      const reuse = dialog.locator("#contractArchiveReuse");
      await expect(reuse).toBeVisible({ timeout: 15_000 });
      // 告知关闭：不显示来源与回退提示，但默认勾选与静默复用保留。
      await expect(reuse).not.toContainText("来源：档案");
      const checkboxes = reuse.locator('input[type="checkbox"]');
      await expect(checkboxes).toHaveCount(2);
      await expect(checkboxes.nth(0)).toBeChecked();
      await expect(checkboxes.nth(1)).toBeChecked();
      await dialog.getByRole("button", { name: "保存", exact: true }).click();
      await expect(dialog).toBeHidden();

      const row = page
        .locator(`#contractTable tr[data-id]:has-text("${contractTitle}（静默复用）")`)
        .first();
      await expect(row).toBeVisible();
      const silentID = (await row.getAttribute("data-id")) ?? "";
      expect(silentID).not.toBe("");
      await expect
        .poll(async () => {
          const res = await page.request.get(
            appUrl(page, `/app/api/v1/contracts/${silentID}`),
          );
          const detail = (await res.json()) as {
            signatures?: Array<{ source?: string }>;
            seals?: unknown[];
          };
          return {
            signatures: (detail.signatures ?? []).map((item) => item.source),
            seals: (detail.seals ?? []).length,
          };
        })
        .toEqual({ signatures: ["archive_seal"], seals: 1 });

      // 对方不同意时取消勾选：合同不套印档案资产，回退现场扫码手写。
      await page.locator("#newContractBtn").click();
      const fallbackDialog = page.getByRole("dialog").filter({
        has: page.getByRole("heading", { name: "新建合同", exact: true }),
      });
      await expect(fallbackDialog).toBeVisible();
      await fallbackDialog.locator("#f_contract_title").fill(`${contractTitle}（现场签名）`);
      const fallbackPartyB = fallbackDialog.locator('.picker-field[data-key="party_b"]');
      await fallbackPartyB.locator("#f_contract_party_b").fill(tenantName);
      await fallbackPartyB
        .locator(`#partyBPickerList button[data-party-id="${tenantID}"]`)
        .click();
      const fallbackReuse = fallbackDialog.locator("#contractArchiveReuse");
      await expect(fallbackReuse).toBeVisible({ timeout: 15_000 });
      await fallbackReuse.locator('input[type="checkbox"]').nth(0).uncheck();
      await fallbackReuse.locator('input[type="checkbox"]').nth(1).uncheck();
      await fallbackDialog.getByRole("button", { name: "保存", exact: true }).click();
      await expect(fallbackDialog).toBeHidden();

      const fallbackRow = page
        .locator(`#contractTable tr[data-id]:has-text("${contractTitle}（现场签名）")`)
        .first();
      await expect(fallbackRow).toBeVisible();
      const fallbackID = (await fallbackRow.getAttribute("data-id")) ?? "";
      expect(fallbackID).not.toBe("");
      const detail = (await (
        await page.request.get(appUrl(page, `/app/api/v1/contracts/${fallbackID}`))
      ).json()) as { signatures?: unknown[]; seals?: unknown[] };
      expect(detail.signatures ?? []).toHaveLength(0);
      expect(detail.seals ?? []).toHaveLength(0);

      await fallbackRow.getByRole("button", { name: "扫码签名" }).click();
      const signDialog = page.getByRole("dialog").filter({
        has: page.getByRole("heading", { name: /扫码签名/ }),
      });
      await expect(signDialog).toBeVisible();
      await expect(signDialog.getByRole("button", { name: "生成签名二维码" })).toBeVisible();
      await signDialog.locator(".modal-foot").getByRole("button", { name: "关闭" }).click();
      await expect(signDialog).toBeHidden();
      await shot(page, testInfo, "09-取消复用后回退现场扫码");
    });

    await test.step("Q5 出租方档案默认复用；Q6 生成面板/批量预检与表单同口径", async () => {
      // 出租方（甲方）档案：单位 + 档案公章 + 档案签名章（桌面导入）。
      const orgName = `出租方企业 ${stamp}`;
      const orgID = await createParty(page, {
        kind: "单位",
        name: orgName,
        id_number: `91330100LANDLORD${stamp.slice(-6)}`,
        phone: "13800004444",
        address: "幸福村 3 组 12 号",
      });
      const seal = await sealPNG();
      const sealDataUrl = `data:image/png;base64,${seal.toString("base64")}`;
      for (const surface of ["seal", "signature-seal"]) {
        const response = await page.request.post(
          appUrl(page, `/app/api/v1/parties/${orgID}/${surface}`),
          { headers: APP_HEADERS, data: { image: sealDataUrl } },
        );
        expect(response.ok()).toBeTruthy();
      }

      // 设置页重新开启告知（上一步已关闭）；开启不弹确认。
      await page.locator('#tabs button[data-tab="settings"]').click();
      const toggle = page.locator("#archiveReuseToggle");
      await expect(toggle).not.toBeChecked();
      await toggle.check();
      await expect(toggle).toBeChecked();

      await page.locator('#tabs button[data-tab="contracts"]').click();
      await page.locator("#newContractBtn").click();
      const dialog = page.getByRole("dialog").filter({
        has: page.getByRole("heading", { name: "新建合同", exact: true }),
      });
      await expect(dialog).toBeVisible();
      const title = `${contractTitle}（甲方复用）`;
      await dialog.locator("#f_contract_title").fill(title);
      await expect(
        dialog.locator(`#f_contract_party_a option[value="${orgID}"]`),
      ).toHaveCount(1, { timeout: 15_000 });
      await dialog.locator("#f_contract_party_a").selectOption(orgID);
      const partyB = dialog.locator('.picker-field[data-key="party_b"]');
      await partyB.locator("#f_contract_party_b").fill(tenantName);
      await partyB
        .locator(`#partyBPickerList button[data-party-id="${tenantID}"]`)
        .click();

      // Q5：甲方档案复用区块默认勾选签名章与公章，并展示同口径告知。
      const partyAReuse = dialog.locator("#contractArchiveReuseA");
      await expect(partyAReuse).toBeVisible({ timeout: 15_000 });
      await expect(partyAReuse).toContainText("将复用档案签名（来源：档案）");
      await expect(partyAReuse).toContainText("与档案公章（来源：档案）");
      const aCheckboxes = partyAReuse.locator('input[type="checkbox"]');
      await expect(aCheckboxes).toHaveCount(2);
      await expect(aCheckboxes.nth(0)).toBeChecked();
      await expect(aCheckboxes.nth(1)).toBeChecked();
      await shot(page, testInfo, "10-甲方档案默认复用与告知");
      await dialog.getByRole("button", { name: "保存", exact: true }).click();
      await expect(dialog).toBeHidden();

      const row = page.locator(`#contractTable tr[data-id]:has-text("${title}")`).first();
      await expect(row).toBeVisible();
      const pairContractID = (await row.getAttribute("data-id")) ?? "";
      expect(pairContractID).not.toBe("");
      await expect
        .poll(async () => {
          const res = await page.request.get(
            appUrl(page, `/app/api/v1/contracts/${pairContractID}`),
          );
          const detail = (await res.json()) as {
            signatures?: Array<{ party_id?: string; source?: string }>;
            seals?: Array<{ id: string }>;
          };
          const signatures = detail.signatures ?? [];
          return {
            partyA: signatures
              .filter((item) => item.party_id === orgID)
              .map((item) => item.source),
            partyB: signatures
              .filter((item) => item.party_id === tenantID)
              .map((item) => item.source),
            seals: (detail.seals ?? []).length,
          };
        })
        .toEqual({ partyA: ["archive_seal"], partyB: ["archive_seal"], seals: 2 });

      // 单份合同「生成 PDF」面板：告知开启时展示来源与回退提示。
      await row.click();
      const edit = page.getByRole("dialog").filter({
        has: page.getByRole("heading", { name: /^编辑合同/ }),
      });
      await expect(edit).toBeVisible();
      await expect(edit.locator("#genTemplate")).toBeVisible({ timeout: 15_000 });
      const genNotice = edit.locator("#genArchiveReuseNotice");
      await expect(genNotice).toBeVisible();
      await expect(genNotice).toContainText("来源：档案");
      await page.keyboard.press("Escape");
      await expect(edit).toBeHidden();

      // 批量预检对话框：同一告知口径。
      await page.locator(`input[data-select="${pairContractID}"]`).check();
      await page.locator("#batchGenerateBtn").click();
      const batch = page.getByRole("dialog").filter({
        has: page.getByRole("heading", { name: /批量生成/ }),
      });
      await expect(batch).toBeVisible();
      await expect(batch.locator("#batchPreflight")).toBeVisible({ timeout: 15_000 });
      await expect(batch.locator("#batchArchiveReuseNotice")).toBeVisible();
      await expect(batch.locator("#batchArchiveReuseNotice")).toContainText("来源：档案");
      const preflightOn = await page.request.post(
        appUrl(page, `/app/api/v1/templates/${templateId}/preflight`),
        { headers: APP_HEADERS, data: { contract_id: pairContractID } },
      );
      expect(preflightOn.ok()).toBeTruthy();
      expect(((await preflightOn.json()) as { ready: boolean }).ready).toBe(true);
      await page.keyboard.press("Escape");
      await expect(batch).toBeHidden();

      // 关闭告知：三处都不再显示「来源：档案」，预检/生成行为不变。
      await page.locator('#tabs button[data-tab="settings"]').click();
      const toggleOff = page.locator("#archiveReuseToggle");
      page.once("dialog", (confirm) => confirm.accept());
      await toggleOff.uncheck();
      await expect(toggleOff).not.toBeChecked();

      await page.locator('#tabs button[data-tab="contracts"]').click();
      await page.locator(`#contractTable tr[data-id="${pairContractID}"]`).click();
      const editOff = page.getByRole("dialog").filter({
        has: page.getByRole("heading", { name: /^编辑合同/ }),
      });
      await expect(editOff).toBeVisible();
      await expect(editOff.locator("#genTemplate")).toBeVisible({ timeout: 15_000 });
      await expect(editOff.locator("#genArchiveReuseNotice")).toHaveCount(0);
      await page.keyboard.press("Escape");
      await expect(editOff).toBeHidden();

      await page.locator(`input[data-select="${pairContractID}"]`).check();
      await page.locator("#batchGenerateBtn").click();
      const batchOff = page.getByRole("dialog").filter({
        has: page.getByRole("heading", { name: /批量生成/ }),
      });
      await expect(batchOff).toBeVisible();
      await expect(batchOff.locator("#batchPreflight")).toBeVisible({ timeout: 15_000 });
      await expect(batchOff.locator("#batchArchiveReuseNotice")).toHaveCount(0);
      const preflightOff = await page.request.post(
        appUrl(page, `/app/api/v1/templates/${templateId}/preflight`),
        { headers: APP_HEADERS, data: { contract_id: pairContractID } },
      );
      expect(preflightOff.ok()).toBeTruthy();
      expect(((await preflightOff.json()) as { ready: boolean }).ready).toBe(true);
      await page.keyboard.press("Escape");
      await expect(batchOff).toBeHidden();
      await shot(page, testInfo, "11-关闭告知后生成面板与批量预检隐藏提示");
    });

    await test.step("海报停用：旧链接与二维码立即失效", async () => {
      await page.locator('#tabs button[data-tab="parties"]').click();
      await page.locator("#enrollPosterBtn").click();
      const dialog = page.getByRole("dialog").filter({
        has: page.getByRole("heading", { name: "自助录入海报（可打印）" }),
      });
      await expect(dialog).toBeVisible();
      page.once("dialog", (confirm) => confirm.accept());
      await dialog.locator("#posterDisableBtn").click();
      await expect(dialog.locator(".poster-state")).toContainText("入口已停用");
      const gone = await page.request.get(`${app.baseURL}/enroll/${enrollToken}`);
      expect(gone.status()).toBe(410);
      const qrGone = await page.request.get(
        appUrl(page, "/app/api/v1/enroll-entry/qr.png"),
      );
      expect(qrGone.status()).toBe(404);
      await shot(page, testInfo, "12-停用后旧海报与二维码失效");
      await dialog.locator(".modal-head button.close").click();
      await expect(dialog).toBeHidden();
    });

    expect(
      consoleErrors,
      `浏览器控制台出现错误：\n${consoleErrors.join("\n")}`,
    ).toEqual([]);
  });
});

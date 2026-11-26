// 场景 G：合同生命周期（te-x54oj）。
//
// 覆盖终止 → 重新出租 → 续签三条业务路径，以及到期提醒徽标：
//   - 生效合同在台账行内「终止」：终止日期 + 原因必填，终止后保留台账、
//     房产恢复空置；
//   - 终止后同一房产重新出租（新建生效合同不再被占用硬拦）；
//   - 续签合同生效时旧合同自动终止（原因=续签，终止日期=新起租日前一天；
//     起租日早于原到期日需要确认）；
//   - 生效合同租期 30 天内到期时台账标记「即将到期」，只提醒不改状态。
// 数据经页面接口准备，关键路径走界面，状态断言同时核对接口。
import { expect, test } from "./support/app-fixture";
import { shot, trackConsoleErrors } from "./support/ui";
import type { Locator, Page } from "@playwright/test";
import type { AppHandle } from "./support/app";

function apiHeaders(): Record<string, string> {
  return { "X-Tengshou-App": "1" };
}

interface SeededContract {
  id: string;
  contract_no: string;
}

async function seedParty(page: Page, app: AppHandle, name: string, idNumber: string): Promise<{ id: string; name: string }> {
  const response = await page.request.post(`${app.baseURL}/app/api/v1/parties`, {
    headers: apiHeaders(),
    data: { kind: "person", name, id_number: idNumber },
  });
  expect(response.ok()).toBeTruthy();
  const payload = (await response.json()) as { item: { id: string; name: string } };
  return payload.item;
}

async function seedAsset(page: Page, app: AppHandle, name: string): Promise<{ id: string; name: string }> {
  const response = await page.request.post(`${app.baseURL}/app/api/v1/assets`, {
    headers: apiHeaders(),
    data: { name, address: "E2E 生命周期路 7 号", room_no: "701" },
  });
  expect(response.ok()).toBeTruthy();
  const payload = (await response.json()) as { item: { id: string; name: string } };
  return payload.item;
}

async function seedContract(
  page: Page,
  app: AppHandle,
  tenantID: string,
  assetID: string,
  data: Record<string, unknown>,
): Promise<SeededContract> {
  const response = await page.request.post(`${app.baseURL}/app/api/v1/contracts`, {
    headers: apiHeaders(),
    data: { party_b_id: tenantID, asset_id: assetID, data: {}, ...data },
  });
  expect(response.ok()).toBeTruthy();
  const payload = (await response.json()) as { item: SeededContract };
  return payload.item;
}

async function assetLeaseStatus(page: Page, app: AppHandle, assetID: string): Promise<string> {
  const response = await page.request.get(`${app.baseURL}/app/api/v1/assets/${assetID}`);
  expect(response.ok()).toBeTruthy();
  const payload = (await response.json()) as { lease_status: string };
  return payload.lease_status;
}

async function contractStatus(page: Page, app: AppHandle, contractID: string): Promise<Record<string, unknown>> {
  const response = await page.request.get(`${app.baseURL}/app/api/v1/contracts/${contractID}`);
  expect(response.ok()).toBeTruthy();
  const payload = (await response.json()) as { contract: Record<string, unknown> };
  return payload.contract;
}

async function openLedger(page: Page, app: AppHandle): Promise<void> {
  await page.goto(`${app.baseURL}/app/`, { waitUntil: "domcontentloaded" });
  await page.locator('#tabs button[data-tab="contracts"]').click();
  await expect(page.locator("#view-contracts")).toHaveClass(/active/);
  await expect(page.locator("#contractTable tr[data-id]").first()).toBeVisible();
}

async function openNewContract(page: Page): Promise<Locator> {
  await page.locator("#newContractBtn").click();
  const dialog = page
    .getByRole("dialog")
    .filter({ has: page.getByRole("heading", { name: "新建合同" }) })
    .last();
  await expect(dialog).toBeVisible();
  return dialog;
}

async function choosePartyB(dialog: Locator, name: string): Promise<void> {
  const field = dialog.locator('.picker-field[data-key="party_b"]');
  await field.locator("#f_contract_party_b").fill(name);
  await field.locator(`#partyBPickerList button[data-party-id]:has-text("${name}")`).click();
  await expect(field.locator(".picker-selected")).toContainText(name);
}

async function chooseAsset(dialog: Locator, name: string): Promise<void> {
  const field = dialog.locator('.picker-field[data-key="asset"]');
  await field.locator("#f_contract_asset").fill(name);
  await field.locator(`#assetPickerList button[data-asset-id]:has-text("${name}")`).click();
  await expect(field.locator(".picker-selected")).toContainText(name);
}

test.describe("场景 G：终止 → 重新出租 → 续签", () => {
  test("行内终止释放房产，重新出租后按新起租日前一天联动终止", async ({
    app,
    page,
  }, testInfo) => {
    const consoleErrors = trackConsoleErrors(page);
    const stamp = Date.now().toString(36);
    const tenantName = `E2E 生命周期租户 ${stamp}`;
    const assetName = `E2E 生命周期房产 ${stamp}`;
    const tenant = await seedParty(page, app, tenantName, `LC${stamp}`);
    const asset = await seedAsset(page, app, assetName);
    const year = new Date().getFullYear();
    const first = await seedContract(page, app, tenant.id, asset.id, {
      year,
      title: `E2E 生命周期首合同 ${stamp}`,
      lease_start: `${year}-01-01`,
      lease_end: `${year}-12-31`,
      status: "active",
    });
    expect(await assetLeaseStatus(page, app, asset.id)).toBe("rented");

    await openLedger(page, app);
    const firstRow = page.locator(`#contractTable tr[data-id="${first.id}"]`);
    await expect(firstRow).toContainText(first.contract_no);

    await test.step("行内终止：日期与原因必填，终止后房产空置", async () => {
      await firstRow.locator('button[data-action="terminate"]').click();
      const dialog = page.locator("#terminateContractDialog");
      await expect(dialog).toBeVisible();
      await dialog.locator("#terminateContractDate").fill(`${year}-06-30`);
      await dialog.locator("#terminateContractReason").selectOption("early");
      await dialog.locator("#terminateContractNote").fill("E2E 提前退租");
      await dialog.getByRole("button", { name: "确认终止" }).click();
      await expect(dialog).toBeHidden();
      await expect(firstRow).toContainText("已终止");
      await expect
        .poll(() => assetLeaseStatus(page, app, asset.id))
        .toBe("vacant");
      await shot(page, testInfo, "01-首合同已终止");
    });

    await test.step("重新出租：重叠原租期的新生效合同可保存", async () => {
      const dialog = await openNewContract(page);
      await dialog.locator("#f_contract_title").fill(`E2E 重新出租合同 ${stamp}`);
      await choosePartyB(dialog, tenantName);
      await chooseAsset(dialog, assetName);
      await dialog.locator("#f_contract_lease_start").fill(`${year}-07-01`);
      await dialog.locator("#f_contract_lease_end").fill(`${year + 1}-06-30`);
      await dialog.locator("#f_contract_status").selectOption("生效");
      await dialog.getByRole("button", { name: "保存", exact: true }).click();
      await expect(dialog).toBeHidden();
      await expect.poll(() => assetLeaseStatus(page, app, asset.id)).toBe("rented");
      await shot(page, testInfo, "02-重新出租成功");
    });

    // 找到重新出租的合同行（标题含标记）。
    const secondRow = page.locator(`#contractTable tr[data-id]`, {
      hasText: `E2E 重新出租合同 ${stamp}`,
    });
    await expect(secondRow).toBeVisible();
    const secondID = (await secondRow.getAttribute("data-id")) ?? "";
    expect(secondID).not.toBe("");

    await test.step("续签：起租早于原到期日需确认，确认后按新起租日前一天终止", async () => {
      await secondRow.click();
      const editDialog = page
        .getByRole("dialog")
        .filter({ has: page.getByRole("heading", { name: /编辑合同/ }) });
      await expect(editDialog).toBeVisible();
      await editDialog.getByRole("button", { name: "续签" }).click();

      const renewalDialog = page
        .getByRole("dialog")
        .filter({ has: page.getByRole("heading", { name: /续签自/ }) });
      await expect(renewalDialog).toBeVisible();
      await renewalDialog.locator("#f_contract_title").fill(`E2E 续签合同 ${stamp}`);
      await renewalDialog.locator("#f_contract_lease_start").fill(`${year + 1}-01-01`);
      await renewalDialog.locator("#f_contract_lease_end").fill(`${year + 1}-12-31`);
      await renewalDialog.locator("#f_contract_status").selectOption("生效");

      const confirmText: string[] = [];
      page.once("dialog", (dialog) => {
        confirmText.push(dialog.message());
        void dialog.accept();
      });
      await renewalDialog.getByRole("button", { name: "保存", exact: true }).click();
      await expect(renewalDialog).toBeHidden();
      await expect.poll(() => confirmText.length).toBe(1);
      expect(confirmText[0]).toContain(`${year}-12-31`);

      // 原合同按确认结果自动终止；新合同生效并占用房产。
      await expect
        .poll(async () => (await contractStatus(page, app, secondID)).status)
        .toBe("terminated");
      const closed = await contractStatus(page, app, secondID);
      expect(closed.termination_reason).toBe("renewal");
      expect(closed.terminated_on).toBe(`${year}-12-31`);
      await expect
        .poll(async () => (await contractStatus(page, app, first.id)).status)
        .toBe("terminated");
      await shot(page, testInfo, "03-续签联动终止原合同");
    });

    expect(
      consoleErrors,
      `浏览器控制台出现错误：\n${consoleErrors.join("\n")}`,
    ).toEqual([]);
  });

  test("租期 30 天内到期的生效合同在台账标记「即将到期」，不改状态", async ({
    app,
    page,
  }, testInfo) => {
    const consoleErrors = trackConsoleErrors(page);
    const stamp = Date.now().toString(36);
    const tenant = await seedParty(page, app, `E2E 到期租户 ${stamp}`, `EX${stamp}`);
    const asset = await seedAsset(page, app, `E2E 到期房产 ${stamp}`);
    const soon = new Date();
    soon.setDate(soon.getDate() + 10);
    const end = soon.toISOString().slice(0, 10);
    const contract = await seedContract(page, app, tenant.id, asset.id, {
      year: new Date().getFullYear(),
      title: `E2E 即将到期合同 ${stamp}`,
      lease_start: new Date().toISOString().slice(0, 10),
      lease_end: end,
      status: "active",
    });

    await openLedger(page, app);
    const row = page.locator(`#contractTable tr[data-id="${contract.id}"]`);
    await expect(row).toContainText("即将到期");
    await expect(page.locator("#contractSummary")).toContainText("即将到期 1 份");
    // 顶栏计数同样来自 bootstrap 的 stats（REQ-002）。
    await expect(page.locator("#meta")).toContainText("即将到期 1 份");
    // 提醒不改状态。
    expect((await contractStatus(page, app, contract.id)).status).toBe("active");
    await shot(page, testInfo, "04-到期提醒徽标");

    expect(
      consoleErrors,
      `浏览器控制台出现错误：\n${consoleErrors.join("\n")}`,
    ).toEqual([]);
  });
});

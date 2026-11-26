// 场景 E/F：合同表单里的档案选择细节。
//
// 覆盖 te-thrth 需求中此前没有自动化验证的界面行为：
//   - 乙方「最近使用」与租户/房产的「快速新建」入口（REQ-002 / REQ-003）；
//   - 选中同证件号档案时的判重警告（REQ-006 界面侧）；
//   - 保存与在租房产重叠的租期时，草稿 toast / 生效硬拦都展示冲突合同编号
//     （REQ-003 界面侧）。
// 只驱动发布形态的 tengshou-cli serve；数据经页面接口准备，断言走界面。
import { expect, test } from "./support/app-fixture";
import { shot, trackConsoleErrors } from "./support/ui";
import type { Locator, Page } from "@playwright/test";
import type { AppHandle } from "./support/app";

async function openNewContract(page: Page): Promise<Locator> {
  await page.locator('#tabs button[data-tab="contracts"]').click();
  await expect(page.locator("#view-contracts")).toHaveClass(/active/);
  await page.locator("#newContractBtn").click();
  const dialog = page
    .getByRole("dialog")
    .filter({ has: page.getByRole("heading", { name: "新建合同" }) });
  await expect(dialog).toBeVisible();
  return dialog;
}

async function choosePartyB(dialog: Locator, name: string): Promise<void> {
  const field = dialog.locator('.picker-field[data-key="party_b"]');
  await field.locator("#f_contract_party_b").fill(name);
  await field
    .locator(`#partyBPickerList button[data-party-id]:has-text("${name}")`)
    .click();
  await expect(field.locator(".picker-selected")).toContainText(name);
}

async function chooseAsset(dialog: Locator, name: string): Promise<void> {
  const field = dialog.locator('.picker-field[data-key="asset"]');
  await field.locator("#f_contract_asset").fill(name);
  await field
    .locator(`#assetPickerList button[data-asset-id]:has-text("${name}")`)
    .click();
  await expect(field.locator(".picker-selected")).toContainText(name);
}

async function seedParty(
  page: Page,
  app: AppHandle,
  name: string,
  idNumber: string,
): Promise<{ id: string; name: string }> {
  const response = await page.request.post(`${app.baseURL}/app/api/v1/parties`, {
    headers: { "X-Tengshou-App": "1" },
    data: { kind: "person", name, id_number: idNumber },
  });
  expect(response.ok()).toBeTruthy();
  const payload = (await response.json()) as { item: { id: string; name: string } };
  return payload.item;
}

test.describe("场景 E：快速新建入口与最近使用", () => {
  test("快速新建租户与房产后自动选中，刷新后最近使用列出该租户", async ({
    app,
    page,
  }, testInfo) => {
    const consoleErrors = trackConsoleErrors(page);
    const stamp = Date.now().toString(36);
    const tenantName = `E2E 快速租户 ${stamp}`;
    const tenantIDNumber = `Q${stamp}`;
    const assetName = `E2E 快速房产 ${stamp}`;
    const toast = page.locator("#toast");

    await page.goto(`${app.baseURL}/app/`, { waitUntil: "domcontentloaded" });
    const dialog = await openNewContract(page);
    const partyField = dialog.locator('.picker-field[data-key="party_b"]');
    const assetField = dialog.locator('.picker-field[data-key="asset"]');

    await test.step("快速新建租户：证件号必填，缺失时提示", async () => {
      await partyField.locator("#quickCreatePartyBtn").click();
      await partyField.locator("#quickPartyName").fill(tenantName);
      await partyField.getByRole("button", { name: "建档并选中" }).click();
      await expect(toast).toHaveClass(/show/, { timeout: 10_000 });
      await expect(toast).toContainText("证件号必填");
      await shot(page, testInfo, "01-快速新建租户缺少证件号");
    });

    await test.step("补上证号后建档并自动选中", async () => {
      await partyField.locator("#quickPartyIDNumber").fill(tenantIDNumber);
      await partyField.getByRole("button", { name: "建档并选中" }).click();
      const selected = partyField.locator(".picker-selected");
      await expect(selected).toContainText(tenantName);
      await expect(selected).toContainText(tenantIDNumber);
      await shot(page, testInfo, "02-快速新建租户已选中");
    });

    await test.step("快速新建房产：名称必填，建档后自动选中", async () => {
      await assetField.locator("#quickCreateAssetBtn").click();
      await assetField.getByRole("button", { name: "建档并选中" }).click();
      await expect(toast).toHaveClass(/show/, { timeout: 10_000 });
      await expect(toast).toContainText("请填写房产名称");
      await assetField.locator("#quickAssetName").fill(assetName);
      await assetField.locator("#quickAssetAddress").fill("E2E 快速路 8 号");
      await assetField.getByRole("button", { name: "建档并选中" }).click();
      const selected = assetField.locator(".picker-selected");
      await expect(selected).toContainText(assetName);
      await expect(selected).toContainText("空置");
      await shot(page, testInfo, "03-快速新建房产已选中");
    });

    await test.step("刷新后「最近使用」列出刚建的租户", async () => {
      await dialog.getByRole("button", { name: "取消" }).click();
      await expect(dialog).toBeHidden();
      // 等首屏合同/档案列表请求全部结束再刷新：WebKit 会把刷新时被中断的
      // 在途请求记成页面异常（E2E 竞态，与业务无关）。
      await expect(page.locator("#contractSummary")).toContainText(/共 \d+ 份/);
      await page.reload({ waitUntil: "domcontentloaded" });
      const reopened = await openNewContract(page);
      const field = reopened.locator('.picker-field[data-key="party_b"]');
      await field.locator("#f_contract_party_b").click();
      const list = field.locator("#partyBPickerList");
      await expect(list).toBeVisible();
      await expect(list.locator(".picker-caption").first()).toHaveText("最近使用");
      await expect(
        list.locator(`button[data-party-id]:has-text("${tenantName}")`).first(),
      ).toBeVisible();
      await shot(page, testInfo, "04-最近使用含新建租户");
    });

    expect(
      consoleErrors,
      `浏览器控制台出现错误：\n${consoleErrors.join("\n")}`,
    ).toEqual([]);
  });
});

test.describe("场景 F：证件号判重与租期重叠的界面提示", () => {
  test("选中同证件号档案时界面给出判重警告", async ({ app, page }, testInfo) => {
    const consoleErrors = trackConsoleErrors(page);
    const stamp = Date.now().toString(36);
    const dupIDNumber = `DUP${stamp}`;
    const firstName = `E2E 判重甲 ${stamp}`;
    const secondName = `E2E 判重乙 ${stamp}`;

    await seedParty(page, app, firstName, dupIDNumber);
    await seedParty(page, app, secondName, dupIDNumber);

    await page.goto(`${app.baseURL}/app/`, { waitUntil: "domcontentloaded" });
    const dialog = await openNewContract(page);
    await choosePartyB(dialog, firstName);

    const warning = dialog.locator('.picker-field[data-key="party_b"] .help.warn');
    await expect(warning).toContainText("判重以证件号为准");
    await expect(warning).toContainText(secondName);
    await shot(page, testInfo, "01-同证件号判重警告");

    expect(
      consoleErrors,
      `浏览器控制台出现错误：\n${consoleErrors.join("\n")}`,
    ).toEqual([]);
  });

  test("保存与在租房产重叠的租期时，toast 提示生效合同编号与拦截规则", async ({
    app,
    page,
  }, testInfo) => {
    const consoleErrors = trackConsoleErrors(page);
    const stamp = Date.now().toString(36);
    const tenant = await seedParty(page, app, `E2E 重叠租户 ${stamp}`, `OV${stamp}`);
    const assetName = `E2E 重叠房产 ${stamp}`;
    const toast = page.locator("#toast");

    const assetResponse = await page.request.post(
      `${app.baseURL}/app/api/v1/assets`,
      {
        headers: { "X-Tengshou-App": "1" },
        data: { name: assetName, address: "E2E 重叠路 3 号", room_no: "303" },
      },
    );
    expect(assetResponse.ok()).toBeTruthy();
    const asset = ((await assetResponse.json()) as { item: { id: string } }).item;

    const firstResponse = await page.request.post(
      `${app.baseURL}/app/api/v1/contracts`,
      {
        headers: { "X-Tengshou-App": "1" },
        data: {
          year: new Date().getFullYear(),
          title: `E2E 重叠首合同 ${stamp}`,
          party_b_id: tenant.id,
          asset_id: asset.id,
          lease_start: "2026-01-01",
          lease_end: "2026-12-31",
          status: "active",
          data: {},
        },
      },
    );
    expect(firstResponse.ok()).toBeTruthy();
    const first = ((await firstResponse.json()) as {
      item: { id: string; contract_no: string };
    }).item;
    expect(first.contract_no).not.toBe("");

    await page.goto(`${app.baseURL}/app/`, { waitUntil: "domcontentloaded" });
    const dialog = await openNewContract(page);
    await dialog.locator("#f_contract_title").fill(`E2E 重叠新合同 ${stamp}`);
    await choosePartyB(dialog, tenant.name);
    await chooseAsset(dialog, assetName);
    await dialog.locator("#f_contract_lease_start").fill("2026-06-01");
    await dialog.locator("#f_contract_lease_end").fill("2027-05-31");
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(dialog).toBeHidden();

    await expect(toast).toContainText(
      `租期与生效合同（编号 ${first.contract_no}）重叠`,
      { timeout: 10_000 },
    );
    await expect(toast).toHaveClass(/show/);
    await shot(page, testInfo, "01-租期重叠提示");

    await test.step("保存「生效」重叠合同：弹窗保留、错误含冲突编号、不新建合同", async () => {
      const listResponse = await page.request.get(`${app.baseURL}/app/api/v1/contracts`);
      expect(listResponse.ok()).toBeTruthy();
      const totalBefore = ((await listResponse.json()) as { total: number }).total;

      const activeDialog = await openNewContract(page);
      await activeDialog.locator("#f_contract_title").fill(`E2E 重叠生效合同 ${stamp}`);
      await choosePartyB(activeDialog, tenant.name);
      await chooseAsset(activeDialog, assetName);
      await activeDialog.locator("#f_contract_lease_start").fill("2026-06-01");
      await activeDialog.locator("#f_contract_lease_end").fill("2027-05-31");
      await activeDialog.locator("#f_contract_status").selectOption("生效");
      await activeDialog.getByRole("button", { name: "保存", exact: true }).click();

      // 硬拦：弹窗保持并显示冲突合同编号，不跳转、不关闭。
      await expect(activeDialog).toBeVisible();
      await expect(activeDialog.locator("#contractErrors")).toContainText(
        `租期与生效合同（编号 ${first.contract_no}）重叠`,
      );
      await shot(page, testInfo, "02-生效重叠硬拦");
      await activeDialog.getByRole("button", { name: "取消" }).click();
      await expect(activeDialog).toBeHidden();

      const afterResponse = await page.request.get(`${app.baseURL}/app/api/v1/contracts`);
      expect(afterResponse.ok()).toBeTruthy();
      const totalAfter = ((await afterResponse.json()) as { total: number }).total;
      expect(totalAfter).toBe(totalBefore);
    });

    // 「生效」硬拦是预期行为：浏览器会把 409 记为一条资源加载错误，
    // 这里只忽略这条，其余控制台错误仍必须为空。
    expect(
      consoleErrors.filter((text) => !text.includes("409 (Conflict)")),
      `浏览器控制台出现错误：\n${consoleErrors.join("\n")}`,
    ).toEqual([]);
  });
});

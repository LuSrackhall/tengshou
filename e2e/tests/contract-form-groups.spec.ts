// 合同表单分组（te-bzqhx）：12 个租赁业务字段设 4 个显式分组 + 1 个未分组
// 字段后，新建/编辑表单按「基本信息 / 租期 / 租金与押金 / 费用与违约 /
// 自定义分组 / 其他约定」分区展示，默认展开、可折叠、可保存且回读不丢值；
// 内置字段（无 group）与未分组动态字段保持完整。
// 只驱动发布形态 tengshou-cli serve；数据经页面接口准备，断言走界面。
import type { Locator, Page } from "@playwright/test";
import { expect, test } from "./support/app-fixture";
import type { AppHandle } from "./support/app";
import { shot, trackConsoleErrors } from "./support/ui";

interface SeedField {
  key: string;
  label: string;
  /** 省略表示未分组，应落「其他约定」。 */
  group?: string;
}

// 12 个租赁字段：四个显式分组各 3/3/3/2 个，另加 1 个未分组字段。
const SEED_GROUPS: Array<{ name: string; fields: SeedField[] }> = [
  {
    name: "租期",
    fields: [
      { key: "rent_free_days", label: "免租期（天）" },
      { key: "renewal_mode", label: "续租方式" },
      { key: "early_exit_terms", label: "提前退租条款" },
    ],
  },
  {
    name: "租金与押金",
    fields: [
      { key: "monthly_rent", label: "月租金（元）" },
      { key: "deposit_amount", label: "押金金额（元）" },
      { key: "rent_pay_mode", label: "租金支付方式" },
    ],
  },
  {
    name: "费用与违约",
    fields: [
      { key: "property_fee_owner", label: "物业费承担方" },
      { key: "penalty_rate", label: "违约金比例" },
      { key: "utility_settlement", label: "水电费结算方式" },
    ],
  },
  {
    name: "房屋信息",
    fields: [
      { key: "house_area", label: "房屋面积" },
      { key: "house_floor", label: "所在楼层" },
    ],
  },
];

const UNGROUPED: SeedField = { key: "inventory_note", label: "物品清单备注" };

async function seedField(
  page: Page,
  app: AppHandle,
  field: SeedField,
): Promise<void> {
  const response = await page.request.post(`${app.baseURL}/app/api/v1/fields`, {
    headers: { "X-Tengshou-App": "1" },
    data: {
      entity: "contract",
      type: "text",
      key: field.key,
      label: field.label,
      ...(field.group === undefined ? {} : { group: field.group }),
    },
  });
  expect(response.ok(), `新建字段 ${field.label} 失败`).toBeTruthy();
}

async function seedParty(
  page: Page,
  app: AppHandle,
  name: string,
): Promise<void> {
  const response = await page.request.post(`${app.baseURL}/app/api/v1/parties`, {
    headers: { "X-Tengshou-App": "1" },
    data: { kind: "person", name, id_number: `G${name}` },
  });
  expect(response.ok(), `新建相对方 ${name} 失败`).toBeTruthy();
}

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

function section(dialog: Locator, name: string): Locator {
  return dialog.locator(`details.form-section[data-group="${name}"]`);
}

/** 断言某分组存在且包含这些字段键（内置控件与动态字段共用 data-key）。 */
async function expectSectionKeys(
  dialog: Locator,
  name: string,
  keys: string[],
): Promise<void> {
  const group = section(dialog, name);
  await expect(group).toBeVisible();
  for (const key of keys) {
    await expect(
      group.locator(`[data-key="${key}"]`),
      `分组「${name}」缺少字段 ${key}`,
    ).toBeVisible();
  }
}

test.describe("合同表单分组", () => {
  test("12 个租赁字段设 4 组后分区渲染、可折叠、可保存且回读不丢值", async ({
    app,
    page,
  }, testInfo) => {
    const consoleErrors = trackConsoleErrors(page);
    const tenantName = "E2E 分组租户";
    const contractTitle = "E2E 分组租赁合同";

    for (const group of SEED_GROUPS) {
      for (const field of group.fields) {
        await seedField(page, app, { ...field, group: group.name });
      }
    }
    await seedField(page, app, UNGROUPED);
    await seedParty(page, app, tenantName);

    await page.goto(`${app.baseURL}/app/`, { waitUntil: "domcontentloaded" });
    await expect(page.locator("#tabs")).toBeVisible();
    const dialog = await openNewContract(page);

    await test.step("分区按预设顺序出现，全部默认展开", async () => {
      const sections = dialog.locator("details.form-section");
      await expect(sections).toHaveCount(6);
      await expect(sections.locator("summary")).toHaveText([
        "基本信息",
        "租期",
        "租金与押金",
        "费用与违约",
        "房屋信息",
        "其他约定",
      ]);
      for (const group of await sections.all()) {
        await expect(group).toHaveAttribute("open", "");
      }
      await shot(page, testInfo, "01-分组默认展开");
    });

    await test.step("内置字段固定归组，动态字段按 group 归位，未分组落其他约定", async () => {
      await expectSectionKeys(dialog, "基本信息", [
        "contract_no",
        "year",
        "title",
        "type",
        "status",
        "signed_on",
        "party_b",
        "asset",
      ]);
      await expect(
        section(dialog, "基本信息").locator("#f_contract_party_a"),
      ).toBeVisible();
      await expectSectionKeys(dialog, "租期", [
        "lease_start",
        "lease_end",
        "valid_from",
        "valid_to",
        "rent_free_days",
        "renewal_mode",
        "early_exit_terms",
      ]);
      await expectSectionKeys(dialog, "租金与押金", [
        "amount",
        "monthly_rent",
        "deposit_amount",
        "rent_pay_mode",
      ]);
      await expectSectionKeys(dialog, "费用与违约", [
        "property_fee_owner",
        "penalty_rate",
        "utility_settlement",
      ]);
      await expectSectionKeys(dialog, "房屋信息", ["house_area", "house_floor"]);
      await expectSectionKeys(dialog, "其他约定", ["inventory_note"]);
    });

    await test.step("分组可折叠，再次点击恢复展开", async () => {
      const lease = section(dialog, "租期");
      const leaseStart = lease.locator('[data-key="lease_start"]');
      await expect(leaseStart).toBeVisible();
      await lease.locator("summary").click();
      await expect(leaseStart).toBeHidden();
      await shot(page, testInfo, "02-租期分组已折叠");
      await lease.locator("summary").click();
      await expect(leaseStart).toBeVisible();
    });

    await test.step("选择乙方、填写分组字段后保存成功", async () => {
      const partyField = dialog.locator('.picker-field[data-key="party_b"]');
      await partyField.locator("#f_contract_party_b").fill(tenantName);
      await partyField
        .locator(`#partyBPickerList button[data-party-id]:has-text("${tenantName}")`)
        .click();
      await expect(partyField.locator(".picker-selected")).toContainText(tenantName);

      await dialog.locator("#f_contract_title").fill(contractTitle);
      await dialog.locator("#f_contract_monthly_rent").fill("3000");
      await dialog.locator("#f_contract_inventory_note").fill("含家具家电");
      await dialog.getByRole("button", { name: "保存", exact: true }).click();
      await expect(dialog).toBeHidden();
      await expect(page.locator("#toast")).toContainText("合同已保存");
      await expect(
        page.locator(`#contractTable tr[data-id]:has-text("${contractTitle}")`),
      ).toBeVisible();
      await shot(page, testInfo, "03-合同已保存");
    });

    await test.step("编辑回读：分组仍在，已保存值不丢", async () => {
      const row = page.locator(
        `#contractTable tr[data-id]:has-text("${contractTitle}")`,
      );
      await row.locator('button[data-action="edit"]').click();
      const editDialog = page
        .getByRole("dialog")
        .filter({ has: page.getByRole("heading", { name: /编辑合同/ }) });
      await expect(editDialog).toBeVisible();
      await expect(editDialog.locator("details.form-section")).toHaveCount(6);
      await expect(editDialog.locator("#f_contract_monthly_rent")).toHaveValue(
        "3000",
      );
      await expect(editDialog.locator("#f_contract_inventory_note")).toHaveValue(
        "含家具家电",
      );
      await expectSectionKeys(editDialog, "其他约定", ["inventory_note"]);
      await shot(page, testInfo, "04-编辑回读分组与值");
      await editDialog.getByRole("button", { name: "取消", exact: true }).click();
      await expect(editDialog).toBeHidden();
    });

    expect(
      consoleErrors,
      `浏览器控制台出现错误：\n${consoleErrors.join("\n")}`,
    ).toEqual([]);
  });
});

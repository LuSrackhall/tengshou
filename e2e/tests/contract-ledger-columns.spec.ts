// 合同台账列设置（te-iuns4）：12 个租赁动态字段在台账里默认不建列，只显示
// 关键列（合同编号/年度/合同名称/合同类型/相对方/金额/签订日期/租赁起租日/
// 租赁到期日/状态），1440 宽屏无横向溢出；「列设置」勾选动态列即时出现、
// 刷新后保持；「恢复默认」清掉本机偏好后动态列消失。未勾选的字段仍在合同
// 编辑表单中（模板与 Excel 导出同理由服务端字段定义驱动）。
// 只驱动发布形态 tengshou-cli serve；数据经页面接口准备，断言走界面。
import type { Locator, Page } from "@playwright/test";
import { expect, test } from "./support/app-fixture";
import type { AppHandle } from "./support/app";
import { shot, trackConsoleErrors } from "./support/ui";

const API_HEADERS = { "X-Tengshou-App": "1" };
const STORAGE_KEY = "tengshou.contractLedgerColumns";

// 与合同表单分组用例一致的 12 个租赁业务字段：默认都不进台账列。
const RENTAL_FIELDS: Array<{ key: string; label: string }> = [
  { key: "rent_free_days", label: "免租期（天）" },
  { key: "renewal_mode", label: "续租方式" },
  { key: "early_exit_terms", label: "提前退租条款" },
  { key: "monthly_rent", label: "月租金（元）" },
  { key: "deposit_amount", label: "押金金额（元）" },
  { key: "rent_pay_mode", label: "租金支付方式" },
  { key: "property_fee_owner", label: "物业费承担方" },
  { key: "penalty_rate", label: "违约金比例" },
  { key: "utility_settlement", label: "水电费结算方式" },
  { key: "house_area", label: "房屋面积" },
  { key: "house_floor", label: "所在楼层" },
  { key: "inventory_note", label: "物品清单备注" },
];

// 默认关键列（与台账列设置的默认勾选一致）。
const KEY_COLUMNS: Array<{ key: string; label: string }> = [
  { key: "contract_no", label: "合同编号" },
  { key: "year", label: "年度" },
  { key: "title", label: "合同名称" },
  { key: "type", label: "合同类型" },
  { key: "party_b", label: "相对方" },
  { key: "amount", label: "金额（元）" },
  { key: "signed_on", label: "签订日期" },
  { key: "lease_start", label: "租赁起租日" },
  { key: "lease_end", label: "租赁到期日" },
  { key: "status", label: "状态" },
];

/** 同一 worker 的合同表单分组用例使用同一组字段键（共享服务与数据目录）。 */
async function existingFieldKeys(page: Page, app: AppHandle): Promise<Set<string>> {
  const response = await page.request.get(
    `${app.baseURL}/app/api/v1/fields?entity=contract&archived=1`,
    { headers: API_HEADERS },
  );
  expect(response.ok(), "读取合同字段列表失败").toBeTruthy();
  const payload = (await response.json()) as { items?: Array<{ key: string }> };
  return new Set((payload.items ?? []).map((field) => field.key));
}

async function seedField(
  page: Page,
  app: AppHandle,
  field: { key: string; label: string },
  existing: Set<string>,
): Promise<void> {
  if (existing.has(field.key)) {
    // 字段已存在（另一用例先建过）时直接复用，避免重复建字段 409 让套件
    // 结果依赖用例调度顺序；本用例只依赖这些字段存在且默认不进台账列。
    return;
  }
  const response = await page.request.post(`${app.baseURL}/app/api/v1/fields`, {
    headers: API_HEADERS,
    data: { entity: "contract", type: "text", key: field.key, label: field.label },
  });
  expect(response.ok(), `新建字段 ${field.label} 失败`).toBeTruthy();
  existing.add(field.key);
}

async function seedParty(
  page: Page,
  app: AppHandle,
  name: string,
): Promise<{ id: string; name: string }> {
  const response = await page.request.post(`${app.baseURL}/app/api/v1/parties`, {
    headers: API_HEADERS,
    data: { kind: "person", name, id_number: `LC${name}` },
  });
  expect(response.ok(), `新建相对方 ${name} 失败`).toBeTruthy();
  const payload = (await response.json()) as { item: { id: string; name: string } };
  return payload.item;
}

async function seedContract(
  page: Page,
  app: AppHandle,
  data: Record<string, unknown>,
): Promise<{ id: string }> {
  const response = await page.request.post(`${app.baseURL}/app/api/v1/contracts`, {
    headers: API_HEADERS,
    data,
  });
  expect(response.ok(), "新建合同失败").toBeTruthy();
  const payload = (await response.json()) as { item: { id: string } };
  return payload.item;
}

function header(page: Page, label: string): Locator {
  return page.getByRole("columnheader", { name: label, exact: true });
}

async function openLedger(page: Page, app: AppHandle): Promise<void> {
  await page.goto(`${app.baseURL}/app/`, { waitUntil: "domcontentloaded" });
  await page.locator('#tabs button[data-tab="contracts"]').click();
  await expect(page.locator("#view-contracts")).toHaveClass(/active/);
  await expect(page.locator("#contractTable tr[data-id]").first()).toBeVisible();
}

async function openColumnSettings(page: Page): Promise<Locator> {
  await page.locator("#ledgerColumnsBtn").click();
  const dialog = page
    .getByRole("dialog")
    .filter({ has: page.getByRole("heading", { name: "台账列设置" }) });
  await expect(dialog).toBeVisible();
  return dialog;
}

async function tableOverflow(page: Page): Promise<number> {
  return await page
    .locator("#view-contracts .table-wrap")
    .evaluate((element) => element.scrollWidth - element.clientWidth);
}

test.describe("合同台账列设置", () => {
  test("默认关键列无横向溢出，勾选动态列持久化，恢复默认后隐藏", async ({
    app,
    page,
  }, testInfo) => {
    const consoleErrors = trackConsoleErrors(page);
    const stamp = Date.now().toString(36);
    const existingFields = await existingFieldKeys(page, app);
    for (const field of RENTAL_FIELDS) {
      await seedField(page, app, field, existingFields);
    }
    const tenantName = `E2E 列设置租户 ${stamp}`;
    const tenant = await seedParty(page, app, tenantName);
    const year = new Date().getFullYear();
    const createdContracts: string[] = [];
    try {
      const contract = await seedContract(page, app, {
        party_b_id: tenant.id,
        year,
        title: `E2E 列设置合同 ${stamp}`,
        type: "租赁",
        status: "active",
        signed_on: `${year}-01-05`,
        lease_start: `${year}-02-01`,
        lease_end: `${year + 1}-01-31`,
        amount: 4200,
        data: { monthly_rent: "3500", house_area: "89.5" },
      });
      createdContracts.push(contract.id);
      // 30 天内到期的合同：状态列带「即将到期」徽标，默认布局仍不能横向溢出。
      const soon = new Date();
      soon.setDate(soon.getDate() + 10);
      const expiring = await seedContract(page, app, {
        party_b_id: tenant.id,
        year,
        title: `E2E 列设置到期合同 ${stamp}`,
        type: "租赁",
        status: "active",
        signed_on: `${year}-01-05`,
        lease_start: `${year}-02-01`,
        lease_end: soon.toISOString().slice(0, 10),
        amount: 2800,
        data: { monthly_rent: "2800" },
      });
      createdContracts.push(expiring.id);

      await openLedger(page, app);

      await test.step("12 个租赁字段下默认只显示关键列", async () => {
        for (const column of KEY_COLUMNS) {
          await expect(header(page, column.label), `缺少关键列 ${column.label}`).toBeVisible();
        }
        await expect(header(page, "月租金（元）")).toHaveCount(0);
        await expect(header(page, "房屋面积")).toHaveCount(0);
        // 生效日期/到期日期是内置字段但不在默认关键列，同样默认隐藏。
        await expect(header(page, "生效日期")).toHaveCount(0);
        // 到期徽标行为不变：默认状态列仍显示「即将到期」。
        await expect(
          page.locator(`#contractTable tr[data-id="${expiring.id}"] td.status-cell`),
        ).toContainText("即将到期");
        await shot(page, testInfo, "01-默认关键列");
      });

      await test.step("默认台账与页面均无横向溢出", async () => {
        expect(await tableOverflow(page), "台账表格超出容器宽度").toBeLessThanOrEqual(1);
        const pageOverflow = await page.evaluate(
          () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
        );
        expect(pageOverflow, "页面出现横向滚动").toBeLessThanOrEqual(0);
      });

      await test.step("勾选月租金后列立即出现并持久化", async () => {
        const dialog = await openColumnSettings(page);
        await expect(dialog.locator('[data-column-key="monthly_rent"]')).not.toBeChecked();
        for (const column of KEY_COLUMNS) {
          await expect(
            dialog.locator(`[data-column-key="${column.key}"]`),
            `默认应勾选 ${column.label}`,
          ).toBeChecked();
        }
        await dialog.locator('[data-column-key="monthly_rent"]').check();
        await expect(header(page, "月租金（元）")).toBeVisible();
        await expect(
          page.locator(`#contractTable tr[data-id="${contract.id}"] td`, { hasText: "3500" }).first(),
        ).toBeVisible();
        await shot(page, testInfo, "02-勾选月租金");
        await dialog.getByRole("button", { name: "完成" }).click();
        await expect(dialog).toBeHidden();
        const stored = await page.evaluate(
          (key) => window.localStorage.getItem(key),
          STORAGE_KEY,
        );
        expect(stored ?? "").toContain("monthly_rent");
      });

      await test.step("刷新后勾选的列仍显示", async () => {
        await page.reload({ waitUntil: "domcontentloaded" });
        await page.locator('#tabs button[data-tab="contracts"]').click();
        await expect(page.locator("#contractTable tr[data-id]").first()).toBeVisible();
        await expect(header(page, "月租金（元）")).toBeVisible();
        await expect(header(page, "合同编号")).toBeVisible();
      });

      await test.step("恢复默认后动态列消失且本机偏好清空", async () => {
        const dialog = await openColumnSettings(page);
        await expect(dialog.locator('[data-column-key="monthly_rent"]')).toBeChecked();
        await dialog.locator("#restoreLedgerColumnsBtn").click();
        await expect(header(page, "月租金（元）")).toHaveCount(0);
        await expect(dialog.locator('[data-column-key="monthly_rent"]')).not.toBeChecked();
        for (const column of KEY_COLUMNS) {
          await expect(dialog.locator(`[data-column-key="${column.key}"]`)).toBeChecked();
        }
        await shot(page, testInfo, "03-恢复默认");
        await dialog.getByRole("button", { name: "完成" }).click();
        await expect(dialog).toBeHidden();
        const stored = await page.evaluate(
          (key) => window.localStorage.getItem(key),
          STORAGE_KEY,
        );
        expect(stored).toBeNull();
        expect(await tableOverflow(page), "恢复默认后台账仍应无横向溢出").toBeLessThanOrEqual(1);
      });

      await test.step("至少保留一列：最后一列不可取消", async () => {
        const dialog = await openColumnSettings(page);
        const keyCheckboxes = KEY_COLUMNS.map((column) =>
          dialog.locator(`[data-column-key="${column.key}"]`),
        );
        // 取消除合同编号外的 9 个关键列后，台账只剩合同编号一列。
        for (const checkbox of keyCheckboxes.slice(1)) {
          await checkbox.uncheck();
        }
        await expect(header(page, "合同编号")).toBeVisible();
        await expect(header(page, "状态")).toHaveCount(0);
        // 取消最后一列被拦下：复选框保持勾选并提示「至少保留一列」。
        // 用 click 而非 uncheck：uncheck 会断言控件变为未勾选，而守卫会恢复勾选。
        await keyCheckboxes[0]!.click();
        await expect(dialog.locator(".ledger-column-hint")).toHaveText("至少保留一列");
        await expect(keyCheckboxes[0]!).toBeChecked();
        await dialog.locator("#restoreLedgerColumnsBtn").click();
        for (const column of KEY_COLUMNS) {
          await expect(dialog.locator(`[data-column-key="${column.key}"]`)).toBeChecked();
        }
        await dialog.getByRole("button", { name: "完成" }).click();
        await expect(dialog).toBeHidden();
      });

      await test.step("未勾选的字段仍在合同编辑表单中", async () => {
        await page.locator(`#contractTable tr[data-id="${contract.id}"]`).click();
        const dialog = page
          .getByRole("dialog")
          .filter({ has: page.getByRole("heading", { name: /^编辑合同/ }) });
        await expect(dialog).toBeVisible();
        await expect(dialog.locator('[data-key="monthly_rent"]')).toBeVisible();
        await expect(dialog.locator('[data-key="house_area"]')).toBeVisible();
        await page.keyboard.press("Escape");
        await expect(dialog).toBeHidden();
      });

      await test.step("关键词搜索与状态筛选无回归", async () => {
        await page.locator("#contractSearch").fill(stamp);
        await expect(page.locator("#contractSummary")).toContainText(`搜索「${stamp}」`);
        await expect(page.locator("#contractTable tr[data-id]")).toHaveCount(2);
        await page.locator("#statusFilter").selectOption("draft");
        await expect(page.locator("#contractSummary")).toContainText("状态：草稿");
        await expect(page.locator("#contractTable tr[data-id]")).toHaveCount(0);
        await page.locator("#statusFilter").selectOption("active");
        await expect(page.locator("#contractTable tr[data-id]")).toHaveCount(2);
        await page.locator("#contractSearch").fill("");
        await page.locator("#statusFilter").selectOption("");
        await expect(page.locator("#contractSummary")).not.toContainText("搜索");
      });

      await test.step("批量勾选与批量生成入口无回归", async () => {
        const batchButton = page.locator("#batchGenerateBtn");
        await expect(batchButton).toBeDisabled();
        await page.locator(`input[data-select="${contract.id}"]`).check();
        await expect(batchButton).toBeEnabled();
        await expect(batchButton).toHaveText("批量生成 PDF（1）");
        const visibleRows = await page.locator("#contractTable tr[data-id]").count();
        await page.locator("#contractSelectAll").check();
        await expect(batchButton).toHaveText(`批量生成 PDF（${visibleRows}）`);
        await page.locator("#contractSelectAll").uncheck();
        await expect(batchButton).toBeDisabled();
      });

      await test.step("行删除仍可从台账完成", async () => {
        page.once("dialog", (dialog) => dialog.accept());
        const row = page.locator(`#contractTable tr[data-id="${expiring.id}"]`);
        await row.locator('button[data-action="delete"]').click();
        await expect(row).toHaveCount(0);
        await expect(page.locator("#toast")).toContainText("合同已删除");
        const index = createdContracts.indexOf(expiring.id);
        if (index >= 0) {
          createdContracts.splice(index, 1);
        }
      });

      expect(consoleErrors, consoleErrors.join("\n")).toEqual([]);
    } finally {
      // 同一 worker 内的后续用例共享服务与数据：清掉本次新建的合同，避免
      // 影响合同生命周期场景的「共 N 份 / 即将到期 N 份」精确计数。
      for (const id of createdContracts) {
        await page.request.delete(`${app.baseURL}/app/api/v1/contracts/${id}`, {
          headers: API_HEADERS,
        });
      }
    }
  });
});

// 字段键自动生成（te-xz9zr）：新建字段只填「字段名称」即可保存。
//
// 业务含义：普通用户不需要理解机器标识。断言：
//   1. 名称下方有只读字段键预览；中文名称 → field_ 递增序号，直接保存成功；
//   2. 英文名称 → snake_case；同实体派生键冲突 → 自动加 _2；数字开头补
//      field_ 前缀，超长名称截断到 63 字符；
//   3. 「高级选项」可手改字段键，非法/重复键在前端就被拦住并提示，
//      手改后可「恢复自动生成」；手改后预览文案不再说「将自动生成」；
//   4. 编辑字段时字段键仍禁用；内置字段既有 key 不变；预览文案为「字段键：」；
//   5. 已归档字段键与内置键参与冲突避让；
//   6. 字段列表「键」列默认隐藏，工具栏「显示字段键」开关打开后才显示，
//      每次进入字段页回到默认隐藏（不持久化）。
import type { Locator } from "@playwright/test";
import { expect, test } from "./support/app-fixture";
import { trackConsoleErrors } from "./support/ui";

test.describe("字段键自动生成", () => {
  test("只填名称即可新建字段，键自动避让冲突，高级选项可手改", async ({
    app,
    page,
  }) => {
    const consoleErrors = trackConsoleErrors(page);
    const stamp = Date.now().toString(36);
    const cnLabel1 = `平方数${stamp}`;
    const cnLabel2 = `备注说明${stamp}`;
    const enLabel1 = `Area sqm ${stamp}`;
    const enLabel2 = `Area SQM ${stamp}`;
    const manualLabel = `模板字段${stamp}`;

    await page.goto(`${app.baseURL}/app/`, { waitUntil: "domcontentloaded" });
    await expect(page.locator("#tabs")).toBeVisible();
    await page.locator('#tabs button[data-tab="fields"]').click();
    await expect(page.locator("#view-fields")).toHaveClass(/active/);

    const showKeys = page
      .locator("#view-fields")
      .getByLabel("显示字段键", { exact: true });

    await test.step("键列默认隐藏，打开「显示字段键」后显示在名称列之后", async () => {
      const headers = page.locator("#view-fields thead th");
      // 默认隐藏：表头 9 列，没有「键」列，数据行同样 9 个单元格。
      await expect(showKeys).not.toBeChecked();
      await expect(headers).toHaveCount(9);
      await expect(headers.filter({ hasText: /^键$/ })).toHaveCount(0);
      await expect(
        page.locator("#view-fields tbody tr[data-id]").first().locator("td"),
      ).toHaveCount(9);

      // 打开开关：键列出现在名称之后（第 2 列），列序与接口数据不变。
      await showKeys.check();
      await expect(headers).toHaveCount(10);
      await expect(headers.nth(1)).toHaveText("键");
      await expect(
        page.locator("#view-fields tbody tr[data-id]").first().locator("td"),
      ).toHaveCount(10);

      // 关闭后恢复隐藏，开关即时生效。
      await showKeys.uncheck();
      await expect(headers).toHaveCount(9);
      await showKeys.check();
      await expect(headers).toHaveCount(10);

      // 切走再回到字段页：回到默认隐藏（每次进入页面默认关闭，不持久化）。
      await page.locator('#tabs button[data-tab="contracts"]').click();
      await page.locator('#tabs button[data-tab="fields"]').click();
      await expect(showKeys).not.toBeChecked();
      await expect(headers).toHaveCount(9);

      // 后续步骤要读取键列，重新打开并保持。
      await showKeys.check();
      await expect(headers).toHaveCount(10);
    });

    const newButton = page
      .locator("#view-fields")
      .getByRole("button", { name: "新建字段", exact: true });

    async function openCreate(): Promise<Locator> {
      await newButton.click();
      const dialog = page
        .getByRole("dialog")
        .filter({
          has: page.getByRole("heading", { name: "新建字段", exact: true }),
        });
      await expect(dialog).toBeVisible();
      return dialog;
    }

    async function save(dialog: Locator): Promise<void> {
      await dialog
        .getByRole("button", { name: "保存", exact: true })
        .click();
    }

    async function rowKey(label: string): Promise<string> {
      // 「键」列默认隐藏；本用例在第一步已打开「显示字段键」，键固定在第 2 列。
      // 名称列精确匹配：:has-text 不区分大小写，会误中同名不同大小写的行。
      const row = page.locator(
        `#view-fields tr[data-id]:has(td:first-child:text-is("${label}"))`,
      );
      await expect(row).toBeVisible();
      return (await row.locator("td").nth(1).innerText()).trim();
    }

    await test.step("中文名称：预览 field_ 序号键，直接保存成功", async () => {
      const dialog = await openCreate();
      await dialog.locator("#fieldLabel").fill(cnLabel1);
      const preview = dialog.locator(".form-grid code");
      await expect(preview).toHaveText(/^field_[0-9]+$/);
      // 自动生成时预览文案说明来源。
      await expect(dialog.getByText("字段键将自动生成：")).toBeVisible();
      const firstKey = (await preview.innerText()).trim();
      await save(dialog);
      await expect(dialog).toBeHidden();

      expect(await rowKey(cnLabel1)).toBe(firstKey);
      const firstSeq = Number(firstKey.replace("field_", ""));

      const second = await openCreate();
      await second.locator("#fieldLabel").fill(cnLabel2);
      await expect(second.locator(".form-grid code")).toHaveText(
        `field_${firstSeq + 1}`,
      );
      await save(second);
      await expect(second).toBeHidden();
      expect(await rowKey(cnLabel2)).toBe(`field_${firstSeq + 1}`);
    });

    await test.step("英文名称：snake_case，冲突自动加 _2", async () => {
      const first = await openCreate();
      await first.locator("#fieldLabel").fill(enLabel1);
      await expect(first.locator(".form-grid code")).toHaveText(
        `area_sqm_${stamp}`,
      );
      await save(first);
      await expect(first).toBeHidden();
      expect(await rowKey(enLabel1)).toBe(`area_sqm_${stamp}`);

      const conflict = await openCreate();
      await conflict.locator("#fieldLabel").fill(enLabel2);
      await expect(conflict.locator(".form-grid code")).toHaveText(
        `area_sqm_${stamp}_2`,
      );
      await save(conflict);
      await expect(conflict).toBeHidden();
      expect(await rowKey(enLabel2)).toBe(`area_sqm_${stamp}_2`);
    });

    await test.step("数字开头补 field_ 前缀；超长名称截断到 63 字符", async () => {
      const numericLabel = `2026 contract ${stamp}`;
      const numeric = await openCreate();
      await numeric.locator("#fieldLabel").fill(numericLabel);
      await expect(numeric.locator(".form-grid code")).toHaveText(
        `field_2026_contract_${stamp}`,
      );
      await save(numeric);
      await expect(numeric).toBeHidden();
      expect(await rowKey(numericLabel)).toBe(`field_2026_contract_${stamp}`);

      // 80 个 ASCII 字母 → 截断到 63 字符且仍满足后端格式（不保存）。
      const long = await openCreate();
      await long.locator("#fieldLabel").fill("a".repeat(80));
      const longPreview = long.locator(".form-grid code");
      await expect(longPreview).toHaveText(/^a{63}$/);
      expect((await longPreview.innerText()).trim()).toMatch(
        /^[a-z][a-z0-9_]{0,62}$/,
      );
      await long.getByRole("button", { name: "取消", exact: true }).click();
      await expect(long).toBeHidden();
    });

    await test.step("高级选项手改：非法/重复键被拦住，合法键按手填保存", async () => {
      const dialog = await openCreate();
      await dialog.locator("#fieldLabel").fill(manualLabel);
      await dialog.locator("details.advanced-options summary").click();
      const keyInput = dialog.locator("#fieldKey");
      await expect(keyInput).toBeVisible();

      await keyInput.fill("9bad");
      await expect(
        dialog.getByText(
          "字段键只能使用小写字母、数字和下划线，且以字母开头（如 area_sqm）",
        ).first(),
      ).toBeVisible();
      await save(dialog);
      await expect(dialog).toBeVisible();

      await keyInput.fill(`area_sqm_${stamp}`);
      await expect(
        dialog.getByText(`字段键 area_sqm_${stamp} 已存在`).first(),
      ).toBeVisible();
      await save(dialog);
      await expect(dialog).toBeVisible();

      const manualKey = `manual_${stamp}`;
      await keyInput.fill(manualKey);
      await expect(dialog.getByText(`字段键 ${manualKey} 已存在`)).toHaveCount(0);
      // 手改后预览显示手填值，文案不再说「将自动生成」。
      await expect(dialog.locator(".form-grid code")).toHaveText(manualKey);
      await expect(dialog.getByText("字段键将自动生成：")).toHaveCount(0);
      await save(dialog);
      await expect(dialog).toBeHidden();
      expect(await rowKey(manualLabel)).toBe(manualKey);
    });

    await test.step("恢复自动生成：手改后可回到自动派生键", async () => {
      const recoverLabel = `恢复自动${stamp}`;
      const dialog = await openCreate();
      await dialog.locator("#fieldLabel").fill(recoverLabel);
      await dialog.locator("details.advanced-options summary").click();
      const keyInput = dialog.locator("#fieldKey");
      await expect(keyInput).toBeVisible();

      const resume = dialog.getByRole("button", {
        name: "恢复自动生成",
        exact: true,
      });
      await expect(resume).toHaveCount(0);
      await keyInput.fill(`recover_${stamp}`);
      await expect(resume).toBeVisible();
      await resume.click();
      await expect(resume).toHaveCount(0);
      await expect(keyInput).toHaveValue(/^field_[0-9]+$/);
      // 恢复自动生成后回到自动文案。
      await expect(dialog.getByText("字段键将自动生成：")).toBeVisible();
      const autoKey = await keyInput.inputValue();
      await save(dialog);
      await expect(dialog).toBeHidden();
      expect(await rowKey(recoverLabel)).toBe(autoKey);
    });

    await test.step("归档与内置字段键：新建时自动避让冲突", async () => {
      const archivedLabel = `Archived key ${stamp}`;
      const created = await openCreate();
      await created.locator("#fieldLabel").fill(archivedLabel);
      await expect(created.locator(".form-grid code")).toHaveText(
        `archived_key_${stamp}`,
      );
      await save(created);
      await expect(created).toBeHidden();

      const archivedRow = page.locator(
        `#view-fields tr[data-id]:has(td:first-child:text-is("${archivedLabel}"))`,
      );
      await expect(archivedRow).toBeVisible();
      page.once("dialog", (dialog) => void dialog.accept());
      await archivedRow
        .getByRole("button", { name: "归档", exact: true })
        .click();
      await expect(archivedRow).toHaveCount(0);

      // 已归档字段的键仍在 existingKeys 中：同名新建自动加 _2。
      const again = await openCreate();
      await again.locator("#fieldLabel").fill(archivedLabel);
      await expect(again.locator(".form-grid code")).toHaveText(
        `archived_key_${stamp}_2`,
      );
      await save(again);
      await expect(again).toBeHidden();
      expect(await rowKey(archivedLabel)).toBe(`archived_key_${stamp}_2`);

      // 内置键 contract_no 也参与避让：同基键自动加 _2（不保存）。
      const builtinClash = await openCreate();
      await builtinClash.locator("#fieldLabel").fill("Contract No");
      await expect(builtinClash.locator(".form-grid code")).toHaveText(
        "contract_no_2",
      );
      await builtinClash
        .getByRole("button", { name: "取消", exact: true })
        .click();
      await expect(builtinClash).toBeHidden();
    });

    await test.step("编辑字段：键仍禁用；内置字段 key 不变", async () => {
      await page
        .locator(
          `#view-fields tr[data-id]:has(td:first-child:text-is("${cnLabel1}"))`,
        )
        .click();
      const editDialog = page
        .getByRole("dialog")
        .filter({
          has: page.getByRole("heading", { name: /编辑字段/ }),
        });
      await expect(editDialog).toBeVisible();
      // 编辑态预览文案固定为「字段键：<key>」，不说「将自动生成」。
      await expect(editDialog.getByText("字段键将自动生成：")).toHaveCount(0);
      await expect(editDialog.locator(".form-grid code")).toHaveText(
        /^field_[0-9]+$/,
      );
      await editDialog.locator("details.advanced-options summary").click();
      const editKey = editDialog.locator("#fieldKey");
      await expect(editKey).toBeDisabled();
      await expect(editKey).toHaveValue(/^field_[0-9]+$/);
      await editDialog
        .getByRole("button", { name: "取消", exact: true })
        .click();
      await expect(editDialog).toBeHidden();

      // 内置字段名称列带「内置」徽标，用包含匹配定位。
      const builtIn = page.locator(
        '#view-fields tr[data-id]:has(td:first-child:has-text("合同编号"))',
      );
      await expect(builtIn).toBeVisible();
      expect((await builtIn.locator("td").nth(1).innerText()).trim()).toBe(
        "contract_no",
      );
    });

    expect(
      consoleErrors,
      `浏览器控制台出现错误：\n${consoleErrors.join("\n")}`,
    ).toEqual([]);
  });
});

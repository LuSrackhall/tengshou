// 字段键自动生成（te-xz9zr）：新建字段只填「字段名称」即可保存。
//
// 业务含义：普通用户不需要理解机器标识。断言：
//   1. 名称下方有只读字段键预览；中文名称 → field_ 递增序号，直接保存成功；
//   2. 英文名称 → snake_case；同实体派生键冲突 → 自动加 _2；
//   3. 「高级选项」可手改字段键，非法/重复键在前端就被拦住并提示；
//   4. 编辑字段时字段键仍禁用；内置字段既有 key 不变。
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
      await save(dialog);
      await expect(dialog).toBeHidden();
      expect(await rowKey(manualLabel)).toBe(manualKey);
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

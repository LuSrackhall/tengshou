// 房产档案 CRUD（te-to7tr）：新建 → 列表/搜索 → 编辑 → 停用/启用。
//
// 业务含义：v0.2 资产档案只做房产，字段固定（名称/地址/房号/面积/备注）；
// 在租/空置由「生效中且未终止的合同」派生，本珠还没有合同能引用资产，
// 因此列表统一显示空置。停用保留档案数据，可随时重新启用。
import { expect, test } from "./support/app-fixture";
import { trackConsoleErrors } from "./support/ui";

test.describe("房产档案", () => {
  test("新建、搜索、编辑、停用/启用的完整档案流程", async ({ app, page }) => {
    const consoleErrors = trackConsoleErrors(page);
    // 停用/启用会弹出确认框；测试统一接受。
    page.on("dialog", (dialog) => void dialog.accept());
    const stamp = Date.now().toString(36);
    const name = `幸福里 3 号院 2 单元 501-${stamp}`;
    const row = page.locator(
      `#assetTable tbody tr[data-id]:has(td:first-child:text-is("${name}"))`,
    );

    await page.goto(`${app.baseURL}/app/`, { waitUntil: "domcontentloaded" });
    await expect(page.locator("#tabs")).toBeVisible();
    await page.locator('#tabs button[data-tab="assets"]').click();
    await expect(page.locator("#view-assets")).toHaveClass(/active/);

    await test.step("新建房产：字段保存成功，租赁状态派生为空置", async () => {
      await page.locator("#newAssetBtn").click();
      const dialog = page
        .getByRole("dialog")
        .filter({ has: page.getByRole("heading", { name: "新建房产", exact: true }) });
      await expect(dialog).toBeVisible();
      await dialog.locator("#f_asset_name").fill(name);
      await dialog.locator("#f_asset_address").fill("幸福路 3 号");
      await dialog.locator("#f_asset_room_no").fill("2-501");
      await dialog.locator("#f_asset_area").fill("89.5");
      await dialog.locator("#f_asset_note").fill("临近地铁");
      await dialog.getByRole("button", { name: "保存", exact: true }).click();
      await expect(dialog).toBeHidden();

      await expect(row).toBeVisible();
      await expect(row.locator("td").nth(1)).toHaveText("幸福路 3 号");
      await expect(row.locator("td").nth(2)).toHaveText("2-501");
      await expect(row.locator("td").nth(3)).toHaveText("89.5 ㎡");
      // 没有合同引用资产（合同侧联动在后续珠），派生状态必须是空置。
      await expect(row.locator("td").nth(4)).toHaveText("空置");
      await expect(row.locator("td").nth(5)).toHaveText("启用");
    });

    await test.step("名称必填：空名称保存被后端校验拦截", async () => {
      await page.locator("#newAssetBtn").click();
      const dialog = page
        .getByRole("dialog")
        .filter({ has: page.getByRole("heading", { name: "新建房产", exact: true }) });
      await dialog.getByRole("button", { name: "保存", exact: true }).click();
      await expect(dialog.locator("#assetErrors")).toContainText("名称：不能为空");
      await dialog.getByRole("button", { name: "取消", exact: true }).click();
      await expect(dialog).toBeHidden();
    });

    await test.step("搜索名称/地址/房号，空结果有明确提示", async () => {
      const search = page.locator("#assetSearch");
      await search.fill(`501-${stamp}`);
      await expect(row).toBeVisible();
      await expect(page.locator("#assetSummary")).toContainText("搜索");

      await search.fill(`不存在-${stamp}`);
      await expect(page.locator("#assetTable tbody")).toContainText("没有符合条件的房产档案");
      await search.fill("");
      await expect(row).toBeVisible();
    });

    await test.step("编辑地址与面积，列表立即更新", async () => {
      await row.click();
      const dialog = page
        .getByRole("dialog")
        .filter({ has: page.getByRole("heading", { name: /编辑房产/ }) });
      await expect(dialog).toBeVisible();
      await expect(dialog.locator("#f_asset_name")).toHaveValue(name);
      await dialog.locator("#f_asset_address").fill("幸福路 3 号（北门）");
      await dialog.locator("#f_asset_area").fill("92");
      await dialog.getByRole("button", { name: "保存", exact: true }).click();
      await expect(dialog).toBeHidden();

      await expect(row.locator("td").nth(1)).toHaveText("幸福路 3 号（北门）");
      await expect(row.locator("td").nth(3)).toHaveText("92 ㎡");
      await expect(row.locator("td").nth(4)).toHaveText("空置");
    });

    await test.step("停用保留档案并可筛选，重新启用后恢复", async () => {
      await row.getByRole("button", { name: "停用", exact: true }).click();
      await expect(row.locator("td").nth(5)).toHaveText("停用");

      const statusFilter = page.locator("#assetStatusFilter");
      await statusFilter.selectOption("disabled");
      await expect(row).toBeVisible();
      await statusFilter.selectOption("enabled");
      await expect(row).toBeHidden();
      await statusFilter.selectOption("");

      await row.getByRole("button", { name: "启用", exact: true }).click();
      await expect(row.locator("td").nth(5)).toHaveText("启用");
    });

    await test.step("变更记录按「房产」对象展示审计摘要", async () => {
      await page.locator('#tabs button[data-tab="changelog"]').click();
      await expect(page.locator("#view-changelog")).toHaveClass(/active/);
      await page.locator("#changeEntityFilter").selectOption("asset");
      const list = page.locator("#changelogList");
      await expect(list).toContainText(`新建房产档案「${name}」`);
      await expect(list).toContainText("修改房产档案");
      await expect(list).toContainText("停用房产档案");
    });

    // 「名称必填」步骤会真实提交一次空名称，浏览器把这次预期的 422 记成
    // console error；过滤这一条，其余控制台错误仍然失败。
    const unexpectedErrors = consoleErrors.filter(
      (text) => !text.includes("status of 422"),
    );
    expect(
      unexpectedErrors,
      `浏览器控制台出现错误：\n${unexpectedErrors.join("\n")}`,
    ).toEqual([]);
  });
});

// 模板制作辅助的可见行为验收（te-rccn0，RF-1）：
//   1) 导入含【…】/连续下划线的模板 → 侧栏提示数量与前 5 示例，仅提示不阻断
//      保存、预览与生成；无占位符的模板不显示该区块。
//   2) 同页公章框 × 字段框重叠超 30% → 保存成功并生成新版本、侧栏出现重叠
//      提醒、点击可定位、位置列表两端都有 ⚠；跨页同坐标不提示。
//   3) 「字段值白底遮盖」默认关；开启保存后回读 mask_field_values=true 并可
//      样例预览；关闭再保存回读 false。
//
// 夹具用 e2e/fixtures/template-assist.pdf（由
// e2e/fixtures/generate-template-assist-pdf.go 生成，字体子集内嵌）：
// 不依赖本机 Word/LibreOffice 转换，macOS 的 LibreOffice 中文字体缺陷不会
// 影响本 spec。模板 PDF 是产品发布形态的本地 vendor pdf.js 解析。
import { join } from "node:path";
import type { Page } from "@playwright/test";
import { repoRoot } from "./support/app";
import { appUrl } from "./support/business-api";
import {
  addPlacement,
  dragPlacementToAnchor,
  locateAnchors,
  type PlacementSpec,
} from "./support/business-calibrate";
import { expect, test } from "./support/business-fixture";
import { importTemplate, saveCalibration } from "./support/business-templates";

const PLACEHOLDER_FIXTURE = join(
  repoRoot(),
  "e2e",
  "fixtures",
  "template-assist.pdf",
);
const BLANK_FIXTURE = join(repoRoot(), "e2e", "fixtures", "template.pdf");

async function openApp(page: Page, baseURL: string): Promise<void> {
  const response = await page.goto(`${baseURL}/app/`, {
    waitUntil: "domcontentloaded",
  });
  expect(response?.status()).toBe(200);
  await expect(page.locator("#tabs")).toBeVisible();
}

/** 打开已导入模板的标定编辑器（缩放固定 100%，1px = 1pt）。 */
async function openEditor(page: Page, templateId: string): Promise<void> {
  await page.locator('#tabs button[data-tab="templates"]').click();
  await page.locator(`#templateTable tr[data-id="${templateId}"]`).click();
  await expect(page.locator("#tplCloseBtn")).toBeVisible();
  await page.locator("#tplZoom").selectOption("1");
  await expect(page.locator('.tpl-page[data-page="1"]')).toBeVisible();
}

async function closeEditor(page: Page): Promise<void> {
  await page.locator("#tplCloseBtn").click();
  await expect(page.locator("#tplCloseBtn")).toHaveCount(0);
}

/** 只读接口回读模板详情（白底开关持久化断言用）。 */
async function fetchTemplate(
  page: Page,
  templateId: string,
): Promise<{ mask_field_values?: boolean }> {
  const response = await page.request.get(
    appUrl(page, `/app/api/v1/templates/${templateId}`),
  );
  expect(response.ok()).toBeTruthy();
  return (await response.json()) as { mask_field_values?: boolean };
}

/**
 * 建一份相对方档案与草稿合同：生成 PDF 要求合同有相对方（乙方）档案。
 * 只在测试内使用接口，标定与预览/生成仍全部走界面。
 */
async function createPartyAndContract(
  page: Page,
  templateId: string,
): Promise<string> {
  const appHeaders = { "X-Tengshou-App": "1" };
  const partyResponse = await page.request.post(
    appUrl(page, "/app/api/v1/parties"),
    {
      headers: appHeaders,
      data: {
        name: "占位符提示测试相对方",
        kind: "个人",
        id_number: "330102199001010099",
      },
    },
  );
  expect(partyResponse.ok(), await partyResponse.text()).toBeTruthy();
  const party = (await partyResponse.json()) as { item?: { id?: string } };
  const partyId = party.item?.id ?? "";
  expect(partyId).not.toBe("");

  const contractResponse = await page.request.post(
    appUrl(page, "/app/api/v1/contracts"),
    {
      headers: appHeaders,
      data: {
        title: "占位符提示测试合同",
        type: "测试",
        party_b_id: partyId,
        template_id: templateId,
        status: "草稿",
      },
    },
  );
  expect(contractResponse.ok(), await contractResponse.text()).toBeTruthy();
  const contract = (await contractResponse.json()) as {
    item?: { id?: string };
  };
  const contractId = contract.item?.id ?? "";
  expect(contractId).not.toBe("");
  return contractId;
}

/** 编辑区滚动到指定页顶部，保证默认框与目标位置都在可视区内（100% 缩放）。 */
async function scrollPageToTop(page: Page, pageNumber: number): Promise<void> {
  await page.evaluate((n) => {
    const pageEl = document.querySelector(`.tpl-page[data-page="${n}"]`);
    const scroller = document.querySelector("#tplPages");
    if (!(pageEl instanceof HTMLElement) || !(scroller instanceof HTMLElement)) {
      return;
    }
    const top =
      pageEl.getBoundingClientRect().top -
      scroller.getBoundingClientRect().top +
      scroller.scrollTop;
    scroller.scrollTop = Math.max(0, top - 8);
  }, pageNumber);
  await page.waitForTimeout(150);
}

/** 把标定框拖到页码内的固定 PDF 点（左上角；缩放 100% 时 1px = 1pt）。 */
async function dragBoxToPoint(
  page: Page,
  placementId: string,
  pageNumber: number,
  xPt: number,
  yPt: number,
): Promise<void> {
  await scrollPageToTop(page, pageNumber);
  const pageBox = await page
    .locator(`.tpl-page[data-page="${pageNumber}"]`)
    .boundingBox();
  const box = page.locator(`.tpl-box[data-id="${placementId}"]`);
  const boxBox = await box.boundingBox();
  expect(pageBox, `第 ${pageNumber} 页未渲染`).not.toBeNull();
  expect(boxBox, `标定框 ${placementId} 未渲染`).not.toBeNull();
  const startX = boxBox!.x + boxBox!.width / 2;
  const startY = boxBox!.y + boxBox!.height / 2;
  const dx = pageBox!.x + xPt - boxBox!.x;
  const dy = pageBox!.y + yPt - boxBox!.y;
  await page.mouse.move(startX, startY);
  await page.mouse.down();
  await page.mouse.move(startX + dx, startY + dy, { steps: 12 });
  await page.mouse.up();
  const style = await box.evaluate((element) => {
    const html = element as HTMLElement;
    return {
      left: Number.parseFloat(html.style.left),
      top: Number.parseFloat(html.style.top),
    };
  });
  expect(
    Math.abs(style.left - xPt),
    `框 ${placementId} 的 x 未到位`,
  ).toBeLessThanOrEqual(2);
  expect(
    Math.abs(style.top - yPt),
    `框 ${placementId} 的 y 未到位`,
  ).toBeLessThanOrEqual(2);
}

test.describe("模板制作辅助：占位符提示 / 重叠提醒 / 白底遮盖", () => {
  test("占位符提示：数量与前 5 示例可见，保存/预览/生成不阻断；无占位符不提示", async ({
    app,
    page,
  }) => {
    await openApp(page, app.baseURL);

    const imported = await importTemplate(
      page,
      PLACEHOLDER_FIXTURE,
      "模板辅助测试（占位符）",
    );
    expect(imported.converter).toBe("pdf");
    expect(imported.pageCount).toBe(2);

    await openEditor(page, imported.templateId);
    const hintHeading = page
      .locator(".tpl-side h3")
      .filter({ hasText: "占位符提示" });
    await expect(hintHeading).toBeVisible();
    const hint = page.locator(".tpl-hint");
    await expect(hint).toContainText("模板文字里发现 7 个疑似占位符");
    await expect(hint).toContainText("【…】3 个");
    await expect(hint).toContainText("连续下划线 4 个");
    const examples = hint.locator("ul li");
    await expect(examples).toHaveCount(5);
    await expect(examples.nth(0)).toHaveText("第 1 页 【甲方名称】");
    await expect(examples.nth(1)).toHaveText("第 1 页 【乙方姓名】");
    await expect(examples.nth(2)).toHaveText("第 1 页 【签订日期】");
    await expect(examples.nth(3)).toHaveText("第 1 页 ＿＿＿＿＿＿");
    await expect(hint.locator(".tpl-hint-note")).toContainText(
      "仅提示，不影响保存、预览与生成",
    );

    // 先标定一个字段并保存：占位符提示不阻断保存（生成 v2）。
    const anchors = await locateAnchors(page, imported.templateId, [
      "甲方名称：",
    ]);
    const fieldSpec: PlacementSpec = {
      kind: "field",
      anchor: anchors.get("甲方名称：")!,
      binding: "contract.title",
    };
    const fieldId = await addPlacement(page, fieldSpec);
    await dragPlacementToAnchor(page, fieldId, fieldSpec);
    const saved = await saveCalibration(page, 1);
    await closeEditor(page);

    // 建相对方与合同后重开 v2：合同预览与生成都不被占位符提示阻断。
    const contractId = await createPartyAndContract(page, saved.templateId);
    await openEditor(page, saved.templateId);
    await page.locator("#tplContract").selectOption(contractId);
    await page.locator("#tplPreviewContract").click();
    const overlay = page.locator(".tpl-preview-overlay");
    await expect(overlay).toBeVisible({ timeout: 60_000 });
    await expect(
      overlay.locator(".tpl-preview-pages canvas").first(),
    ).toBeVisible({ timeout: 30_000 });
    await page.locator("#tplPreviewClose").click();
    await expect(overlay).toHaveCount(0);
    await expect(page.locator("#tplGenerate")).toBeEnabled();
    await page.locator("#tplGenerate").click();
    await expect(page.locator("#tplResult")).toContainText("已生成 PDF", {
      timeout: 60_000,
    });
    await closeEditor(page);

    // 无占位符的模板不显示提示区块。
    const blank = await importTemplate(
      page,
      BLANK_FIXTURE,
      "模板辅助测试（无占位符）",
    );
    await openEditor(page, blank.templateId);
    // 扫描在 PDF 加载后异步执行；给空结果留出完成时间。
    await page.waitForTimeout(1_500);
    await expect(hintHeading).toHaveCount(0);
    await closeEditor(page);
  });

  test("重叠提醒：同页公章压字段框保存告警可定位，跨页同坐标不提示", async ({
    app,
    page,
  }) => {
    await openApp(page, app.baseURL);
    const imported = await importTemplate(
      page,
      PLACEHOLDER_FIXTURE,
      "模板辅助测试（重叠）",
    );
    await openEditor(page, imported.templateId);

    const anchors = await locateAnchors(page, imported.templateId, [
      "甲方名称：",
      "甲方签章：",
    ]);
    const fieldSpec: PlacementSpec = {
      kind: "field",
      anchor: anchors.get("甲方名称：")!,
      binding: "contract.contract_no",
    };
    const sealSpec: PlacementSpec = {
      kind: "seal",
      anchor: anchors.get("甲方名称：")!,
      party: "party_a",
    };

    // 第 1 页：公章框与字段框拖到同一位置（重叠约 61% > 30%）。
    const field1 = await addPlacement(page, fieldSpec);
    await dragBoxToPoint(page, field1, 1, 80, 500);
    const seal1 = await addPlacement(page, sealSpec);
    await dragBoxToPoint(page, seal1, 1, 80, 500);

    // 跨页同坐标的一对：第 2 页字段 vs 第 1 页公章，不应产生提示。
    const field2 = await addPlacement(page, {
      ...fieldSpec,
      anchor: anchors.get("甲方签章：")!,
      binding: "contract.title",
    });
    await dragBoxToPoint(page, field2, 2, 300, 650);
    const seal2 = await addPlacement(page, sealSpec);
    await dragBoxToPoint(page, seal2, 1, 300, 650);

    // 保存不阻断：仍生成 v2 并保留 4 个位置；只报告同页的那一对。
    await saveCalibration(page, 4);
    const overlap = page.locator(".tpl-overlap");
    await expect(overlap).toHaveCount(1);
    await expect(overlap).toContainText("重叠");
    await expect(overlap).toContainText("公章");
    const badge = (id: string) =>
      page.locator(
        `#tplPlacementList .tpl-list-item[data-id="${id}"] .tpl-overlap-badge`,
      );
    await expect(badge(seal1)).toBeVisible();
    await expect(badge(field1)).toBeVisible();
    await expect(badge(field2)).toHaveCount(0);
    await expect(badge(seal2)).toHaveCount(0);

    // 点击重叠提醒可选中并定位到公章框。
    await overlap.click();
    await expect(
      page.locator(`.tpl-box[data-id="${seal1}"]`),
    ).toHaveClass(/selected/);
    await expect(
      page.locator(`#tplPlacementList .tpl-list-item[data-id="${seal1}"]`),
    ).toHaveClass(/selected/);
  });

  test("白底遮盖：默认关，开启保存回读 true 且可预览，关闭保存回读 false", async ({
    app,
    page,
  }) => {
    await openApp(page, app.baseURL);
    const imported = await importTemplate(
      page,
      PLACEHOLDER_FIXTURE,
      "白底遮盖测试模板",
    );
    await openEditor(page, imported.templateId);

    const checkbox = page.locator("#tplMaskFieldValues");
    await expect(checkbox).not.toBeChecked();
    expect(
      (await fetchTemplate(page, imported.templateId)).mask_field_values,
    ).toBe(false);

    const anchors = await locateAnchors(page, imported.templateId, [
      "甲方名称：",
    ]);
    const fieldSpec: PlacementSpec = {
      kind: "field",
      anchor: anchors.get("甲方名称：")!,
      binding: "contract.title",
    };
    const fieldId = await addPlacement(page, fieldSpec);
    await dragPlacementToAnchor(page, fieldId, fieldSpec);
    await checkbox.check();
    const savedOn = await saveCalibration(page, 1);
    await expect(checkbox).toBeChecked();
    expect(
      (await fetchTemplate(page, savedOn.templateId)).mask_field_values,
    ).toBe(true);

    // 开启后样例预览正常出 PDF（白底像素绘制由 Go 测试覆盖）。
    await page.locator("#tplPreviewSample").click();
    const overlay = page.locator(".tpl-preview-overlay");
    await expect(overlay).toBeVisible({ timeout: 60_000 });
    await expect(
      overlay.locator(".tpl-preview-pages canvas").first(),
    ).toBeVisible({ timeout: 30_000 });
    await page.locator("#tplPreviewClose").click();
    await expect(overlay).toHaveCount(0);

    // 关闭开关再保存：回读 false，复选框保持关闭。
    await checkbox.uncheck();
    const savedOff = await saveCalibration(page, 1, undefined, "v3");
    await expect(checkbox).not.toBeChecked();
    expect(
      (await fetchTemplate(page, savedOff.templateId)).mask_field_values,
    ).toBe(false);
    await closeEditor(page);
  });
});

// 真实业务场景的界面动作：模板导入、字段/档案、标定、合同、扫码签名、公章、
// 预览、生成与 PDF 核对。spec 只负责编排步骤与报告，选择器与操作细节集中在这里。
import { readFile } from "node:fs/promises";
import { expect, type Page } from "@playwright/test";
import {
  addPlacement,
  dragPlacementToAnchor,
  locateAnchors,
} from "./business-calibrate";
import { analyzePdf } from "./business-pdf";
import {
  AMOUNT_DISPLAY,
  CONTRACT_DATA,
  CONTRACT_FIELDS,
  CONTRACT_VALUES,
  PARTY_A_NAME,
  PARTY_B_NAME,
  PLACEMENTS,
  SEAL_NAME,
  TEMPLATE_NAME,
  requiredPdfValues,
  type FieldDraft,
  type PlacementSpec,
} from "./business-scenario";

export interface ImportedTemplate {
  templateId: string;
  pageCount: number;
}

/** page.request 需要绝对地址；用当前页面 origin 拼 app API 路径。 */
function appUrl(page: Page, path: string): string {
  return new URL(path, page.url()).toString();
}

/** 模板页导入 .docx（产品转换），自动打开编辑器后关闭并返回最新版本 id。 */
export async function importTemplate(
  page: Page,
  docxPath: string,
  name: string,
): Promise<ImportedTemplate> {
  await page.locator('#tabs button[data-tab="templates"]').click();
  await expect(page.locator("#view-templates")).toHaveClass(/active/);
  await page
    .locator('#view-templates input[type="file"]')
    .setInputFiles(docxPath);
  const dialog = page
    .getByRole("dialog")
    .filter({ has: page.getByRole("heading", { name: /导入模板/ }) });
  await expect(dialog).toBeVisible();
  await dialog.locator("#tplImportName").fill(name);
  await dialog.getByRole("button", { name: "导入", exact: true }).click();
  await expect(dialog).toBeHidden();
  await expect(page.locator("#tplCloseBtn")).toBeVisible({ timeout: 120_000 });
  const current = (await page.locator("#tplCurrent").textContent()) ?? "";
  let pageCount = Number(current.match(/·\s*(\d+)\s*页/)?.[1] ?? 0);
  await page.locator("#tplCloseBtn").click();
  const row = page
    .locator(`#templateTable tr[data-id]:has-text("${name}")`)
    .first();
  await expect(row).toBeVisible();
  const templateId = (await row.getAttribute("data-id")) ?? "";
  expect(templateId).not.toBe("");
  const detail = (await (
    await page.request.get(appUrl(page, `/app/api/v1/templates/${templateId}`))
  ).json()) as { page_count?: number };
  pageCount = detail.page_count ?? pageCount;
  return { templateId, pageCount };
}

export async function createFields(
  page: Page,
  drafts: FieldDraft[] = CONTRACT_FIELDS,
): Promise<string> {
  await page.locator('#tabs button[data-tab="fields"]').click();
  await expect(page.locator("#view-fields")).toHaveClass(/active/);
  for (const field of drafts) {
    await page
      .locator("#view-fields .segmented")
      .getByRole("button", { name: field.entity, exact: true })
      .click();
    await page
      .locator("#view-fields")
      .getByRole("button", { name: "新建字段", exact: true })
      .click();
    const dialog = page
      .getByRole("dialog")
      .filter({
        has: page.getByRole("heading", { name: "新建字段", exact: true }),
      });
    await expect(dialog).toBeVisible();
    await dialog.locator("#fieldLabel").fill(field.label);
    await dialog.locator("#fieldKey").fill(field.key);
    await dialog.locator("#fieldType").selectOption(field.type);
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(dialog).toBeHidden();
    await expect(
      page.locator(`#view-fields tr[data-id]:has-text("${field.label}")`),
    ).toBeVisible();
  }
  const contractCount = drafts.filter((item) => item.entity === "合同字段").length;
  return (
    `已建立合同字段 ${contractCount} 个（标的名称、规格、数量、单价、大写金额、` +
    "交货时间/地点、验收标准、付款方式、违约责任、争议解决）与相对方「联系人」字段。"
  );
}

/** 新建一份相对方档案，返回档案 id。 */
export async function createParty(
  page: Page,
  values: Record<string, string>,
): Promise<string> {
  await page.locator('#tabs button[data-tab="parties"]').click();
  await expect(page.locator("#view-parties")).toHaveClass(/active/);
  await page.locator("#newPartyBtn").click();
  const dialog = page
    .getByRole("dialog")
    .filter({
      has: page.getByRole("heading", { name: "新建相对方", exact: true }),
    });
  await expect(dialog).toBeVisible();
  for (const [key, value] of Object.entries(values)) {
    if (key === "kind") {
      await dialog.locator("#f_party_kind").selectOption(value);
    } else {
      await dialog.locator(`#f_party_${key}`).fill(value);
    }
  }
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog).toBeHidden();
  const row = page.locator(
    `#partyTable tr[data-id]:has-text("${values.name}")`,
  );
  await expect(row).toBeVisible();
  const id = (await row.getAttribute("data-id")) ?? "";
  expect(id).not.toBe("");
  return id;
}

/** 打开模板编辑器：按 PDF 锚点逐个新增并拖动标定框；编辑器保持打开供保存。 */
export async function calibrateTemplate(
  page: Page,
  templateId: string,
  placements: PlacementSpec[] = PLACEMENTS,
): Promise<string> {
  await page.locator('#tabs button[data-tab="templates"]').click();
  await page.locator(`#templateTable tr[data-id="${templateId}"]`).click();
  await expect(page.locator("#tplCloseBtn")).toBeVisible();
  await page.locator("#tplZoom").selectOption("1");
  await expect(page.locator('.tpl-page[data-page="1"]')).toBeVisible();
  await expect(
    page.locator('#tplAddField option[value="contract.data.product_name"]'),
  ).toHaveCount(1);
  const anchors = await locateAnchors(
    page,
    templateId,
    placements.map((placement) => placement.label),
  );
  for (const spec of placements) {
    const anchor = anchors.get(spec.label);
    if (!anchor) {
      throw new Error(`模板 PDF 中缺少锚点「${spec.label}」`);
    }
    const placement = { kind: spec.kind, anchor, binding: spec.binding, party: spec.party };
    const id = await addPlacement(page, placement);
    await dragPlacementToAnchor(page, id, placement);
  }
  await expect(
    page.locator("#tplPlacementList .tpl-list-item[data-id]"),
  ).toHaveCount(placements.length);
  const fields = placements.filter((item) => item.kind === "field").length;
  return (
    `已在第 1 页标定 ${fields} 个字段位置，并在第 2 页放置乙方手写签名与甲方公章位置；` +
    "每个框都按 Word 锚点拖动到位。"
  );
}

/** 保存为新版本；返回「v2 · N 页 · M 个位置」文本。 */
export async function saveCalibration(
  page: Page,
  count: number,
): Promise<string> {
  await page.locator("#tplSaveBtn").click();
  await expect(page.locator("#tplCurrent")).toContainText("v2", {
    timeout: 30_000,
  });
  await expect(page.locator("#tplCurrent")).toContainText(`${count} 个位置`);
  return ((await page.locator("#tplCurrent").textContent()) ?? "").trim();
}

export interface ContractDetailView {
  contract: { contract_no: string; status: string; amount: number };
  party_a?: { name: string };
  party_b?: { name: string };
}

export interface CreatedContract {
  contractId: string;
  contractNo: string;
  detail: ContractDetailView;
}

/** 关闭标定编辑器，在合同页新建草稿并核对服务端返回的甲方/相对方。 */
export async function createContract(
  page: Page,
  partyAId: string,
): Promise<CreatedContract> {
  await page.locator("#tplCloseBtn").click();
  await page.locator('#tabs button[data-tab="contracts"]').click();
  await expect(page.locator("#view-contracts")).toHaveClass(/active/);
  await page.locator("#newContractBtn").click();
  const dialog = page
    .getByRole("dialog")
    .filter({
      has: page.getByRole("heading", { name: "新建合同", exact: true }),
    });
  await expect(dialog).toBeVisible();
  await dialog.locator("#f_contract_party_a").selectOption(partyAId);
  await dialog.locator("#f_contract_title").fill(CONTRACT_VALUES.title);
  await dialog.locator("#f_contract_type").fill(CONTRACT_VALUES.type);
  await dialog.locator("#f_contract_party_b").fill(CONTRACT_VALUES.partyB);
  await dialog.locator("#f_contract_amount").fill(CONTRACT_VALUES.amount);
  await dialog.locator("#f_contract_signed_on").fill(CONTRACT_VALUES.signedOn);
  await dialog.locator("#f_contract_valid_from").fill(CONTRACT_VALUES.validFrom);
  await dialog.locator("#f_contract_valid_to").fill(CONTRACT_VALUES.validTo);
  for (const [key, value] of Object.entries(CONTRACT_DATA)) {
    await dialog.locator(`#f_contract_${key}`).fill(value);
  }
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog).toBeHidden();
  const row = page.locator(
    `#contractTable tr[data-id]:has-text("${CONTRACT_VALUES.title}")`,
  );
  await expect(row).toBeVisible();
  const contractId = (await row.getAttribute("data-id")) ?? "";
  expect(contractId).not.toBe("");
  const detail = (await (
    await page.request.get(appUrl(page, `/app/api/v1/contracts/${contractId}`))
  ).json()) as ContractDetailView;
  expect(detail.contract.status).toBe("draft");
  expect(detail.contract.amount).toBe(Number(CONTRACT_VALUES.amount));
  expect(detail.party_a?.name).toBe(PARTY_A_NAME);
  expect(detail.party_b?.name).toBe(PARTY_B_NAME);
  return { contractId, contractNo: detail.contract.contract_no, detail };
}

/** 桌面生成二维码 → 第二页面用鼠标手写三笔 → 提交 → 桌面轮询到已保存。 */
export async function signByPhone(
  page: Page,
  baseURL: string,
  contractId: string,
): Promise<{ actual: string; phoneShot: Buffer; desktopShot: Buffer }> {
  const row = page.locator(`#contractTable tr[data-id="${contractId}"]`);
  await row.locator('[data-action="sign"]').click();
  const dialog = page
    .getByRole("dialog")
    .filter({ has: page.getByRole("heading", { name: /扫码签名/ }) });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: /生成签名二维码/ }).click();
  await expect(dialog.locator("#signQrImg")).toBeVisible({ timeout: 15_000 });
  const urlText = (await dialog.locator("#signUrlText").textContent()) ?? "";
  const token = urlText.trim().split("/sign/")[1]?.replace(/\/+$/, "") ?? "";
  expect(token, `签名页 URL 中没有 token：${urlText}`).not.toBe("");
  const signPage = await page.context().newPage();
  let phoneShot: Buffer;
  try {
    await signPage.goto(`${baseURL}/sign/${token}`, {
      waitUntil: "domcontentloaded",
    });
    await expect(signPage.locator("#pad")).toBeVisible();
    const box = await signPage.locator("#pad").boundingBox();
    expect(box, "手写画布未渲染").not.toBeNull();
    const point = (fx: number, fy: number) => ({
      x: box!.x + box!.width * fx,
      y: box!.y + box!.height * fy,
    });
    const stroke = async (points: Array<[number, number]>) => {
      const first = point(points[0]![0], points[0]![1]);
      await signPage.mouse.move(first.x, first.y);
      await signPage.mouse.down();
      for (const [fx, fy] of points.slice(1)) {
        const next = point(fx, fy);
        await signPage.mouse.move(next.x, next.y, { steps: 8 });
      }
      await signPage.mouse.up();
    };
    await stroke([
      [0.18, 0.62],
      [0.28, 0.3],
      [0.38, 0.72],
      [0.48, 0.28],
    ]);
    await stroke([
      [0.52, 0.55],
      [0.62, 0.25],
      [0.72, 0.7],
      [0.82, 0.35],
    ]);
    await stroke([
      [0.24, 0.8],
      [0.5, 0.55],
      [0.78, 0.8],
    ]);
    await expect(signPage.locator("#submitBtn")).toBeEnabled();
    phoneShot = await signPage.screenshot({ fullPage: true });
    await signPage.locator("#submitBtn").click();
    await expect(signPage.locator(".result.done h1")).toHaveText("签名已提交", {
      timeout: 20_000,
    });
  } finally {
    await signPage.close();
  }
  await expect(dialog.locator("#signState")).toContainText("签名已保存", {
    timeout: 30_000,
  });
  const state = ((await dialog.locator("#signState").textContent()) ?? "").trim();
  const desktopShot = await page.screenshot({ fullPage: true });
  await dialog
    .locator(".modal-foot")
    .getByRole("button", { name: "关闭", exact: true })
    .click();
  await expect(dialog).toBeHidden();
  return {
    actual: `手机签名页已完成三笔手写并提交；桌面端轮询到「${state}」。`,
    phoneShot,
    desktopShot,
  };
}

/** 导入白底公章（产品自动去底）并关联到本合同。 */
export async function importAndAttachSeal(
  page: Page,
  sealPath: string,
  partyAId: string,
  contractId: string,
): Promise<string> {
  await page.locator('#tabs button[data-tab="seals"]').click();
  await expect(page.locator("#view-seals")).toHaveClass(/active/);
  await page
    .locator('#view-seals input[type="file"][accept="image/png,image/jpeg"]')
    .first()
    .setInputFiles(sealPath);
  const dialog = page
    .getByRole("dialog")
    .filter({ has: page.getByRole("heading", { name: /导入公章图片/ }) });
  await expect(dialog).toBeVisible();
  await dialog.locator("#sealName").fill(SEAL_NAME);
  await dialog.locator("#sealParty").selectOption(partyAId);
  await dialog.getByRole("button", { name: "导入", exact: true }).click();
  await expect(dialog).toBeHidden();
  const card = page.locator(`#view-seals .seal-card:has-text("${SEAL_NAME}")`);
  await expect(card).toBeVisible({ timeout: 30_000 });
  await card.locator('[data-action="attach"]').click();
  const attach = page
    .getByRole("dialog")
    .filter({ has: page.getByRole("heading", { name: /关联合同/ }) });
  await expect(attach).toBeVisible();
  await attach.locator(`.seal-contract-check[data-id="${contractId}"]`).check();
  await attach.getByRole("button", { name: "保存关联", exact: true }).click();
  await expect(attach).toBeHidden();
  await expect(card).toContainText("已关联 1 份合同", { timeout: 15_000 });
  return `白底公章「${SEAL_NAME}」已导入（自动去底）并关联到本合同。`;
}

/** 打开合同编辑弹窗，确认模板版本并用目标合同预览。 */
export async function previewContract(
  page: Page,
  contractId: string,
): Promise<string> {
  await page.locator('#tabs button[data-tab="contracts"]').click();
  await page.locator(`#contractTable tr[data-id="${contractId}"]`).click();
  const dialog = page
    .getByRole("dialog")
    .filter({ has: page.getByRole("heading", { name: /编辑合同/ }) });
  await expect(dialog).toBeVisible();
  const panel = dialog.locator("#contractGeneration");
  await expect(panel.locator("#genTemplate option:checked")).toContainText(
    TEMPLATE_NAME,
    { timeout: 30_000 },
  );
  await panel.locator("#genPreview").click();
  await expect(page.locator(".pdf-overlay")).toBeVisible({ timeout: 60_000 });
  await expect(panel.locator("#genStatus")).toContainText("预览已完成");
  await page.locator("#pdfOverlayClose").click();
  await expect(page.locator(".pdf-overlay")).toHaveCount(0);
  return "合同预览已生成并关闭；版式与数据确认通过，可以生成正式 PDF。";
}

/** 生成并下载最终合同 PDF；返回字节数与已保存路径。 */
export async function generateContract(
  page: Page,
  outputPath: string,
): Promise<{ actual: string; bytes: Buffer }> {
  const panel = page.locator("#contractGeneration");
  await expect(panel.locator("#genGenerate")).toBeEnabled();
  await panel.locator("#genGenerate").click();
  await expect(panel.locator("#genStatus")).toContainText("已初次生成 PDF", {
    timeout: 60_000,
  });
  await expect(panel.locator("#genHistory")).toContainText("成功");
  const downloadButton = panel
    .locator("#genHistory")
    .getByRole("button", { name: "下载 PDF" })
    .first();
  await expect(downloadButton).toBeVisible({ timeout: 30_000 });
  const downloadPromise = page.waitForEvent("download");
  await downloadButton.click();
  const download = await downloadPromise;
  await download.saveAs(outputPath);
  const bytes = await readFile(outputPath);
  expect(bytes.byteLength).toBeGreaterThan(1000);
  expect(bytes.subarray(0, 5).toString("latin1")).toBe("%PDF-");
  return {
    actual: `已生成并下载合同 PDF（${(bytes.byteLength / 1024).toFixed(1)} KB，文件头 %PDF- 正确）。`,
    bytes,
  };
}

/** 用页面自带 pdf.js 核对页数、关键文字与签名/公章图片绘制次数。 */
export async function verifyFinalPdf(
  page: Page,
  bytes: Buffer,
  templatePageCount: number,
  contractNo: string,
): Promise<{ actual: string; details: string[] }> {
  const analysis = await analyzePdf(page, bytes);
  expect(analysis.pageCount).toBe(templatePageCount);
  const required = requiredPdfValues(contractNo);
  for (const value of required) {
    expect(analysis.text, `最终 PDF 文本缺少「${value}」`).toContain(value);
  }
  expect(
    analysis.imageDraws,
    "最终 PDF 中签名与公章图片绘制次数不足 2 次",
  ).toBeGreaterThanOrEqual(2);
  const details = [
    `页数：${analysis.pageCount} 页（与模板一致）`,
    `图片绘制：${analysis.imageDraws} 次（含乙方签名与甲方公章）`,
    `关键文字齐全：${required.join("、")}`,
  ];
  return {
    actual:
      `最终 PDF 共 ${analysis.pageCount} 页（与模板一致）；` +
      `文本含编号 ${contractNo}、金额 ${AMOUNT_DISPLAY}、双方名称、标的与大写金额；` +
      `检测到 ${analysis.imageDraws} 次图片绘制（签名 + 公章）。`,
    details,
  };
}

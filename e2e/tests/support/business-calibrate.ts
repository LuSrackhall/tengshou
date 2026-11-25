// 标定助手：从产品转换后的模板 PDF 里按「字段名：＿＿＿＿」定位锚点，再通过
// 界面新增标定框并拖动到锚点位置。坐标全部是 PDF 点、左上原点；编辑器缩放
// 固定为 100%，因此 1px = 1pt，拖动距离可直接由锚点坐标换算。
import { expect, type Page } from "@playwright/test";
import {
  analyzePdf,
  lineEntryAt,
  lineXAt,
  pdfLines,
  type LabelAnchor,
  type PdfLine,
} from "./business-pdf";

export type PlacementKind = "field" | "signature" | "seal";

export interface PlacementSpec {
  kind: PlacementKind;
  anchor: LabelAnchor;
  /** 字段绑定键，例如 contract.contract_no / party_a.data.contact。 */
  binding?: string;
  /** 签名 / 公章所属方。 */
  party?: "party_a" | "party_b";
  /** 公章来源：contract=合同关联或单位公章（默认）。 */
  sealMode?: "contract" | "seal";
}

/** 页面右侧「新增位置」表单；kind=field 时 #tplAddField 以绑定键为值。 */
export async function addPlacement(
  page: Page,
  spec: PlacementSpec,
): Promise<string> {
  await page.locator("#tplAddPage").selectOption(String(spec.anchor.page));
  await page.locator("#tplAddKind").selectOption(spec.kind);
  if (spec.kind === "field") {
    const binding = spec.binding ?? "";
    await expect(
      page.locator(`#tplAddField option[value="${binding}"]`),
    ).toHaveCount(1);
    await page.locator("#tplAddField").selectOption(binding);
  } else if (spec.kind === "signature") {
    await page.locator("#tplAddParty").selectOption(spec.party ?? "party_b");
  } else {
    await page.locator("#tplAddParty").selectOption(spec.party ?? "party_a");
    await page.locator("#tplAddSealMode").selectOption(spec.sealMode ?? "contract");
  }
  const before = await page
    .locator("#tplPlacementList .tpl-list-item[data-id]")
    .count();
  await page.locator("#tplAddPlace").click();
  await expect(
    page.locator("#tplPlacementList .tpl-list-item[data-id]"),
  ).toHaveCount(before + 1);
  const last = page.locator("#tplPlacementList .tpl-list-item[data-id]").last();
  const id = (await last.getAttribute("data-id")) ?? "";
  expect(id).not.toBe("");
  await last.click();
  await expect(page.locator(`.tpl-box[data-id="${id}"]`)).toBeVisible();
  return id;
}

/** 把标定框拖动到锚点：字段框覆盖填空位置，签名/公章框放在标签右侧留白处。 */
export async function dragPlacementToAnchor(
  page: Page,
  placementId: string,
  spec: PlacementSpec,
): Promise<void> {
  const target = targetFor(spec);
  await scrollTemplatePageToTop(page, spec.anchor.page);
  const pageBox = await page
    .locator(`.tpl-page[data-page="${spec.anchor.page}"]`)
    .boundingBox();
  const box = page.locator(`.tpl-box[data-id="${placementId}"]`);
  const boxBox = await box.boundingBox();
  expect(pageBox, `第 ${spec.anchor.page} 页未渲染`).not.toBeNull();
  expect(boxBox, `标定框 ${placementId} 未渲染`).not.toBeNull();
  const startX = boxBox!.x + boxBox!.width / 2;
  const startY = boxBox!.y + boxBox!.height / 2;
  const endX = pageBox!.x + target.x + boxBox!.width / 2;
  const endY = pageBox!.y + target.y + boxBox!.height / 2;
  await page.mouse.move(startX, startY);
  await page.mouse.down();
  await page.mouse.move(endX, endY, { steps: 12 });
  await page.mouse.up();
  const style = await box.evaluate((element) => {
    const html = element as HTMLElement;
    return {
      left: Number.parseFloat(html.style.left),
      top: Number.parseFloat(html.style.top),
    };
  });
  expect(Math.abs(style.left - target.x), `框 ${placementId} 的 x 未到位`).toBeLessThanOrEqual(2);
  expect(Math.abs(style.top - target.y), `框 ${placementId} 的 y 未到位`).toBeLessThanOrEqual(2);
}

/** 编辑区滚动到指定页顶部，保证默认框与目标位置都在可视区内（100% 缩放）。 */
async function scrollTemplatePageToTop(page: Page, pageNumber: number): Promise<void> {
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
  await page.waitForTimeout(120);
}

/** 目标框左上角（PDF 点），并夹在页面内避免被编辑器 clamp 造成误差。 */
function targetFor(spec: PlacementSpec): { x: number; y: number } {
  const anchor = spec.anchor;
  const box =
    spec.kind === "field"
      ? { w: 180, h: 22 }
      : spec.kind === "signature"
        ? { w: 130, h: 50 }
        : { w: 110, h: 110 };
  let x: number;
  let y: number;
  if (spec.kind === "field") {
    // 文本框第一行基线 = 框顶 + h - ascent（约 0.8 字号），让套印文字落在
    // 原 Word 文字基线上。
    x = anchor.x;
    y = anchor.baseline - (box.h - 0.8 * anchor.fontSize);
  } else {
    // 签字/盖章放在标签行的下方留白处，不遮住标题与标签文字。
    x = anchor.x + 6;
    y = anchor.baseline + 6;
  }
  return {
    x: roundPt(clamp(x, 4, Math.max(4, anchor.pageWidth - box.w - 4))),
    y: roundPt(clamp(y, 4, Math.max(4, anchor.pageHeight - box.h - 4))),
  };
}

function clamp(value: number, low: number, high: number): number {
  return Math.min(Math.max(value, low), high);
}

function roundPt(value: number): number {
  return Math.round(value * 10) / 10;
}

/**
 * 从模板 PDF 定位全部锚点：标签必须在整份文档中唯一，否则给出可排查的中文
 * 错误（列出重复位置或文档文字片段）。标签后的 x 优先取独立下划线项，否则
 * 按全角字符占比估算（夹具的「字段名：＿＿」都是全角）。
 */
export async function locateAnchors(
  page: Page,
  templateId: string,
  labels: string[],
): Promise<Map<string, LabelAnchor>> {
  const base64 = await page.evaluate(async (id) => {
    const response = await fetch(
      `/app/api/v1/templates/${encodeURIComponent(id)}/pdf`,
    );
    if (!response.ok) {
      throw new Error(`读取模板 PDF 失败：HTTP ${response.status}`);
    }
    const bytes = new Uint8Array(await response.arrayBuffer());
    let binary = "";
    const chunk = 0x8000;
    for (let offset = 0; offset < bytes.length; offset += chunk) {
      binary += String.fromCharCode(...bytes.subarray(offset, offset + chunk));
    }
    return btoa(binary);
  }, templateId);
  const analysis = await analyzePdf(page, Buffer.from(base64, "base64"));
  const lines: Array<{ page: number; line: PdfLine }> = [];
  analysis.pages.forEach((pdfPage, index) => {
    for (const line of pdfLines(pdfPage)) {
      lines.push({ page: index + 1, line });
    }
  });
  const anchors = new Map<string, LabelAnchor>();
  for (const label of labels) {
    const want = label.replace(/\s+/g, "");
    const matches = lines.filter(({ line }) => line.text.includes(want));
    if (matches.length === 0) {
      const snippet = analysis.text.replace(/\s+/g, " ").slice(0, 240);
      throw new Error(`模板 PDF 中找不到锚点「${label}」。文档文字片段：${snippet}`);
    }
    if (matches.length > 1) {
      const where = matches
        .map(
          ({ page, line }) =>
            `第 ${page} 页 x=${lineXAt(line, line.text.indexOf(want)).toFixed(0)}`,
        )
        .join("、");
      throw new Error(`锚点「${label}」不唯一：${where}`);
    }
    const match = matches[0]!;
    const line = match.line;
    const index = line.text.indexOf(want);
    const start = lineEntryAt(line, index);
    if (!start) {
      throw new Error(`模板 PDF 中无法定位锚点「${label}」的坐标`);
    }
    const startX = lineXAt(line, index);
    const endX = lineXAt(line, index + want.length);
    const blank = line.items.find(
      (entry) => entry.item.x >= startX && /^[_＿]+$/.test(entry.text),
    );
    const pdfPage = analysis.pages[match.page - 1]!;
    anchors.set(label, {
      page: match.page,
      x: blank ? blank.item.x : endX,
      baseline: start.item.y,
      fontSize: start.item.height || 10.5,
      pageWidth: pdfPage.width,
      pageHeight: pdfPage.height,
    });
  }
  return anchors;
}

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

/** 把标定框拖动到锚点：字段框覆盖填空位置，签名/公章框放在标签右侧留白处。
 *
 * 编辑器在新增/选中位置后会做平滑滚动；滚动未稳时测得的坐标可能瞬间过期，
 * 鼠标按下会落空（macOS 上表现为「框的 x 未到位」偶发）。因此每次拖动前
 * 等滚动稳定，并用 elementFromPoint 确认起点确实命中本框；未命中或拖动后
 * 坐标未到位时重新测量重试，最多 3 次。
 */
export async function dragPlacementToAnchor(
  page: Page,
  placementId: string,
  spec: PlacementSpec,
): Promise<void> {
  const target = targetFor(spec);
  const box = page.locator(`.tpl-box[data-id="${placementId}"]`);
  let last = { left: Number.NaN, top: Number.NaN };
  for (let attempt = 0; attempt < 3; attempt += 1) {
    await scrollTemplatePageToTop(page, spec.anchor.page);
    await waitForScrollSettled(page);
    const pageBox = await page
      .locator(`.tpl-page[data-page="${spec.anchor.page}"]`)
      .boundingBox();
    const boxBox = await box.boundingBox();
    expect(pageBox, `第 ${spec.anchor.page} 页未渲染`).not.toBeNull();
    expect(boxBox, `标定框 ${placementId} 未渲染`).not.toBeNull();
    const startX = boxBox!.x + boxBox!.width / 2;
    const startY = boxBox!.y + boxBox!.height / 2;
    const endX = pageBox!.x + target.x + boxBox!.width / 2;
    const endY = pageBox!.y + target.y + boxBox!.height / 2;
    const hitId = await page.evaluate(
      ({ x, y }) =>
        (document.elementFromPoint(x, y)?.closest(".tpl-box") as HTMLElement | null)
          ?.dataset.id ?? "",
      { x: startX, y: startY },
    );
    if (hitId !== placementId) {
      continue;
    }
    await page.mouse.move(startX, startY);
    await page.mouse.down();
    await page.mouse.move(endX, endY, { steps: 12 });
    await page.mouse.up();
    // 等待 Vue 把拖动结果写回样式后再读坐标。
    await box.evaluate(async () => {
      await new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      );
    });
    last = await box.evaluate((element) => {
      const html = element as HTMLElement;
      return {
        left: Number.parseFloat(html.style.left),
        top: Number.parseFloat(html.style.top),
      };
    });
    if (
      Math.abs(last.left - target.x) <= 2 &&
      Math.abs(last.top - target.y) <= 2
    ) {
      return;
    }
  }
  throw new Error(
    `框 ${placementId} 拖动未到位：目标 (${target.x}, ${target.y})，` +
      `实际 (${last.left}, ${last.top})`,
  );
}

/** 等编辑器滚动稳定（连续 3 帧 scrollTop 不变），避免测到动画中途的位置。 */
async function waitForScrollSettled(page: Page): Promise<void> {
  await page.evaluate(async () => {
    const scroller = document.querySelector("#tplPages");
    if (!(scroller instanceof HTMLElement)) {
      return;
    }
    let last = scroller.scrollTop;
    let stable = 0;
    for (let frame = 0; frame < 90 && stable < 3; frame += 1) {
      await new Promise<void>((resolve) =>
        requestAnimationFrame(() => resolve()),
      );
      if (Math.abs(scroller.scrollTop - last) < 0.5) {
        stable += 1;
        continue;
      }
      stable = 0;
      last = scroller.scrollTop;
    }
  });
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
 * 错误（列出重复位置或文档文字片段）。标签可以跨视觉行甚至跨页（按阅读
 * 顺序拼接全文检索）；标签后的 x 优先取空白下划线项，否则按字符占比估算。
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
  // 模板按页宽折行时，一个标签（如「建筑面积」）可能被拆到两行甚至跨页
  // （P2 末尾「拖欠租」→ P3 行首「金超过」）。因此把整份文档所有视觉行按
  // 阅读顺序拼成扁平文本检索，再把命中位置映射回「页/行/行内偏移」。
  const pages = analysis.pages.map((pdfPage, index) => ({
    page: index + 1,
    lines: pdfLines(pdfPage),
    width: pdfPage.width,
    height: pdfPage.height,
  }));
  interface DocLine {
    page: number;
    line: PdfLine;
    offset: number;
  }
  const docLines: DocLine[] = [];
  let flatAll = "";
  for (const pdfPage of pages) {
    for (const line of pdfPage.lines) {
      docLines.push({ page: pdfPage.page, line, offset: flatAll.length });
      flatAll += line.text;
    }
  }
  const lineOfDoc = (index: number): number => {
    let low = 0;
    let high = docLines.length - 1;
    while (low < high) {
      const mid = Math.ceil((low + high) / 2);
      if (docLines[mid]!.offset <= index) {
        low = mid;
      } else {
        high = mid - 1;
      }
    }
    return low;
  };
  const anchors = new Map<string, LabelAnchor>();
  for (const label of labels) {
    const want = label.replace(/\s+/g, "");
    interface AnchorMatch {
      startDoc: number;
      startLocal: number;
      endDoc: number;
      endLocal: number;
    }
    const matches: AnchorMatch[] = [];
    let from = 0;
    while (from <= flatAll.length - want.length) {
      const at = flatAll.indexOf(want, from);
      if (at < 0) {
        break;
      }
      const startDoc = lineOfDoc(at);
      const endDoc = lineOfDoc(at + want.length - 1);
      matches.push({
        startDoc,
        startLocal: at - docLines[startDoc]!.offset,
        endDoc,
        endLocal: at + want.length - 1 - docLines[endDoc]!.offset,
      });
      from = at + 1;
    }
    if (matches.length === 0) {
      const snippet = analysis.text.replace(/\s+/g, " ").slice(0, 240);
      throw new Error(`模板 PDF 中找不到锚点「${label}」。文档文字片段：${snippet}`);
    }
    if (matches.length > 1) {
      const where = matches
        .map((match) => {
          const end = docLines[match.endDoc]!;
          return `第 ${end.page} 页 x=${lineXAt(end.line, match.endLocal + 1).toFixed(0)}`;
        })
        .join("、");
      throw new Error(`锚点「${label}」不唯一：${where}`);
    }
    const match = matches[0]!;
    const startEntry = docLines[match.startDoc]!;
    const start = lineEntryAt(startEntry.line, match.startLocal);
    if (!start) {
      throw new Error(`模板 PDF 中无法定位锚点「${label}」的坐标`);
    }
    const endEntry = docLines[match.endDoc]!;
    const endX = lineXAt(endEntry.line, match.endLocal + 1);
    // 空白下划线优先取标签结束后的同一视觉行；标签恰好折行到行尾（甚至页尾）
    // 时，取下一行行首的下划线（如「…止。甲方应于」→ 换行后的填空）。
    let blankDoc = endEntry;
    let blank = endEntry.line.items.find(
      (entry) => entry.item.x >= endX - 1 && /^[_＿]+$/.test(entry.text),
    );
    if (!blank) {
      const next = docLines[match.endDoc + 1];
      const first = next?.line.items[0];
      if (next && first && first.item.x <= 160 && /^[_＿]+$/.test(first.text)) {
        blank = first;
        blankDoc = next;
      }
    }
    // 标定框必须落在空白所在页：标签跨页时取空白所在页。
    const anchorPage = blankDoc.page;
    const pdfPage = pages[anchorPage - 1]!;
    anchors.set(label, {
      page: anchorPage,
      x: blank ? blank.item.x : endX,
      baseline: blank ? blank.item.y : start.item.y,
      fontSize: start.item.height || 10.5,
      pageWidth: pdfPage.width,
      pageHeight: pdfPage.height,
    });
  }
  return anchors;
}

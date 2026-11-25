// 模板域动作：导入 Word 模板（产品转换）、逐框标定、保存为新版本。
// 选择器与操作细节集中在这里；分镜采集通过可选的 FrameRecorder 注入。
import { expect, type Page } from "@playwright/test";
import { appUrl } from "./business-api";
import {
  addPlacement,
  dragPlacementToAnchor,
  locateAnchors,
} from "./business-calibrate";
import type { FrameRecorder } from "./business-frames";
import { PLACEMENTS, type PlacementDraft } from "./business-scenario";

export interface ImportedTemplate {
  templateId: string;
  pageCount: number;
  /** 产品记录的转换器：word / libreoffice（模板详情的 converter 字段）。 */
  converter: string;
}

/**
 * 导入 .docx 要把 Word 转成 PDF：服务端转换上限 3 分钟（导入端点已清除
 * 服务端 WriteTimeout），Windows 还会先尝试 Word COM 再回退 LibreOffice。
 * 等待取 240s（高于 180s 转换上限 + 余量），确保服务端超时后能回显真实
 * 错误文案，而不是把「服务端已返回错误」误判为「界面卡死」。
 */
const IMPORT_WAIT_MS = 240_000;

/** 模板页导入 .docx（产品转换），自动打开编辑器后关闭并返回最新版本 id。 */
export async function importTemplate(
  page: Page,
  docxPath: string,
  name: string,
  frames?: FrameRecorder,
): Promise<ImportedTemplate> {
  await page.locator('#tabs button[data-tab="templates"]').click();
  await expect(page.locator("#view-templates")).toHaveClass(/active/);
  await frames?.capture(
    "① 打开『模板』页，点击『导入模板』，选择仓库里的 Word 文件",
    { targets: [page.locator("#importTemplateBtn")] },
  );
  await page
    .locator('#view-templates input[type="file"]')
    .setInputFiles(docxPath);
  const dialog = page
    .getByRole("dialog")
    .filter({ has: page.getByRole("heading", { name: /导入模板/ }) });
  await expect(dialog).toBeVisible();
  await dialog.locator("#tplImportName").fill(name);
  await frames?.capture(
    `② 弹窗确认：文件=${docxPath.split(/[\\/]/).pop() ?? "Word 合同"}，模板名称=${name}`,
    {
      region: dialog,
      targets: [dialog.locator("#tplImportName")],
    },
  );
  await dialog.getByRole("button", { name: "导入", exact: true }).click();
  // 等待强度不变；失败时附带界面错误文本与模板表行数，让报告能直接
  // 区分「转换仍在进行」与「服务端已返回错误」。
  try {
    await expect(dialog).toBeHidden({ timeout: IMPORT_WAIT_MS });
  } catch (error) {
    const dialogError = await page
      .locator("#templateImportErrors")
      .textContent({ timeout: 2_000 })
      .catch(() => "");
    const templateRows = await page
      .locator("#templateTable tr[data-id]")
      .count()
      .catch(() => 0);
    throw new Error(
      `导入弹窗在 ${IMPORT_WAIT_MS / 1000}s 内未关闭；` +
        `界面错误：${dialogError?.trim() || "无"}；模板表行数：${templateRows}`,
      { cause: error },
    );
  }
  await expect(page.locator("#tplCloseBtn")).toBeVisible({ timeout: 120_000 });
  const current = (await page.locator("#tplCurrent").textContent()) ?? "";
  let pageCount = Number(current.match(/·\s*(\d+)\s*页/)?.[1] ?? 0);
  await frames?.capture(
    `③ 产品自动把 Word 转成 PDF，共 ${pageCount || "?"} 页`,
    {
      region: page.locator(".tpl-toolbar"),
      targets: [page.locator("#tplCurrent")],
    },
  );
  await frames?.capture("④ 编辑器里是 Word 转换后的真实合同版面", {
    region: page.locator('.tpl-page[data-page="1"]'),
  });
  await page.locator("#tplCloseBtn").click();
  const row = page
    .locator(`#templateTable tr[data-id]:has-text("${name}")`)
    .first();
  await expect(row).toBeVisible();
  const templateId = (await row.getAttribute("data-id")) ?? "";
  expect(templateId).not.toBe("");
  const detail = (await (
    await page.request.get(appUrl(page, `/app/api/v1/templates/${templateId}`))
  ).json()) as { page_count?: number; converter?: string };
  pageCount = detail.page_count ?? pageCount;
  return { templateId, pageCount, converter: detail.converter ?? "" };
}

/** 打开模板编辑器：按 PDF 锚点逐个新增并拖动标定框；编辑器保持打开供保存。 */
export async function calibrateTemplate(
  page: Page,
  templateId: string,
  placements: PlacementDraft[] = PLACEMENTS,
  frames?: FrameRecorder,
): Promise<string> {
  await page.locator('#tabs button[data-tab="templates"]').click();
  await page.locator(`#templateTable tr[data-id="${templateId}"]`).click();
  await expect(page.locator("#tplCloseBtn")).toBeVisible();
  await page.locator("#tplZoom").selectOption("1");
  await expect(page.locator('.tpl-page[data-page="1"]')).toBeVisible();
  await expect(
    page.locator('#tplAddField option[value="contract.data.product_name"]'),
  ).toHaveCount(1);
  await frames?.capture(
    "① 在『新增位置』选择页码、类型（字段/签名/公章）与绑定值",
    {
      region: page.locator(".tpl-side"),
      targets: [
        page.locator("#tplAddPage"),
        page.locator("#tplAddKind"),
        page.locator("#tplAddField"),
      ],
    },
  );
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
    const box = page.locator(`.tpl-box[data-id="${id}"]`);
    const pageRegion = page.locator(`.tpl-page[data-page="${anchor.page}"]`);
    if (spec.label === "标的名称：") {
      await frames?.capture(
        "② 新增字段框后，用鼠标拖到 Word 的填空位置（示例：标的名称）",
        { region: pageRegion, targets: [box] },
      );
    }
    if (spec.label === "争议解决：") {
      await frames?.capture("③ 跨页字段同样标定（第 2 页：争议解决）", {
        region: pageRegion,
        targets: [box],
      });
    }
    if (spec.kind === "signature") {
      await frames?.capture("④ 新增『签名·乙方』框，拖到签字位置", {
        region: pageRegion,
        targets: [box],
      });
    }
    if (spec.kind === "seal") {
      await frames?.capture("⑤ 新增『公章·甲方』框，拖到盖章位置", {
        region: pageRegion,
        targets: [box],
      });
    }
  }
  await expect(
    page.locator("#tplPlacementList .tpl-list-item[data-id]"),
  ).toHaveCount(placements.length);
  await frames?.capture(
    `⑥ 全部 ${placements.length} 个位置到位：第 1 页 23 个字段、第 2 页 1 个字段、第 3 页签名+公章`,
    {
      region: page.locator(".tpl-side"),
      targets: [page.locator("#tplPlacementList")],
    },
  );
  const pageSummary = (kind: PlacementDraft["kind"]): string => {
    const counts = new Map<number, number>();
    for (const spec of placements) {
      if (spec.kind !== kind) continue;
      const anchor = anchors.get(spec.label);
      if (!anchor) continue;
      counts.set(anchor.page, (counts.get(anchor.page) ?? 0) + 1);
    }
    return [...counts.entries()]
      .sort(([left], [right]) => left - right)
      .map(([pageNumber, count]) => `第 ${pageNumber} 页 ${count} 个`)
      .join("、");
  };
  return (
    `已按 Word 锚点把字段（${pageSummary("field")}）、乙方手写签名（${pageSummary("signature")}）` +
    `与甲方公章（${pageSummary("seal")}）逐个拖动到位。`
  );
}

export interface SavedCalibration {
  /** 例如「v2 · 3 页 · 26 个位置」。 */
  label: string;
  /** 保存后生成的新版本模板 id（用于读取 placements 渲染 PDF 标定框）。 */
  templateId: string;
}

/** 保存为新版本；返回版本文本与新模板 id。 */
export async function saveCalibration(
  page: Page,
  count: number,
  frames?: FrameRecorder,
): Promise<SavedCalibration> {
  await frames?.capture("① 保存前：当前仍是导入生成的 v1", {
    targets: [page.locator("#tplCurrent")],
  });
  await frames?.capture("② 点击『保存为新版本』", {
    targets: [page.locator("#tplSaveBtn")],
  });
  const mappingSaved = page.waitForResponse(
    (response) =>
      response.url().includes("/mapping") &&
      response.request().method() === "PUT",
  );
  await page.locator("#tplSaveBtn").click();
  await expect(page.locator("#tplCurrent")).toContainText("v2", {
    timeout: 30_000,
  });
  await expect(page.locator("#tplCurrent")).toContainText(`${count} 个位置`);
  const response = await mappingSaved;
  const body = (await response.json()) as { item?: { id?: string } };
  const templateId = body.item?.id ?? "";
  expect(templateId, "保存标定后未返回新版本模板 id").not.toBe("");
  const label = ((await page.locator("#tplCurrent").textContent()) ?? "").trim();
  await frames?.capture(`③ 保存后生成 ${label}；v1 保留，旧合同可回滚`, {
    targets: [page.locator("#tplCurrent")],
  });
  return { label, templateId };
}


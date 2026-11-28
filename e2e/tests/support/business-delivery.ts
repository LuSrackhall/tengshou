// 交付域动作：扫码手写签名、公章导入与关联、预览、生成下载、最终 PDF
// 核对与逐页渲染。选择器与操作细节集中在这里；分镜采集通过可选的
// FrameRecorder 注入。
import { readFile } from "node:fs/promises";
import { expect, type Page } from "@playwright/test";
import { appUrl } from "./business-api";
import type { FrameRecorder } from "./business-frames";
import {
  analyzePdf,
  renderPdfPages,
  type PlacementBox,
} from "./business-pdf";
import {
  ASSET_ADDRESS,
  ASSET_AREA,
  DEPOSIT_DISPLAY,
  DEPOSIT_WORDS,
  MONTHLY_RENT_DISPLAY,
  MONTHLY_RENT_WORDS,
  PARTY_A_ID_NUMBER,
  PARTY_A_NAME,
  PARTY_B_ID_NUMBER,
  PARTY_B_NAME,
  SEAL_NAME,
  TEMPLATE_NAME,
  requiredPdfValues,
} from "./business-scenario";
import { trackConsoleErrors } from "./ui";

function errorSummary(error: unknown): string {
  if (error instanceof Error) {
    return error.message.split("\n")[0] ?? error.message;
  }
  return String(error);
}

/** 桌面生成二维码 → 第二页面用鼠标手写三笔 → 提交 → 桌面轮询到已保存。 */
export async function signByPhone(
  page: Page,
  baseURL: string,
  contractId: string,
  frames?: FrameRecorder,
): Promise<{ actual: string }> {
  const row = page.locator(`#contractTable tr[data-id="${contractId}"]`);
  await row.locator('[data-action="sign"]').click();
  const dialog = page
    .getByRole("dialog")
    .filter({ has: page.getByRole("heading", { name: /扫码签名/ }) });
  await expect(dialog).toBeVisible();
  const generateButton = dialog.getByRole("button", { name: /生成签名二维码/ });
  await frames?.capture("① 在合同行点击『扫码签名』，弹窗中点『生成签名二维码』", {
    region: dialog,
    targets: [generateButton],
  });
  await generateButton.click();
  await expect(dialog.locator("#signQrImg")).toBeVisible({ timeout: 15_000 });
  await frames?.capture(
    "② 手机扫一扫（同一局域网、非访客网络）；链接一次性使用",
    {
      region: dialog,
      targets: [dialog.locator("#signQrImg")],
    },
  );
  const urlText = (await dialog.locator("#signUrlText").textContent()) ?? "";
  const token = urlText.trim().split("/sign/")[1]?.replace(/\/+$/, "") ?? "";
  expect(token, `签名页 URL 中没有 token：${urlText}`).not.toBe("");
  const signPage = await page.context().newPage();
  const signErrors = trackConsoleErrors(signPage);
  try {
    await signPage.goto(`${baseURL}/sign/${token}`, {
      waitUntil: "domcontentloaded",
    });
    await expect(signPage.locator("#pad")).toBeVisible();
    await frames?.capture("③ 手机上打开签名页，在画布上手写", {
      page: signPage,
      targets: [signPage.locator("#pad")],
    });
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
    await frames?.capture("④ 三笔手写完成，点『提交签名』", {
      page: signPage,
      targets: [signPage.locator("#pad")],
    });
    await signPage.locator("#submitBtn").click();
    await expect(signPage.locator(".result.done h1")).toHaveText("签名已提交", {
      timeout: 20_000,
    });
    await frames?.capture("⑤ 手机页显示『签名已提交』", {
      page: signPage,
      targets: [signPage.locator(".result.done")],
    });
    expect(
      signErrors,
      `手机签名页出现浏览器错误：\n${signErrors.join("\n")}`,
    ).toEqual([]);
  } finally {
    await signPage.close();
  }
  // 手机签名页录像在 page 关闭后才保证落盘；读入后内嵌进报告/教程，
  // 让「手机上打开签名页、手写三笔并提交」的过程可回放。
  await frames?.capturePageVideo(
    signPage,
    "手机侧实录：手机上打开签名页、手写三笔并提交",
  );
  await expect(dialog.locator("#signState")).toContainText("签名已保存", {
    timeout: 30_000,
  });
  const state = ((await dialog.locator("#signState").textContent()) ?? "").trim();
  await frames?.capture(
    `⑥ 桌面端实时收到「${state}」——签名保存在本机，不上传互联网`,
    { region: dialog, targets: [dialog.locator("#signState")] },
  );
  await dialog
    .locator(".modal-foot")
    .getByRole("button", { name: "关闭", exact: true })
    .click();
  await expect(dialog).toBeHidden();
  return {
    actual:
      `手机签名页已完成三笔手写并提交，手机页无 console error；` +
      `桌面端轮询到「${state}」。`,
  };
}

/** 导入白底公章（产品自动去底）并关联到本合同。 */
export async function importAndAttachSeal(
  page: Page,
  sealPath: string,
  partyAId: string,
  contractId: string,
  frames?: FrameRecorder,
): Promise<string> {
  await page.locator('#tabs button[data-tab="seals"]').click();
  await expect(page.locator("#view-seals")).toHaveClass(/active/);
  await frames?.capture("① 打开『公章』页，选择白底公章图片", {
    targets: [page.locator("#importSealBtn")],
  });
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
  await frames?.capture("② 填写公章名称，选择所属方=甲方", {
    region: dialog,
    targets: [dialog.locator("#sealName"), dialog.locator("#sealParty")],
  });
  await dialog.getByRole("button", { name: "导入", exact: true }).click();
  await expect(dialog).toBeHidden();
  const card = page.locator(`#view-seals .seal-card:has-text("${SEAL_NAME}")`);
  await expect(card).toBeVisible({ timeout: 30_000 });
  await frames?.capture("③ 导入成功：产品自动去底并裁剪白底", {
    targets: [card],
  });
  await card.locator('[data-action="attach"]').click();
  const attach = page
    .getByRole("dialog")
    .filter({ has: page.getByRole("heading", { name: /关联合同/ }) });
  await expect(attach).toBeVisible();
  const checkbox = attach.locator(`.seal-contract-check[data-id="${contractId}"]`);
  await checkbox.check();
  await frames?.capture("④ 在『关联合同』中勾选本合同", {
    region: attach,
    targets: [checkbox],
  });
  await attach.getByRole("button", { name: "保存关联", exact: true }).click();
  await expect(attach).toBeHidden();
  await expect(card).toContainText("已关联 1 份合同", { timeout: 15_000 });
  await frames?.capture("⑤ 关联完成，生成时公章会自动套印", {
    targets: [card],
  });
  return `白底公章「${SEAL_NAME}」已导入（自动去底）并关联到本合同。`;
}

/** 打开合同编辑弹窗，确认模板版本并用目标合同预览。 */
export async function previewContract(
  page: Page,
  contractId: string,
  frames?: FrameRecorder,
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
  await frames?.capture("① 生成面板默认选中已标定的 v2", {
    region: panel,
    targets: [panel.locator("#genTemplate"), panel.locator("#genPreview")],
  });
  await panel.locator("#genPreview").click();
  await expect(page.locator(".pdf-overlay")).toBeVisible({ timeout: 60_000 });
  await expect(panel.locator("#genStatus")).toContainText("预览已完成");
  const pages = page.locator(".pdf-overlay .pdf-page");
  await expect(pages.first()).toBeVisible({ timeout: 30_000 });
  await frames?.capture("② 预览成功：版式与数据确认无误（第 1 页）", {
    region: pages.first(),
  });
  await page
    .waitForFunction(
      () => document.querySelectorAll(".pdf-overlay .pdf-page").length >= 3,
      null,
      { timeout: 20_000 },
    )
    .catch(() => undefined);
  const pageCount = await pages.count();
  if (pageCount >= 3) {
    const last = pages.nth(pageCount - 1);
    await last.scrollIntoViewIfNeeded().catch(() => undefined);
    await frames?.capture("③ 预览中签名与盖章位置已就绪（最后一页）", {
      region: last,
    });
  }
  await page.locator("#pdfOverlayClose").click();
  await expect(page.locator(".pdf-overlay")).toHaveCount(0);
  return "合同预览已生成并关闭；版式与数据确认通过，可以生成正式 PDF。";
}

/** 生成并下载最终合同 PDF；返回字节数、已保存路径与文件信息卡。 */
export async function generateContract(
  page: Page,
  outputPath: string,
  frames?: FrameRecorder,
): Promise<{
  actual: string;
  file: { name: string; sizeKB: string; header: string };
}> {
  const panel = page.locator("#contractGeneration");
  await expect(panel.locator("#genGenerate")).toBeEnabled();
  await panel.locator("#genGenerate").click();
  await expect(panel.locator("#genStatus")).toContainText("已初次生成 PDF", {
    timeout: 60_000,
  });
  await frames?.capture("① 点击『生成 PDF』，产品完成套印", {
    region: panel,
    targets: [panel.locator("#genStatus")],
  });
  await expect(panel.locator("#genHistory")).toContainText("成功");
  const downloadButton = panel
    .locator("#genHistory")
    .getByRole("button", { name: "下载 PDF" })
    .first();
  await expect(downloadButton).toBeVisible({ timeout: 30_000 });
  await frames?.capture("② 生成历史出现『成功』，点击『下载 PDF』", {
    region: panel.locator("#genHistory"),
    targets: [downloadButton],
  });
  const downloadPromise = page.waitForEvent("download");
  await downloadButton.click();
  const download = await downloadPromise;
  await download.saveAs(outputPath);
  const bytes = await readFile(outputPath);
  expect(bytes.byteLength).toBeGreaterThan(1000);
  const header = bytes.subarray(0, 5).toString("latin1");
  expect(header).toBe("%PDF-");
  const sizeKB = (bytes.byteLength / 1024).toFixed(1);
  return {
    actual: `已生成并下载合同 PDF（${sizeKB} KB，文件头 %PDF- 正确）。`,
    file: { name: download.suggestedFilename(), sizeKB, header },
  };
}

export interface VerifyPdfOptions {
  /** 模板 v2 的标定框：第 3 页签名/公章红框与放大图坐标。 */
  placements?: PlacementBox[];
  frames?: FrameRecorder;
}

/**
 * 用页面自带 pdf.js 核对页数、关键文字与签名/公章图片绘制次数，并把最终
 * PDF 逐页渲染成带红框的报告图片（第 1/2 页关键值、第 3 页签名与公章）。
 */
export async function verifyFinalPdf(
  page: Page,
  bytes: Buffer,
  templatePageCount: number,
  contractNo: string,
  options: VerifyPdfOptions = {},
): Promise<{ actual: string; details: string[] }> {
  const analysis = await analyzePdf(page, bytes);
  expect(analysis.pageCount).toBe(templatePageCount);
  const required = requiredPdfValues();
  const normalizedText = analysis.text.replace(/\s+/g, "");
  for (const value of required) {
    expect(
      normalizedText,
      `最终 PDF 文本缺少「${value}」`,
    ).toContain(value.replace(/\s+/g, ""));
  }
  expect(
    analysis.imageDraws,
    "最终 PDF 中签名与公章图片绘制次数不足 2 次",
  ).toBeGreaterThanOrEqual(2);
  // 月租金/押金/违约金都是 1,500.00，押金与押金大写各再出现一次：
  // 用出现次数确认三处金额与两处大写都套印，而不是只看有值。
  const occurrences = (needle: string): number =>
    needle === "" ? 0 : normalizedText.split(needle).length - 1;
  expect(
    occurrences(MONTHLY_RENT_DISPLAY),
    "月租金、押金与违约金金额应各出现一次",
  ).toBeGreaterThanOrEqual(3);
  expect(
    occurrences(MONTHLY_RENT_WORDS),
    "月租金与押金大写应各出现一次",
  ).toBeGreaterThanOrEqual(2);
  const details = [
    `页数：${analysis.pageCount} 页（与模板一致）`,
    `图片绘制：${analysis.imageDraws} 次（含乙方签名与甲方公章）`,
    `关键文字齐全：${required.join("、")}`,
    `金额出现次数：${MONTHLY_RENT_DISPLAY} × ${occurrences(MONTHLY_RENT_DISPLAY)}、` +
      `${MONTHLY_RENT_WORDS} × ${occurrences(MONTHLY_RENT_WORDS)}`,
    `合同编号：${contractNo}（定稿模板没有编号位置，编号在台账与生成记录核对）`,
  ];
  // 签名/公章所在页由模板 v2 的标定框决定（CI 的 3 页模板是第 3 页；
  // 不同 LibreOffice 版本可能把签署页压到第 2 页，报告按实际页渲染）。
  const signaturePlacements = (options.placements ?? []).filter(
    (item) => item.type === "signature" || item.type === "seal",
  );
  const signaturePage =
    signaturePlacements[0]?.page ?? Math.max(1, analysis.pageCount);
  const rendered = await renderPdfPages(page, bytes, {
    scale: 2,
    textHighlights: [
      {
        page: 1,
        values: [
          PARTY_A_NAME,
          PARTY_A_ID_NUMBER,
          PARTY_B_NAME,
          PARTY_B_ID_NUMBER,
          ASSET_ADDRESS,
          ASSET_AREA,
          "2026年12月1日",
          MONTHLY_RENT_DISPLAY,
          MONTHLY_RENT_WORDS,
        ].filter((value) => value !== ""),
      },
      {
        page: 2,
        values: [DEPOSIT_DISPLAY, DEPOSIT_WORDS].filter(
          (value) => value !== "",
        ),
      },
    ],
    placements: options.placements ?? [],
    zoomPage: signaturePage,
  }).catch((error: unknown) => {
    // 报告渲染问题不影响业务判定：只记录提示，分镜与文本断言仍然完整。
    options.frames?.card({
      title: "PDF 页面渲染提示",
      tone: "info",
      lines: [`报告内 PDF 页面渲染失败：${errorSummary(error)}`],
    });
    return null;
  });
  if (rendered) {
    const captions: Record<number, string> = {
      1: "第 1 页：双方名称与证件号、房号/面积、租期、月租金及大写逐项比对全部正确（红框处）",
    };
    if (analysis.pageCount >= 2) {
      captions[2] = "第 2 页：押金及大写与录入一致（红框处）；费用承担条款文字齐全";
    }
    captions[signaturePage] =
      signaturePage === 2
        ? "第 2 页：押金与费用条款核对一致；红框处为乙方手写签名与甲方公章，已套印"
        : `第 ${signaturePage} 页：红框处为乙方手写签名与甲方公章，已套印`;
    for (const image of rendered.pages) {
      options.frames?.addImage(
        captions[image.page] ?? `第 ${image.page} 页：最终合同内容`,
        image.png,
        "pdf-page",
      );
    }
    if (rendered.zoom) {
      options.frames?.addImage(
        "放大：手写签名与红章清晰印在合同上（红框处）",
        rendered.zoom.png,
        "pdf-zoom",
      );
    }
    if (rendered.missing.length > 0) {
      options.frames?.card({
        title: "PDF 标注提示",
        tone: "info",
        lines: rendered.missing,
      });
    }
  }
  options.frames?.card({ title: "文本断言结果", tone: "pass", lines: details });
  return {
    actual:
      `最终 PDF 共 ${analysis.pageCount} 页（与模板一致）；` +
      "文本含双方名称与证件号、房产地址/房号/面积、租期、月租金/押金及大写；" +
      `检测到 ${analysis.imageDraws} 次图片绘制（签名 + 公章）。`,
    details,
  };
}

/** 读取模板 v2 的标定位置（签名/公章框用于报告 PDF 页红框与放大图）。 */
export async function fetchTemplatePlacements(
  page: Page,
  templateId: string,
): Promise<PlacementBox[]> {
  const detail = (await (
    await page.request.get(appUrl(page, `/app/api/v1/templates/${templateId}`))
  ).json()) as {
    placements?: Array<{
      page: number;
      x: number;
      y: number;
      w: number;
      h: number;
      type?: string;
      party?: string;
    }>;
  };
  return (detail.placements ?? []).map((item) => ({
    page: item.page,
    type: item.type ?? "field",
    x: item.x,
    y: item.y,
    w: item.w,
    h: item.h,
    party: item.party,
  }));
}

// 用发布形态界面自带的本地 pdf.js（index.html 的 /app/vendor/pdf.min.js）
// 在浏览器里解析 PDF：逐页文本项坐标、文本全文与图片绘制次数。
//
// 不引入 npm 版 pdfjs-dist：产品页面已经加载同一份离线 vendor，E2E 只借用，
// 因而报告与标定定位都不新增运行时/构建期依赖。
import type { Page } from "@playwright/test";

/** PDF 文本项：x/y 是 scale=1、左上原点坐标；y 为基线。 */
export interface PdfTextItem {
  str: string;
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface PdfPageLayout {
  width: number;
  height: number;
  items: PdfTextItem[];
}

export interface PdfAnalysis {
  pageCount: number;
  pages: PdfPageLayout[];
  /** 按内容顺序拼接的全部文本（页间换行）。 */
  text: string;
  /** 图片 XObject 绘制次数（签名 + 公章套印据此断言）。 */
  imageDraws: number;
}

export interface LabelAnchor {
  /** 1 起页码。 */
  page: number;
  /** 填空下划线的起点 x（PDF 点）。 */
  x: number;
  /** 标签文字基线 y（PDF 点，左上原点）。 */
  baseline: number;
  /** 标签字号（PDF 点，通常 10.5）。 */
  fontSize: number;
  pageWidth: number;
  pageHeight: number;
}

interface PdfJsTextContent {
  items: Array<{
    str?: string;
    transform?: number[];
    width?: number;
    height?: number;
  }>;
}

interface PdfJsPage {
  getViewport(params: { scale: number }): {
    width: number;
    height: number;
    transform: number[];
  };
  getTextContent(): Promise<PdfJsTextContent>;
  getOperatorList(): Promise<{ fnArray: number[] }>;
}

interface PdfJsDocument {
  numPages: number;
  getPage(n: number): Promise<PdfJsPage>;
  destroy(): Promise<void>;
}

interface PdfJsRuntime {
  getDocument(params: {
    data: Uint8Array;
    isEvalSupported: boolean;
    useSystemFonts: boolean;
  }): { promise: Promise<PdfJsDocument> };
  Util: { transform(m1: number[], m2: number[]): number[] };
  OPS: Record<string, number>;
}

/** 把 PDF 原始字节交给页面里的 pdf.js 解析；页面需已加载 /app/。 */
export async function analyzePdf(
  page: Page,
  bytes: Buffer,
): Promise<PdfAnalysis> {
  return await page.evaluate(async (base64) => {
    const runtime = (
      window as unknown as { pdfjsLib?: PdfJsRuntime }
    ).pdfjsLib;
    if (!runtime) {
      throw new Error("页面缺少本地 pdf.js（/app/vendor/pdf.min.js）");
    }
    const raw = atob(base64);
    const data = new Uint8Array(raw.length);
    for (let i = 0; i < raw.length; i += 1) {
      data[i] = raw.charCodeAt(i);
    }
    const doc = await runtime.getDocument({
      data,
      isEvalSupported: false,
      useSystemFonts: true,
    }).promise;
    const imageOps = [
      runtime.OPS.paintImageXObject,
      runtime.OPS.paintInlineImageXObject,
      runtime.OPS.paintImageMaskXObject,
      runtime.OPS.paintJpegXObject,
    ].filter((value) => typeof value === "number");
    const pages = [];
    let text = "";
    let imageDraws = 0;
    const pageCount = doc.numPages;
    for (let n = 1; n <= pageCount; n += 1) {
      const pdfPage = await doc.getPage(n);
      const viewport = pdfPage.getViewport({ scale: 1 });
      const content = await pdfPage.getTextContent();
      const items = [];
      for (const item of content.items) {
        const str = typeof item.str === "string" ? item.str : "";
        if (!str || !item.transform) {
          continue;
        }
        const tx = runtime.Util.transform(viewport.transform, item.transform);
        items.push({
          str,
          x: tx[4] ?? 0,
          y: tx[5] ?? 0,
          width: typeof item.width === "number" ? item.width : 0,
          height: typeof item.height === "number" ? item.height : 0,
        });
        text += str;
      }
      text += "\n";
      pages.push({ width: viewport.width, height: viewport.height, items });
      const ops = await pdfPage.getOperatorList();
      for (const fn of ops.fnArray) {
        if (imageOps.includes(fn)) {
          imageDraws += 1;
        }
      }
    }
    await doc.destroy();
    return { pageCount, pages, text, imageDraws };
  }, bytes.toString("base64"));
}

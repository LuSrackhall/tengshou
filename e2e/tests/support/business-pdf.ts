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

/** 视觉行：同一 y（±3pt）的文本项按 x 排序，text 为去掉空白后的拼接。 */
export interface PdfLine {
  y: number;
  items: Array<{ item: PdfTextItem; text: string }>;
  text: string;
}

/**
 * 把一页 PDF 的文本项重排成视觉行。LibreOffice 在不同平台导出 PDF 时会把
 * 同一行的文字按字体/格式分块，内容流顺序与视觉顺序并不一致（Windows 实测
 * 如此）；按 y 聚类、按 x 排序后拼接才能还原用户看到的阅读顺序。文本项
 * 去掉空白，锚点匹配不受分块与空格影响。
 */
export function pdfLines(page: PdfPageLayout): PdfLine[] {
  const entries = page.items
    .map((item) => ({ item, text: item.str.replace(/\s+/g, "") }))
    .filter((entry) => entry.text !== "");
  const lines: PdfLine[] = [];
  for (const entry of [...entries].sort(
    (a, b) => a.item.y - b.item.y || a.item.x - b.item.x,
  )) {
    const line = lines.find(
      (candidate) => Math.abs(candidate.y - entry.item.y) < 3,
    );
    if (line) {
      line.items.push(entry);
    } else {
      lines.push({ y: entry.item.y, items: [entry], text: "" });
    }
  }
  for (const line of lines) {
    line.items.sort((a, b) => a.item.x - b.item.x);
    line.text = line.items.map((entry) => entry.text).join("");
  }
  return lines.sort((a, b) => a.y - b.y);
}

/** 行内第 index 个字符的 x 坐标；跨文本项时按宽度比例插值。 */
export function lineXAt(line: PdfLine, index: number): number {
  let consumed = 0;
  for (const entry of line.items) {
    if (index <= consumed + entry.text.length) {
      const ratio = (index - consumed) / entry.text.length;
      return entry.item.x + entry.item.width * ratio;
    }
    consumed += entry.text.length;
  }
  const last = line.items[line.items.length - 1];
  return last ? last.item.x + last.item.width : 0;
}

/** 行内第 index 个字符所在的文本项（用于基线 y 与字号）。 */
export function lineEntryAt(
  line: PdfLine,
  index: number,
): { item: PdfTextItem; text: string } | undefined {
  let consumed = 0;
  for (const entry of line.items) {
    if (index < consumed + entry.text.length) {
      return entry;
    }
    consumed += entry.text.length;
  }
  return line.items[line.items.length - 1];
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

/** 与 model.Placement 对齐的最小子集：签名 / 公章标定框（PDF 点、左上原点）。 */
export interface PlacementBox {
  page: number;
  type: string;
  x: number;
  y: number;
  w: number;
  h: number;
  party?: string;
}

export interface PdfTextHighlight {
  page: number;
  /** 要圈出的完整值或唯一片段；按行内连续文本项匹配。 */
  values: string[];
}

export interface RenderPdfOptions {
  /** 渲染倍率（默认 2，报告里足够清晰）。 */
  scale?: number;
  /** 每页要圈出的关键文字。 */
  textHighlights?: PdfTextHighlight[];
  /** 签名 / 公章标定框（第 3 页红框）。 */
  placements?: PlacementBox[];
  /** 需要额外生成放大图的页（取该页签名 + 公章框的并集）。 */
  zoomPage?: number;
}

export interface PdfPageImage {
  page: number;
  png: Buffer;
}

export interface RenderedPdf {
  pages: PdfPageImage[];
  /** 签名/公章区域放大图；没有可用标定框时为 null。 */
  zoom: PdfPageImage | null;
  /** 未能标注的文字值或缺失的标定框（报告里作为提示展示）。 */
  missing: string[];
}

interface PdfJsRenderPage extends PdfJsPage {
  render(params: {
    canvasContext: CanvasRenderingContext2D;
    viewport: { width: number; height: number };
  }): { promise: Promise<void> };
}

/**
 * 用页面自带 pdf.js 把最终 PDF 逐页渲染成 PNG，并在关键文字 / 签名公章
 * 标定框处画红框；可选生成签名+公章区域的高倍放大图。
 */
export async function renderPdfPages(
  page: Page,
  bytes: Buffer,
  options: RenderPdfOptions = {},
): Promise<RenderedPdf> {
  const scale = options.scale ?? 2;
  const textHighlights = options.textHighlights ?? [];
  const placements = options.placements ?? [];
  const result = await page.evaluate(
    async ({ base64, renderScale, highlights, boxes, zoomPage }) => {
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
      const doc = (await runtime.getDocument({
        data,
        isEvalSupported: false,
        useSystemFonts: true,
      }).promise) as PdfJsDocument;
      const missing: string[] = [];
      const pages: Array<{ page: number; png: string }> = [];
      const canvases = new Map<
        number,
        { canvas: HTMLCanvasElement; boxes: Array<{ x: number; y: number; w: number; h: number }> }
      >();

      interface LineItem {
        str: string;
        x: number;
        y: number;
        width: number;
        height: number;
      }

      const findValueBox = (items: LineItem[], value: string) => {
        const lines = new Map<number, LineItem[]>();
        for (const item of items) {
          const key = Math.round(item.y / 4);
          const line = lines.get(key) ?? [];
          line.push(item);
          lines.set(key, line);
        }
        // 先找与整段文本项完全一致的值（如套印的面积「80」）：证件号等
        // 长数字里也可能含「80」，子串匹配会先命中错误位置。
        for (const line of lines.values()) {
          const exact = line.find((item) => item.str.trim() === value.trim());
          if (exact) {
            return {
              x: exact.x,
              y: exact.y - exact.height,
              w: exact.width,
              h: exact.height * 1.25,
            };
          }
        }
        for (const line of lines.values()) {
          line.sort((left, right) => left.x - right.x);
          for (let start = 0; start < line.length; start += 1) {
            let text = "";
            let left = Number.POSITIVE_INFINITY;
            let right = Number.NEGATIVE_INFINITY;
            let top = Number.POSITIVE_INFINITY;
            let bottom = Number.NEGATIVE_INFINITY;
            for (let end = start; end < line.length; end += 1) {
              const item = line[end]!;
              text += item.str;
              left = Math.min(left, item.x);
              right = Math.max(right, item.x + item.width);
              top = Math.min(top, item.y - item.height);
              bottom = Math.max(bottom, item.y + item.height * 0.25);
              if (text.includes(value)) {
                return { x: left, y: top, w: right - left, h: bottom - top };
              }
              if (text.length > value.length + 24) {
                break;
              }
            }
          }
        }
        return null;
      };

      const drawBox = (
        context: CanvasRenderingContext2D,
        box: { x: number; y: number; w: number; h: number },
        factor: number,
      ) => {
        context.save();
        context.strokeStyle = "#e02424";
        context.fillStyle = "rgba(224,36,36,.10)";
        context.lineWidth = Math.max(2, factor * 1.6);
        context.fillRect(box.x * factor, box.y * factor, box.w * factor, box.h * factor);
        context.strokeRect(box.x * factor, box.y * factor, box.w * factor, box.h * factor);
        context.restore();
      };

      try {
        for (let n = 1; n <= doc.numPages; n += 1) {
          const pdfPage = (await doc.getPage(n)) as PdfJsRenderPage;
          const baseViewport = pdfPage.getViewport({ scale: 1 });
          const viewport = pdfPage.getViewport({ scale: renderScale });
          const content = await pdfPage.getTextContent();
          const items: LineItem[] = [];
          for (const item of content.items) {
            const str = typeof item.str === "string" ? item.str : "";
            if (!str || !item.transform) {
              continue;
            }
            const tx = runtime.Util.transform(baseViewport.transform, item.transform);
            items.push({
              str,
              x: tx[4] ?? 0,
              y: tx[5] ?? 0,
              width: typeof item.width === "number" ? item.width : 0,
              height: typeof item.height === "number" ? item.height : 0,
            });
          }
          const width = Math.max(1, Math.round(viewport.width));
          const height = Math.max(1, Math.round(viewport.height));
          // pdf.js 渲染会接管画布（透明底）：先渲染到临时画布，再合成到
          // 白底画布上，避免报告里出现黑底（透明区域查看器显示为黑色）。
          const renderCanvas = document.createElement("canvas");
          renderCanvas.width = width;
          renderCanvas.height = height;
          const renderContext = renderCanvas.getContext("2d");
          if (!renderContext) {
            throw new Error("无法创建 PDF 渲染画布");
          }
          await pdfPage.render({
            canvasContext: renderContext,
            viewport,
          }).promise;
          const canvas = document.createElement("canvas");
          canvas.width = width;
          canvas.height = height;
          const context = canvas.getContext("2d");
          if (!context) {
            throw new Error("无法创建 PDF 合成画布");
          }
          context.fillStyle = "#ffffff";
          context.fillRect(0, 0, canvas.width, canvas.height);
          context.drawImage(renderCanvas, 0, 0);
          const pageBoxes: Array<{ x: number; y: number; w: number; h: number }> = [];
          for (const highlight of highlights) {
            if (highlight.page !== n) {
              continue;
            }
            for (const value of highlight.values) {
              const box = findValueBox(items, value);
              if (box) {
                drawBox(context, box, renderScale);
              } else {
                missing.push(`第 ${n} 页未找到「${value}」的文本位置，未画红框`);
              }
            }
          }
          for (const box of boxes) {
            if (box.page !== n || (box.type !== "signature" && box.type !== "seal")) {
              continue;
            }
            drawBox(context, box, renderScale);
            pageBoxes.push(box);
          }
          canvases.set(n, { canvas, boxes: pageBoxes });
          pages.push({ page: n, png: canvas.toDataURL("image/png") });
        }
      } finally {
        await doc.destroy().catch(() => undefined);
      }

      let zoom: string | null = null;
      if (typeof zoomPage === "number" && zoomPage >= 1) {
        const rendered = canvases.get(zoomPage);
        const target = rendered?.boxes ?? [];
        if (!rendered || target.length === 0) {
          missing.push(`第 ${zoomPage} 页没有签名/公章标定框，无法生成放大图`);
        } else {
          let left = Number.POSITIVE_INFINITY;
          let top = Number.POSITIVE_INFINITY;
          let right = Number.NEGATIVE_INFINITY;
          let bottom = Number.NEGATIVE_INFINITY;
          for (const box of target) {
            left = Math.min(left, box.x * renderScale);
            top = Math.min(top, box.y * renderScale);
            right = Math.max(right, (box.x + box.w) * renderScale);
            bottom = Math.max(bottom, (box.y + box.h) * renderScale);
          }
          const margin = 36;
          const sx = Math.max(0, left - margin);
          const sy = Math.max(0, top - margin);
          const sw = Math.min(rendered.canvas.width - sx, right - left + margin * 2);
          const sh = Math.min(rendered.canvas.height - sy, bottom - top + margin * 2);
          const zoomCanvas = document.createElement("canvas");
          zoomCanvas.width = Math.max(1, Math.round(sw * 2));
          zoomCanvas.height = Math.max(1, Math.round(sh * 2));
          const zoomContext = zoomCanvas.getContext("2d");
          if (zoomContext) {
            zoomContext.drawImage(
              rendered.canvas,
              sx,
              sy,
              sw,
              sh,
              0,
              0,
              zoomCanvas.width,
              zoomCanvas.height,
            );
            zoom = zoomCanvas.toDataURL("image/png");
          }
        }
      }
      return { pages, zoom, missing };
    },
    {
      base64: bytes.toString("base64"),
      renderScale: scale,
      highlights: textHighlights,
      boxes: placements,
      zoomPage: options.zoomPage ?? null,
    },
  );
  const toBuffer = (dataUrl: string): Buffer => {
    const comma = dataUrl.indexOf(",");
    return Buffer.from(comma >= 0 ? dataUrl.slice(comma + 1) : "", "base64");
  };
  return {
    pages: result.pages.map((item) => ({ page: item.page, png: toBuffer(item.png) })),
    zoom: result.zoom ? { page: options.zoomPage ?? 0, png: toBuffer(result.zoom) } : null,
    missing: result.missing,
  };
}

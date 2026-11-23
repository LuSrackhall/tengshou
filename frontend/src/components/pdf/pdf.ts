// pdf.js 适配（迁移期）：类型统一来自 src/pdfjs.ts（与模板标定域共用一份
// 全局声明）。index.html 以 classic script 加载本地 vendor
// `/app/vendor/pdf.min.js`，app.js 先把 worker 指到
// `/app/vendor/pdf.worker.min.js`；Vue 侧只读取全局 `pdfjsLib`，不打包、不改
// CDN（离线单二进制约束）。
//
// 渲染循环与 app.js renderPDFPagesInto 保持一致
// （0.9 倍缩放、按 devicePixelRatio 放大画布）。
import type {
  PdfDocument,
  PdfJsLib,
  PdfPage,
  PdfRenderTask,
  PdfViewport,
} from "../../pdfjs";

export type { PdfDocument, PdfJsLib, PdfPage, PdfRenderTask, PdfViewport };

/** base64 → 字节数组（app.js base64ToBytes 的等价实现）。 */
export function base64ToBytes(value: string): Uint8Array {
  const binary = atob(value);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes;
}

/** 把容器内容替换成一条居中的加载/失败提示（tpl-loading）。 */
export function showPdfMessage(container: HTMLElement, message: string): void {
  container.replaceChildren();
  const notice = document.createElement("div");
  notice.className = "tpl-loading";
  notice.textContent = message;
  container.appendChild(notice);
}

/**
 * 把 PDF 逐页渲染为 canvas 追加进容器；返回文档句柄供调用方销毁。
 * 与 app.js showPDFOverlay / renderPDFPagesInto 的观感一致。
 */
export async function renderPdfPagesInto(
  container: HTMLElement,
  bytes: Uint8Array,
  scale: number,
): Promise<PdfDocument> {
  const lib = window.pdfjsLib;
  if (!lib) {
    throw new Error("PDF 渲染组件未加载");
  }
  container.replaceChildren();
  const doc = await lib.getDocument({
    data: bytes.slice(0),
    isEvalSupported: false,
    useSystemFonts: true,
  }).promise;
  try {
    const dpr = window.devicePixelRatio || 1;
    for (let pageNumber = 1; pageNumber <= doc.numPages; pageNumber += 1) {
      if (!container.isConnected) {
        break;
      }
      const page = await doc.getPage(pageNumber);
      const viewport = page.getViewport({ scale });
      const wrap = document.createElement("div");
      wrap.className = "pdf-page";
      wrap.style.width = `${viewport.width}px`;
      wrap.style.height = `${viewport.height}px`;
      const canvas = document.createElement("canvas");
      canvas.width = Math.floor(viewport.width * dpr);
      canvas.height = Math.floor(viewport.height * dpr);
      canvas.style.width = `${viewport.width}px`;
      canvas.style.height = `${viewport.height}px`;
      wrap.appendChild(canvas);
      container.appendChild(wrap);
      const context = canvas.getContext("2d");
      if (!context) {
        throw new Error("无法创建画布");
      }
      await page.render({
        canvasContext: context,
        viewport,
        transform: dpr !== 1 ? [dpr, 0, 0, dpr, 0, 0] : null,
      }).promise;
    }
    return doc;
  } catch (error) {
    await doc.destroy().catch(() => undefined);
    throw error;
  }
}

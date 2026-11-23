// 阶段 1 模板标定域的 pdf.js 调用面：类型统一来自 src/pdfjs.ts（与生成域
// 共用一份全局声明），这里只保留模板域用到的加载/渲染函数。
//
// index.html 以 classic script 先加载 /app/vendor/pdf.min.js（全局
// pdfjsLib）再加载 Vue 产物；worker 也来自 /app/vendor/。
// 不引入 npm 包、不访问 CDN，保持离线单二进制。禁止 any / ts-ignore。
import type {
  PdfDocument,
  PdfJsLib,
  PdfPage,
  PdfRenderTask,
  PdfViewport,
} from "../../pdfjs";

export type { PdfDocument, PdfPage, PdfRenderTask, PdfViewport };

function requirePdfLib(): PdfJsLib {
  const lib = window.pdfjsLib;
  if (!lib) {
    throw new Error("PDF 渲染组件未加载：缺少本地 pdf.js");
  }
  if (!lib.GlobalWorkerOptions.workerSrc) {
    lib.GlobalWorkerOptions.workerSrc = "/app/vendor/pdf.worker.min.js";
  }
  return lib;
}

/** 与迁移前 app.js 相同的加载参数（不 eval、使用系统字体）。 */
export function loadPdfDocument(bytes: Uint8Array): Promise<PdfDocument> {
  return requirePdfLib().getDocument({
    data: bytes.slice(0),
    isEvalSupported: false,
    useSystemFonts: true,
  }).promise;
}

/**
 * 在 canvas 上按设备像素比渲染一页；返回可取消的渲染任务。
 * 缩放切换时调用方先 cancel() 再重新渲染，避免 pdf.js 的
 * 「同一 canvas 上并发渲染」错误。
 */
export function renderPageOnCanvas(
  page: PdfPage,
  canvas: HTMLCanvasElement,
  scale: number,
): PdfRenderTask {
  const viewport = page.getViewport({ scale });
  const dpr = window.devicePixelRatio || 1;
  canvas.width = Math.floor(viewport.width * dpr);
  canvas.height = Math.floor(viewport.height * dpr);
  canvas.style.width = `${viewport.width}px`;
  canvas.style.height = `${viewport.height}px`;
  const context = canvas.getContext("2d");
  if (!context) {
    throw new Error("当前浏览器无法创建 Canvas 渲染上下文");
  }
  return page.render({
    canvasContext: context,
    viewport,
    transform: dpr !== 1 ? [dpr, 0, 0, dpr, 0, 0] : null,
  });
}

/**
 * 把整份 PDF 逐页渲染进容器（预览层用，与迁移前 renderPDFPagesInto 等价）。
 * 容器被移除（预览关闭）后停止继续渲染。
 */
export async function renderPdfPagesInto(
  container: HTMLElement,
  bytes: Uint8Array,
  scale: number,
): Promise<PdfDocument> {
  container.innerHTML = "";
  const doc = await loadPdfDocument(bytes);
  for (let index = 1; index <= doc.numPages; index += 1) {
    if (!container.isConnected) {
      break;
    }
    const page = await doc.getPage(index);
    const viewport = page.getViewport({ scale });
    const wrap = document.createElement("div");
    wrap.className = "pdf-page";
    wrap.style.width = `${viewport.width}px`;
    wrap.style.height = `${viewport.height}px`;
    const canvas = document.createElement("canvas");
    wrap.appendChild(canvas);
    container.appendChild(wrap);
    await renderPageOnCanvas(page, canvas, scale).promise;
  }
  return doc;
}

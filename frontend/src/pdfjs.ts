// 本地 vendored pdf.js 的最小类型适配（模板标定域与合同生成域共用一份）。
//
// index.html 以 classic script 先加载 /app/vendor/pdf.min.js（全局
// pdfjsLib）再加载 Vue 产物；worker 也来自 /app/vendor/。
// 这里只声明用到的调用面，不引入 npm 包、不访问 CDN，保持离线单二进制。
// 禁止 any / ts-ignore：未知能力一律按最小接口收敛。

export interface PdfViewport {
  width: number;
  height: number;
}

export interface PdfRenderTask {
  promise: Promise<void>;
  /** pdf.js 原生 render task 支持取消；模板域在缩放切换时会用到。 */
  cancel(): void;
}

export interface PdfTextItem {
  /** 文本片段；pdf.js 也可能返回不含文本的伪项。 */
  str?: string;
  /** 该片段后是否意味着换行。 */
  hasEOL?: boolean;
}

export interface PdfTextContent {
  items: PdfTextItem[];
}

export interface PdfPage {
  getViewport(params: { scale: number }): PdfViewport;
  render(params: {
    canvasContext: CanvasRenderingContext2D | null;
    viewport: PdfViewport;
    transform?: number[] | null;
  }): PdfRenderTask;
  /** 提取该页文字（模板占位符扫描使用；不触发渲染）。 */
  getTextContent(): Promise<PdfTextContent>;
}

export interface PdfDocument {
  numPages: number;
  getPage(pageNumber: number): Promise<PdfPage>;
  destroy(): Promise<void>;
}

export interface PdfJsLib {
  /** 本地 vendor 全局对象始终带 worker 配置；模板域在 requirePdfLib 中兜底设置。 */
  GlobalWorkerOptions: { workerSrc: string };
  getDocument(params: {
    data: Uint8Array;
    isEvalSupported?: boolean;
    useSystemFonts?: boolean;
  }): { promise: Promise<PdfDocument> };
}

declare global {
  interface Window {
    /** /app/vendor/pdf.min.js 暴露的全局对象；开发预览页可能缺席。 */
    pdfjsLib?: PdfJsLib;
  }
}

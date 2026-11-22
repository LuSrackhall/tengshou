// 模板 PDF 的加载与销毁：编辑器挂载后取模板 PDF、记录每页尺寸，卸载时
// 销毁 PDF.js 文档。逐条对应拆分前 useTemplateEditor 的 loadPdf 与
// onBeforeUnmount 清理（请求、错误文案与 loading 时序不变）。
import { computed, ref, shallowRef } from "vue";
import { fetchTemplatePdf } from "../../api/endpoints";
import { errorMessage } from "../../bridge/app";
import { loadPdfDocument, type PdfDocument, type PdfViewport } from "./pdfjs";

export function useTemplatePdf(templateId: () => string) {
  const pdfDoc = shallowRef<PdfDocument | null>(null);
  const pageSizes = ref<PdfViewport[]>([]);
  const loadingPages = ref(true);
  const pagesError = ref("");
  let disposed = false;

  const pages = computed(() =>
    pageSizes.value.map((size, index) => ({
      pageNumber: index + 1,
      pageSize: size,
    })),
  );

  async function load(): Promise<void> {
    try {
      const bytes = await fetchTemplatePdf(templateId());
      const doc = await loadPdfDocument(bytes);
      if (disposed) {
        void doc.destroy();
        return;
      }
      pdfDoc.value = doc;
      const sizes: PdfViewport[] = [];
      for (let index = 1; index <= doc.numPages; index += 1) {
        const page = await doc.getPage(index);
        const viewport = page.getViewport({ scale: 1 });
        sizes.push({ width: viewport.width, height: viewport.height });
      }
      pageSizes.value = sizes;
    } catch (error) {
      if (disposed) {
        return;
      }
      pagesError.value = errorMessage(error);
    } finally {
      loadingPages.value = false;
    }
  }

  /** 卸载时销毁文档；关闭编辑器时的清理失败不影响页面。 */
  function dispose(): void {
    disposed = true;
    try {
      void pdfDoc.value?.destroy();
    } catch {
      /* 关闭编辑器时的清理失败不影响页面 */
    }
    pdfDoc.value = null;
  }

  return { pdfDoc, pageSizes, pages, loadingPages, pagesError, load, dispose };
}

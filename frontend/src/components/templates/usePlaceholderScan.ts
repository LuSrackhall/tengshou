// 占位符扫描的视图状态：编辑器加载完模板 PDF 后调用 scan()，侧栏据此显示
// 数量与示例。扫描失败只记录提示，不影响标定、预览与生成。
import { ref } from "vue";
import { errorMessage } from "../../bridge/app";
import type { PdfDocument } from "./pdfjs";
import {
  scanDocumentPlaceholders,
  type PlaceholderScan,
} from "./placeholders";

export type PlaceholderScanView =
  | { kind: "idle" }
  | { kind: "loading" }
  | { kind: "ready"; scan: PlaceholderScan }
  | { kind: "error"; text: string };

export function usePlaceholderScan() {
  const view = ref<PlaceholderScanView>({ kind: "idle" });
  // 关闭编辑器后旧扫描结果不得回写；用序号丢弃过期结果。
  let run = 0;

  async function scan(doc: PdfDocument | null): Promise<void> {
    if (!doc) {
      view.value = { kind: "idle" };
      return;
    }
    const seq = ++run;
    view.value = { kind: "loading" };
    try {
      const result = await scanDocumentPlaceholders(doc);
      if (seq === run) {
        view.value = { kind: "ready", scan: result };
      }
    } catch (error) {
      if (seq === run) {
        view.value = { kind: "error", text: errorMessage(error) };
      }
    }
  }

  return { view, scan };
}

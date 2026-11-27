// 批量生成结果里单份失败的重试：预检与生成都走 job 端点（与批量主流程
// 同一套真实阶段进度），成功后把该合同的结果合并回批次结果并刷新摘要/台账。
//
// 从 BatchGenerateDialog 抽出（单文件 ≤600 行结构预算）；业务语义与迁移前
// 一致：预检不通过时提示原因、失败只替换该行结果、成功 toast。
import type { Ref } from "vue";
import { startBatchGenerateJob, startBatchPreviewJob } from "../../api/endpoints";
import type {
  BatchGenerateResult,
  BatchPreviewResponse,
  DocumentKind,
  JobStartResponse,
} from "../../api/types";
import { isJobCancelled } from "../../composables/useJob";
import { errorMessage, toast } from "../../bridge/app";

export interface BatchRetryOptions {
  templateID: Ref<string>;
  allowStale: Ref<boolean>;
  /** 整改重出时固定为 regenerate；空值由服务端判断。 */
  kind: DocumentKind | "";
  results: Ref<BatchGenerateResult | null>;
  summaryText: Ref<string>;
  refreshLists: () => Promise<void>;
  /** 复用对话框里已有的 job runner，让重试也显示阶段进度。 */
  startPreview: (
    starter: () => Promise<JobStartResponse>,
  ) => Promise<BatchPreviewResponse>;
  startGenerate: (
    starter: () => Promise<JobStartResponse>,
  ) => Promise<BatchGenerateResult>;
}

export async function retryBatchItem(
  contractID: string,
  options: BatchRetryOptions,
): Promise<void> {
  try {
    const previewRes = await options.startPreview(() =>
      startBatchPreviewJob(options.templateID.value, {
        contract_ids: [contractID],
        sample: false,
      }),
    );
    const item = (previewRes.items ?? [])[0];
    const eligible = Boolean(
      item &&
        (item.ready ||
          (options.allowStale.value &&
            (item.issues ?? []).every((issue) => issue.code === "content_changed"))),
    );
    if (!item || !eligible) {
      const reason = item
        ? (item.issues ?? []).map((issue) => issue.message).join("；")
        : "预检失败";
      toast(`仍不能生成：${reason}`, true);
      return;
    }
    const res = await options.startGenerate(() =>
      startBatchGenerateJob(options.templateID.value, {
        items: [{ contract_id: contractID, preview_sha256: item.preview_sha256 }],
        kind: options.kind || undefined,
        allow_stale_signature: options.allowStale.value,
      }),
    );
    const outcome = (res.items ?? [])[0];
    if (!outcome) {
      throw new Error("批次没有返回结果");
    }
    const current = options.results.value;
    if (current) {
      const items = (current.items ?? []).map((entry) =>
        entry.contract_id === contractID ? outcome : entry,
      );
      options.results.value = {
        ...current,
        items,
        summary: {
          total: items.length,
          success: items.filter((entry) => entry.success).length,
          failed: items.filter((entry) => !entry.success).length,
        },
      };
    }
    const summary = options.results.value?.summary;
    options.summaryText.value =
      `生成完成：成功 ${summary?.success ?? 0} 份 · 失败 ${summary?.failed ?? 0} 份`;
    toast(outcome.success ? "已重试成功" : `重试失败：${outcome.error_message || ""}`, !outcome.success);
    await options.refreshLists();
  } catch (error) {
    if (isJobCancelled(error)) {
      return;
    }
    toast(errorMessage(error), true);
  }
}

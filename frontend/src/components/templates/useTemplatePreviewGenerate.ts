// 预览、生成、下载与预览层编排：样例/合同预览、生成 PDF、查看/下载生成结果、
// 预览层下载与引导进度记录。逐条对应拆分前 useTemplateEditor 的 297-438 行
// （请求、文案、时序与错误处理不变）。
//
// 合同预览与生成 PDF 走后台任务（`/jobs/contracts/{id}/*`），阶段与已用秒数
// 由侧栏 ProgressPanel 展示；样例预览没有合同，保持同步端点（瞬时路径）。
import { computed, nextTick, ref, shallowRef, type Ref } from "vue";
import {
  fetchDocumentFile,
  previewTemplate as previewTemplateRequest,
  startContractGenerateJob,
  startContractPreviewJob,
  updateOnboardingState,
} from "../../api/endpoints";
import type {
  Contract,
  JobSnapshot,
  OverlayWarning,
  Template,
  TemplateGenerateResponse,
  TemplatePreviewResponse,
} from "../../api/types";
import { isJobCancelled, useJob } from "../../composables/useJob";
import {
  errorMessage,
  refreshAfterWrite,
  toast,
  type TemplateEditorAction,
} from "../../bridge/app";
import { base64ToBytes, downloadBytes } from "./format";
import type {
  GeneratedResult,
  PreviewOverlayState,
  PreviewState,
} from "./types";

export interface PreviewGenerateOptions {
  /** 当前模板；保存为新版本后 id 不变、version 更新。 */
  templateItem: Ref<Template>;
  /** 合同预览选择的合同。 */
  contractId: Ref<string>;
  contracts: Ref<Contract[]>;
  /** 有未保存修改时，预览前先保存为新版本。 */
  dirty: Ref<boolean>;
  saveMapping: () => Promise<Template>;
  /** 生成结果面板（#tplResult）所在的编辑器根节点。 */
  editorEl: Ref<HTMLElement | null>;
  loadUsage: () => void;
}

export function useTemplatePreviewGenerate(options: PreviewGenerateOptions) {
  // 字节数组不做深层响应式（避免 TypedArray 被代理后交给 PDF.js/Blob）。
  const preview = shallowRef<PreviewState | null>(null);
  const warnings = ref<OverlayWarning[] | null>(null);
  const result = ref<GeneratedResult | null>(null);
  const previewOverlay = shallowRef<PreviewOverlayState | null>(null);
  const previewOverlayKey = ref(0);

  // 合同预览与生成 PDF 走后台任务：阶段、已用秒数与不确定进度由侧栏面板
  // 展示；卸载时 useJob 只停轮询，不取消服务端任务。
  const {
    job: previewJob,
    running: previewRunning,
    start: startPreview,
  } = useJob<TemplatePreviewResponse>();
  const {
    job: generateJob,
    running: generating,
    start: startGenerate,
  } = useJob<TemplateGenerateResponse>();

  /** 侧栏进度面板的快照：有生成任务时优先展示生成，否则展示预览。 */
  const activeJob = computed<JobSnapshot<unknown> | null>(() => {
    if (generating.value) {
      return generateJob.value;
    }
    if (previewRunning.value) {
      return previewJob.value;
    }
    return null;
  });

  function labelError(error: unknown): void {
    toast(errorMessage(error), true);
  }

  /** 置脏或保存后让当前预览失效（状态由本组合式函数持有）。 */
  function invalidatePreview(): void {
    preview.value = null;
  }

  /** 每次打开预览层都换 key，保证 PDF 重新加载（迁移前每次重建 overlay）。 */
  function showPreviewOverlay(payload: PreviewOverlayState): void {
    previewOverlay.value = payload;
    previewOverlayKey.value += 1;
  }

  async function recordSampleStep(
    step: "sample_preview" | "trial_pdf",
  ): Promise<void> {
    try {
      await updateOnboardingState({ action: "step", step });
    } catch {
      // 引导进度记录失败不影响预览本身（与迁移前一致）。
    }
  }

  async function previewTemplate(sample: boolean): Promise<void> {
    if (previewRunning.value) {
      return;
    }
    try {
      if (options.dirty.value) {
        await options.saveMapping();
      }
      const targetId = sample ? "" : options.contractId.value;
      if (!sample && !targetId) {
        toast("请先选择用于预览的合同", true);
        return;
      }
      // 合同预览走 job（真实阶段 + 已用秒数）；样例预览没有合同，job 端点
      // 按合同路径定位，故样例仍用同步端点。
      const response = sample
        ? await previewTemplateRequest(options.templateItem.value.id, {
            sample: true,
          })
        : await startPreview(() =>
            startContractPreviewJob(targetId, {
              template_id: options.templateItem.value.id,
              contract_id: targetId,
            }),
          );
      const bytes = base64ToBytes(response.pdf_base64);
      preview.value = {
        sample,
        contractId: sample ? "" : targetId,
        sha: response.preview_sha256,
        bytes,
      };
      const contract = options.contracts.value.find(
        (item) => item.id === targetId,
      );
      const title = sample
        ? "样例数据预览（不写入台账）"
        : `合同预览 · ${contract ? `${contract.contract_no} · ${contract.title}` : ""}`;
      showPreviewOverlay({
        bytes,
        warnings: response.warnings ?? [],
        title,
        download: sample
          ? { bytes, filename: "样例试生成.pdf", sample: true }
          : null,
      });
      warnings.value = response.warnings ?? [];
      if (sample) {
        await recordSampleStep("sample_preview");
      }
    } catch (error) {
      if (isJobCancelled(error)) {
        return;
      }
      labelError(error);
    }
  }

  async function downloadSample(): Promise<void> {
    const state = preview.value;
    if (!state || !state.sample || !state.bytes) {
      toast("先完成样例预览，再试生成 PDF", true);
      return;
    }
    downloadBytes(state.bytes, "样例试生成.pdf");
    await recordSampleStep("trial_pdf");
    toast("样例 PDF 已下载；样例数据未写入台账");
  }

  async function downloadGenerated(): Promise<void> {
    const generated = result.value;
    if (!generated) {
      return;
    }
    try {
      const bytes = await fetchDocumentFile(generated.document.id);
      downloadBytes(bytes, "contract.pdf");
    } catch (error) {
      labelError(error);
    }
  }

  async function onPreviewDownload(): Promise<void> {
    const download = previewOverlay.value?.download;
    if (!download) {
      return;
    }
    try {
      if (download.bytes) {
        downloadBytes(download.bytes, download.filename);
        if (download.sample) {
          await recordSampleStep("trial_pdf");
          toast("样例 PDF 已下载；样例数据未写入台账");
        }
        return;
      }
      if (download.documentId) {
        const bytes = await fetchDocumentFile(download.documentId);
        downloadBytes(bytes, download.filename);
      }
    } catch (error) {
      labelError(error);
    }
  }

  async function generate(): Promise<void> {
    if (generating.value) {
      return;
    }
    const state = preview.value;
    if (!state || state.sample) {
      toast("请先用目标合同预览，再生成 PDF", true);
      return;
    }
    if (options.dirty.value) {
      toast("标定已修改，请重新预览后再生成", true);
      return;
    }
    try {
      const response = await startGenerate(() =>
        startContractGenerateJob(state.contractId, {
          template_id: options.templateItem.value.id,
          contract_id: state.contractId,
          preview_sha256: state.sha,
        }),
      );
      result.value = {
        document: response.item,
        warnings: response.warnings ?? [],
      };
      warnings.value = response.warnings ?? [];
      await refreshAfterWrite();
      void options.loadUsage();
      toast(`已生成 PDF（v${response.item.template_version}）`);
      await nextTick();
      options.editorEl.value
        ?.querySelector("#tplResult")
        ?.scrollIntoView({ block: "nearest", behavior: "smooth" });
    } catch (error) {
      if (isJobCancelled(error)) {
        return;
      }
      labelError(error);
    }
  }

  async function openGenerated(): Promise<void> {
    const generated = result.value;
    if (!generated) {
      return;
    }
    try {
      const bytes = await fetchDocumentFile(generated.document.id);
      showPreviewOverlay({
        bytes,
        warnings: generated.warnings,
        title: "生成结果",
        download: {
          documentId: generated.document.id,
          filename: "contract.pdf",
          sample: false,
        },
      });
    } catch (error) {
      labelError(error);
    }
  }

  /** 引导入口打开编辑器后的初始动作（样例预览 / 样例预览并下载）。 */
  async function runInitialAction(action: TemplateEditorAction): Promise<void> {
    if (action === "sample-preview" || action === "download-sample") {
      await previewTemplate(true);
    }
    if (action === "download-sample") {
      await downloadSample();
    }
  }

  return {
    preview,
    warnings,
    result,
    previewOverlay,
    previewOverlayKey,
    activeJob,
    invalidatePreview,
    previewTemplate,
    downloadGenerated,
    onPreviewDownload,
    generate,
    openGenerated,
    runInitialAction,
  };
}

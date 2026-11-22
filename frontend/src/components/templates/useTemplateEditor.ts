// 模板标定编辑器的状态与业务逻辑（从 TemplateEditor.vue 抽出以满足
// 单文件 ≤ 600 行）：PDF 加载、标定框增删改、保存为新版本、样例/合同预览、
// 生成 PDF、样例下载、影响范围与模板体检。
//
// 坐标与契约不变：PDF 点、左上起源、1 起页码、字号 0=10.5pt；保存时
// placements 结构与迁移前一致；PDF.js 仍用 index.html 的本地 vendor。
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  shallowRef,
  type ComponentPublicInstance,
} from "vue";
import {
  fetchDocumentFile,
  fetchTemplatePdf,
  generateContract as generateContractRequest,
  previewTemplate as previewTemplateRequest,
  saveTemplateMapping as saveTemplateMappingRequest,
  updateOnboardingState,
} from "../../api/endpoints";
import type {
  OverlayWarning,
  Placement,
  Template,
} from "../../api/types";
import {
  errorMessage,
  refreshAfterWrite,
  toast,
  type TemplateEditorAction,
} from "../../bridge/app";
import {
  base64ToBytes,
  clonePlacements,
  downloadBytes,
} from "./format";
import { loadPdfDocument, type PdfDocument, type PdfViewport } from "./pdfjs";
import { useEditorData } from "./editorData";
import { createPlacement, mappingPlacements } from "./placements";
import type {
  AddPlacementPayload,
  GeneratedResult,
  PreviewDownload,
  PreviewState,
} from "./types";

export interface TemplateEditorProps {
  template: Template;
  initialAction?: TemplateEditorAction;
}

export interface TemplateEditorCallbacks {
  onClose: () => void;
  onSaved: (template: Template) => void;
}

export const ZOOM_OPTIONS = [0.75, 1, 1.25, 1.5, 2];

export function useTemplateEditor(
  props: TemplateEditorProps,
  callbacks: TemplateEditorCallbacks,
) {
  const editorEl = ref<HTMLDivElement | null>(null);
  const templateItem = ref<Template>(props.template);
  const placements = ref<Placement[]>(clonePlacements(props.template.placements));
  const scale = ref(1.25);
  const selectedId = ref<string | null>(null);
  const dirty = ref(false);
  // 预览/预览层的字节数组不做深层响应式（避免 TypedArray 被代理后交给 PDF.js/Blob）。
  const preview = shallowRef<PreviewState | null>(null);
  const warnings = ref<OverlayWarning[] | null>(null);
  const result = ref<GeneratedResult | null>(null);
  const previewOverlay = shallowRef<{
    bytes: Uint8Array;
    warnings: OverlayWarning[];
    title: string;
    download: PreviewDownload | null;
  } | null>(null);
  const previewOverlayKey = ref(0);

  // 参考数据与只读面板（字段/公章/合同/影响范围/体检）。
  const {
    fields,
    seals,
    contracts,
    contractsLoaded,
    contractId,
    usageText,
    check,
    loadUsage,
    loadCheck,
    initialize: initializeEditorData,
  } = useEditorData(() => templateItem.value.id);

  const pdfDoc = shallowRef<PdfDocument | null>(null);
  const pageSizes = ref<PdfViewport[]>([]);
  const loadingPages = ref(true);
  const pagesError = ref("");

  let saving = false;
  let actionStarted = false;
  let disposed = false;

  const headerTitle = computed(
    () => `模板标定 · ${templateItem.value.name} v${templateItem.value.version}`,
  );

  const currentText = computed(
    () =>
      `v${templateItem.value.version} · ${templateItem.value.page_count || 0} 页 · ${placements.value.length} 个位置`,
  );

  const pages = computed(() =>
    pageSizes.value.map((size, index) => ({
      pageNumber: index + 1,
      pageSize: size,
    })),
  );

  const pageCount = computed(
    () => pageSizes.value.length || templateItem.value.page_count || 0,
  );

  const previewStatus = computed(() => {
    if (dirty.value) {
      return {
        text: "有未保存的修改；预览或生成时会先自动保存为新版本。",
        cls: "tpl-status warn",
      };
    }
    if (preview.value && !preview.value.sample) {
      return { text: "已用目标合同预览，可以生成 PDF。", cls: "tpl-status ok" };
    }
    if (preview.value && preview.value.sample) {
      return {
        text: "已用样例数据预览；生成正式 PDF 前请用目标合同预览。",
        cls: "tpl-status warn",
      };
    }
    return { text: "请先预览（样例或合同）再生成 PDF。", cls: "tpl-status" };
  });

  const canGenerate = computed(
    () => preview.value !== null && !preview.value.sample && !dirty.value,
  );

  function labelError(error: unknown): void {
    toast(errorMessage(error), true);
  }

  function markDirty(): void {
    dirty.value = true;
    preview.value = null;
  }

  /** 每次打开预览层都换 key，保证 PDF 重新加载（迁移前每次重建 overlay）。 */
  function showPreviewOverlay(payload: {
    bytes: Uint8Array;
    warnings: OverlayWarning[];
    title: string;
    download: PreviewDownload | null;
  }): void {
    previewOverlay.value = payload;
    previewOverlayKey.value += 1;
  }

  /** 模板根节点通过 :ref 回调交给编辑器（选中列表项时滚动定位用）。 */
  function setEditorRoot(
    element: Element | ComponentPublicInstance | null,
  ): void {
    editorEl.value = element instanceof HTMLDivElement ? element : null;
  }

  function selectPlacement(id: string, scroll = false): void {    selectedId.value = id;
    if (!scroll) {
      return;
    }
    const placement = placements.value.find((item) => item.id === id);
    const root = editorEl.value;
    if (!placement || !root) {
      return;
    }
    const pageEl = Array.from(
      root.querySelectorAll<HTMLElement>(".tpl-page"),
    ).find((element) => element.dataset.page === String(placement.page));
    const boxEl = Array.from(
      pageEl?.querySelectorAll<HTMLElement>(".tpl-box") ?? [],
    ).find((element) => element.dataset.id === id);
    boxEl?.scrollIntoView({ block: "center", behavior: "smooth" });
  }

  function patchPlacement(
    id: string,
    patch: { x?: number; y?: number; w?: number; h?: number },
  ): void {
    const placement = placements.value.find((item) => item.id === id);
    if (!placement) {
      return;
    }
    Object.assign(placement, patch);
    markDirty();
  }

  function removePlacement(id: string): void {
    placements.value = placements.value.filter((item) => item.id !== id);
    if (selectedId.value === id) {
      selectedId.value = null;
    }
    markDirty();
  }

  function addPlacement(payload: AddPlacementPayload): void {
    const pageSize = pageSizes.value[0];
    if (!pageSize) {
      toast("模板 PDF 尚未加载完成", true);
      return;
    }
    const created = createPlacement(payload, pageSize, placements.value);
    if (!created.placement) {
      if (created.error) {
        toast(created.error, true);
      }
      return;
    }
    const placementId = created.placement.id;
    placements.value.push(created.placement);
    markDirty();
    selectPlacement(placementId);
    // 等新框渲染到页面上再滚动定位（迁移前是同步建 DOM）。
    void nextTick(() => selectPlacement(placementId, true));
  }

  function updateFontSize(id: string, value: number): void {
    const placement = placements.value.find((item) => item.id === id);
    if (!placement) {
      return;
    }
    const size = value > 0 ? value : 0;
    placement.font_size = size;
    markDirty();
    toast(size > 0 ? `字号设为 ${size}pt（放不下时仍会自动缩小）` : "字号改为自动缩放");
  }

  function updateAlign(id: string, value: string): void {
    const placement = placements.value.find((item) => item.id === id);
    if (!placement) {
      return;
    }
    placement.align = value;
    markDirty();
  }

  async function saveMapping(): Promise<Template> {
    if (!dirty.value || saving) {
      return templateItem.value;
    }
    saving = true;
    try {
      const response = await saveTemplateMappingRequest(templateItem.value.id, {
        placements: mappingPlacements(placements.value),
      });
      const item = response.item;
      templateItem.value = item;
      placements.value = clonePlacements(item.placements);
      selectedId.value = null;
      dirty.value = false;
      preview.value = null;
      callbacks.onSaved(item);
      await refreshAfterWrite();
      void loadUsage();
      void loadCheck();
      toast(`已保存为 v${item.version}`);
      if ((response.warnings ?? []).length) {
        setTimeout(() => toast((response.warnings ?? []).join("；")), 350);
      }
      return item;
    } finally {
      saving = false;
    }
  }

  function requestSave(): void {
    saveMapping().catch(labelError);
  }

  async function recordSampleStep(step: "sample_preview" | "trial_pdf"): Promise<void> {
    try {
      await updateOnboardingState({ action: "step", step });
    } catch {
      // 引导进度记录失败不影响预览本身（与迁移前一致）。
    }
  }

  async function previewTemplate(sample: boolean): Promise<void> {
    try {
      if (dirty.value) {
        await saveMapping();
      }
      const targetId = sample ? "" : contractId.value;
      if (!sample && !targetId) {
        toast("请先选择用于预览的合同", true);
        return;
      }
      const response = await previewTemplateRequest(
        templateItem.value.id,
        sample ? { sample: true } : { contract_id: targetId },
      );
      const bytes = base64ToBytes(response.pdf_base64);
      preview.value = {
        sample,
        contractId: sample ? "" : targetId,
        sha: response.preview_sha256,
        bytes,
      };
      const contract = contracts.value.find((item) => item.id === targetId);
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
    const state = preview.value;
    if (!state || state.sample) {
      toast("请先用目标合同预览，再生成 PDF", true);
      return;
    }
    if (dirty.value) {
      toast("标定已修改，请重新预览后再生成", true);
      return;
    }
    try {
      const response = await generateContractRequest(templateItem.value.id, {
        contract_id: state.contractId,
        preview_sha256: state.sha,
      });
      result.value = {
        document: response.item,
        warnings: response.warnings ?? [],
      };
      warnings.value = response.warnings ?? [];
      await refreshAfterWrite();
      void loadUsage();
      toast(`已生成 PDF（v${response.item.template_version}）`);
      await nextTick();
      editorEl.value
        ?.querySelector("#tplResult")
        ?.scrollIntoView({ block: "nearest", behavior: "smooth" });
    } catch (error) {
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

  async function loadPdf(): Promise<void> {
    try {
      const bytes = await fetchTemplatePdf(props.template.id);
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

  async function runInitialAction(action: TemplateEditorAction): Promise<void> {
    if (action === "sample-preview" || action === "download-sample") {
      await previewTemplate(true);
    }
    if (action === "download-sample") {
      await downloadSample();
    }
  }

  function requestClose(): void {
    if (dirty.value && !window.confirm("还有未保存的标定，确定关闭吗？")) {
      return;
    }
    callbacks.onClose();
  }

  function closeDirect(): void {
    callbacks.onClose();
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === "Escape") {
      closeDirect();
    }
  }

  onMounted(() => {
    document.addEventListener("keydown", onKeydown);
    initializeEditorData();
    void loadPdf().then(() => {
      if (!disposed && !actionStarted && props.initialAction) {
        actionStarted = true;
        runInitialAction(props.initialAction).catch(labelError);
      }
    });
  });

  onBeforeUnmount(() => {
    disposed = true;
    document.removeEventListener("keydown", onKeydown);
    try {
      void pdfDoc.value?.destroy();
    } catch {
      /* 关闭编辑器时的清理失败不影响页面 */
    }
    pdfDoc.value = null;
  });

  return {
    setEditorRoot,
    templateItem,
    placements,
    scale,
    selectedId,
    fields,
    seals,
    contracts,
    contractsLoaded,
    contractId,
    usageText,
    check,
    pdfDoc,
    pages,
    pageCount,
    loadingPages,
    pagesError,
    headerTitle,
    currentText,
    previewStatus,
    canGenerate,
    warnings,
    result,
    previewOverlay,
    previewOverlayKey,
    selectPlacement,
    patchPlacement,
    removePlacement,
    addPlacement,
    updateFontSize,
    updateAlign,
    requestSave,
    requestClose,
    closeDirect,
    previewTemplate,
    generate,
    openGenerated,
    downloadGenerated,
    onPreviewDownload,
  };
}

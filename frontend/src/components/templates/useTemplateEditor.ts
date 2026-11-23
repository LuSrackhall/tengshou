// 模板标定编辑器的接线层：生命周期、键盘与关闭确认、保存为新版本，并把
// PDF 加载（useTemplatePdf）、标定框编辑（usePlacementEditing）、
// 预览/生成/下载（useTemplatePreviewGenerate）组合成组件所需的只读状态与动作。
//
// 坐标与契约不变：PDF 点、左上起源、1 起页码、字号 0=10.5pt；保存时
// placements 结构与迁移前一致；PDF.js 仍用 index.html 的本地 vendor。
import {
  computed,
  onBeforeUnmount,
  onMounted,
  ref,
  type ComponentPublicInstance,
} from "vue";
import { saveTemplateMapping as saveTemplateMappingRequest } from "../../api/endpoints";
import type { Template } from "../../api/types";
import {
  errorMessage,
  refreshAfterWrite,
  toast,
  type TemplateEditorAction,
} from "../../bridge/app";
import { useEditorData } from "./editorData";
import { mappingPlacements } from "./placements";
import { usePlacementEditing } from "./usePlacementEditing";
import { useTemplatePdf } from "./useTemplatePdf";
import { useTemplatePreviewGenerate } from "./useTemplatePreviewGenerate";

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
  const scale = ref(1.25);
  const dirty = ref(false);

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

  const pdf = useTemplatePdf(() => props.template.id);

  // 预览/生成/下载：预览前若有改动会先走保存接线。
  const actions = useTemplatePreviewGenerate({
    templateItem,
    contractId,
    contracts,
    dirty,
    saveMapping,
    editorEl,
    loadUsage,
  });

  // 标定框编辑：任何修改都会置脏并让当前预览失效。
  const editing = usePlacementEditing({
    initialPlacements: props.template.placements,
    pageSize: (page) => pdf.pageSizes.value[page - 1],
    onChanged: markDirty,
  });

  let saving = false;
  let actionStarted = false;
  let disposed = false;

  const headerTitle = computed(
    () => `模板标定 · ${templateItem.value.name} v${templateItem.value.version}`,
  );

  const currentText = computed(
    () =>
      `v${templateItem.value.version} · ${templateItem.value.page_count || 0} 页 · ${editing.placements.value.length} 个位置`,
  );

  const pageCount = computed(
    () => pdf.pageSizes.value.length || templateItem.value.page_count || 0,
  );

  const previewStatus = computed(() => {
    const preview = actions.preview.value;
    if (dirty.value) {
      return {
        text: "有未保存的修改；预览或生成时会先自动保存为新版本。",
        cls: "tpl-status warn",
      };
    }
    if (preview && !preview.sample) {
      return { text: "已用目标合同预览，可以生成 PDF。", cls: "tpl-status ok" };
    }
    if (preview && preview.sample) {
      return {
        text: "已用样例数据预览；生成正式 PDF 前请用目标合同预览。",
        cls: "tpl-status warn",
      };
    }
    return { text: "请先预览（样例或合同）再生成 PDF。", cls: "tpl-status" };
  });

  const canGenerate = computed(() => {
    const preview = actions.preview.value;
    return preview !== null && !preview.sample && !dirty.value;
  });

  function labelError(error: unknown): void {
    toast(errorMessage(error), true);
  }

  function markDirty(): void {
    dirty.value = true;
    actions.invalidatePreview();
  }

  /** 模板根节点通过 :ref 回调交给编辑器（选中列表项时滚动定位用）。 */
  function setEditorRoot(
    element: Element | ComponentPublicInstance | null,
  ): void {
    editorEl.value = element instanceof HTMLDivElement ? element : null;
  }

  async function saveMapping(): Promise<Template> {
    if (!dirty.value || saving) {
      return templateItem.value;
    }
    saving = true;
    try {
      const response = await saveTemplateMappingRequest(templateItem.value.id, {
        placements: mappingPlacements(editing.placements.value),
      });
      const item = response.item;
      templateItem.value = item;
      editing.resetPlacements(item.placements);
      dirty.value = false;
      actions.invalidatePreview();
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
    void pdf.load().then(() => {
      if (!disposed && !actionStarted && props.initialAction) {
        actionStarted = true;
        actions.runInitialAction(props.initialAction).catch(labelError);
      }
    });
  });

  onBeforeUnmount(() => {
    disposed = true;
    document.removeEventListener("keydown", onKeydown);
    pdf.dispose();
  });

  return {
    setEditorRoot,
    templateItem,
    placements: editing.placements,
    scale,
    selectedId: editing.selectedId,
    fields,
    seals,
    contracts,
    contractsLoaded,
    contractId,
    usageText,
    check,
    pdfDoc: pdf.pdfDoc,
    pages: pdf.pages,
    pageCount,
    loadingPages: pdf.loadingPages,
    pagesError: pdf.pagesError,
    headerTitle,
    currentText,
    previewStatus,
    canGenerate,
    warnings: actions.warnings,
    result: actions.result,
    previewOverlay: actions.previewOverlay,
    previewOverlayKey: actions.previewOverlayKey,
    registerPlacementElement: editing.registerPlacementElement,
    selectPlacement: editing.selectPlacement,
    patchPlacement: editing.patchPlacement,
    removePlacement: editing.removePlacement,
    addPlacement: editing.addPlacement,
    updateFontSize: editing.updateFontSize,
    updateAlign: editing.updateAlign,
    requestSave,
    requestClose,
    closeDirect,
    previewTemplate: actions.previewTemplate,
    generate: actions.generate,
    openGenerated: actions.openGenerated,
    downloadGenerated: actions.downloadGenerated,
    onPreviewDownload: actions.onPreviewDownload,
  };
}

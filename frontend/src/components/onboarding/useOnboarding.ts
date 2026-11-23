// 新手引导（阶段 1 迁出 app.js 的 loadOnboarding / handleOnboardingStep /
// setOnboardingAction / loadOnboardingCheck / loadOnboardingTemplateCheck）。
//
// 数据全部走 frontend/src/api 的 typed 端点；跨域动作走
// frontend/src/bridge/app.ts：
//   - 切 tab：bridge/app.ts 的 switchTab；
//   - 打开模板导入 / 标定编辑器 / 合同表单：tengshou:open-* 事件；
//   - 外部写入与本地写入后的刷新：tengshou:data-changed。
// 完成判定（数据库 vs config.json）、步骤顺序、首装判定与跳过语义均由
// 服务端决定，这里只负责展示与触发。

import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import {
  checkTemplate,
  getOnboarding,
  getOnboardingCheck,
  listTemplates,
  updateOnboardingState,
} from "../../api/endpoints";
import type {
  Onboarding,
  OnboardingCheckResponse,
  OnboardingStepID,
  Template,
  TemplateCheck,
} from "../../api/types";
import {
  errorMessage,
  openContractForm,
  openTemplateEditor,
  openTemplateImport,
  subscribeDataChanged,
  subscribeTabChanged,
  switchTab,
  toast,
} from "../../bridge/app";

export type CheckState = "idle" | "loading" | "error" | "ready";
export type TemplateCheckState = "idle" | "loading" | "missing" | "error" | "ready";

/** 引导步骤与模板体检关心的实体（空事件表示整体刷新）。 */
const GUIDE_ENTITIES: readonly string[] = ["contract", "template", "field", "party"];
const TEMPLATE_ENTITIES: readonly string[] = ["template", "field"];

function pickActiveTemplate(list: Template[]): Template | null {
  return list.find((item) => item.is_active) ?? list[0] ?? null;
}

export function useOnboarding() {
  const onboarding = ref<Onboarding | null>(null);
  const guideLoaded = ref(false);
  const guideError = ref("");
  const templates = ref<Template[] | null>(null);

  const check = ref<OnboardingCheckResponse | null>(null);
  const checkState = ref<CheckState>("idle");
  const checkError = ref("");

  const templateCheck = ref<TemplateCheck | null>(null);
  const templateCheckState = ref<TemplateCheckState>("idle");
  const templateCheckError = ref("");
  const templateCheckName = ref("");
  const templateCheckVersion = ref(0);

  let unsubscribeData: (() => void) | null = null;
  let unsubscribeTab: (() => void) | null = null;

  const steps = computed(() => onboarding.value?.steps ?? []);
  // 引导数据未到达前与迁移前的静态骨架一致：三个按钮都先显示，数据到达后
  // 再由服务端状态决定显隐。
  const showSkip = computed(() =>
    onboarding.value
      ? !(onboarding.value.completed || onboarding.value.skipped)
      : true,
  );
  const showComplete = computed(() =>
    onboarding.value ? !onboarding.value.completed : true,
  );
  const showReopen = computed(() =>
    onboarding.value
      ? Boolean(onboarding.value.completed || onboarding.value.skipped)
      : true,
  );

  async function loadGuide(): Promise<void> {
    try {
      onboarding.value = await getOnboarding();
      guideError.value = "";
    } catch (error) {
      guideError.value = errorMessage(error);
    } finally {
      guideLoaded.value = true;
    }
  }

  /** 读取 PDF 模板列表；失败时退回空列表（与迁移前 boot 失败时一致）。 */
  async function ensureTemplates(): Promise<Template[]> {
    try {
      const data = await listTemplates({ kind: "pdf", limit: 500 });
      templates.value = data.items ?? [];
    } catch {
      templates.value = templates.value ?? [];
    }
    return templates.value ?? [];
  }

  async function runCheck(): Promise<void> {
    checkState.value = "loading";
    checkError.value = "";
    try {
      check.value = await getOnboardingCheck();
      checkState.value = "ready";
    } catch (error) {
      checkError.value = errorMessage(error);
      checkState.value = "error";
    }
  }

  async function loadTemplateCheck(): Promise<void> {
    const tpl = pickActiveTemplate(await ensureTemplates());
    if (!tpl) {
      templateCheck.value = null;
      templateCheckState.value = "missing";
      return;
    }
    templateCheckName.value = tpl.name;
    templateCheckVersion.value = tpl.version;
    templateCheckState.value = "loading";
    templateCheckError.value = "";
    try {
      templateCheck.value = await checkTemplate(tpl.id);
      templateCheckState.value = "ready";
    } catch (error) {
      templateCheckError.value = errorMessage(error);
      templateCheckState.value = "error";
    }
  }

  /** 迁移前 switchTab("onboarding") 会同时刷新的三块内容。 */
  function refreshForTab(): void {
    void loadGuide();
    void runCheck();
    void loadTemplateCheck();
  }

  async function setAction(action: "complete" | "skip" | "reopen"): Promise<void> {
    try {
      onboarding.value = await updateOnboardingState({ action });
      toast(
        action === "complete"
          ? "引导已完成；可从顶部「新手引导」再次查看"
          : action === "skip"
            ? "已暂时跳过引导；随时可从顶部「新手引导」重新打开"
            : "引导已重新打开",
      );
    } catch (error) {
      toast(errorMessage(error), true);
    }
  }

  async function step(action: OnboardingStepID): Promise<void> {
    switch (action) {
      case "template_import":
        switchTab("templates");
        setTimeout(openTemplateImport, 60);
        return;
      case "template_calibrate": {
        const tpl = pickActiveTemplate(
          templates.value ?? (await ensureTemplates()),
        );
        if (!tpl) {
          toast("先导入一份模板，再做标定", true);
          return step("template_import");
        }
        switchTab("templates");
        openTemplateEditor({ templateId: tpl.id });
        return;
      }
      case "sample_preview":
      case "trial_pdf": {
        const tpl = pickActiveTemplate(
          templates.value ?? (await ensureTemplates()),
        );
        if (!tpl) {
          toast("先导入并标定模板，再用样例预览", true);
          return step("template_import");
        }
        switchTab("templates");
        openTemplateEditor({
          templateId: tpl.id,
          action: action === "trial_pdf" ? "download-sample" : "sample-preview",
        });
        return;
      }
      case "contract_create":
        switchTab("contracts");
        openContractForm(null);
        return;
      default:
        toast("未知的引导步骤", true);
    }
  }

  /** 「去模板标定」：模板体检通过后才可见，仍按迁移前逻辑先切 tab。 */
  function goCalibrate(): void {
    const tpl = pickActiveTemplate(templates.value ?? []);
    switchTab("templates");
    if (tpl) {
      openTemplateEditor({ templateId: tpl.id });
    }
  }

  function goFields(): void {
    switchTab("fields");
  }

  function openCheckTarget(target: string): void {
    switchTab(target);
  }

  onMounted(() => {
    void loadGuide();
    const host = document.getElementById("view-onboarding");
    if (host?.classList.contains("active")) {
      refreshForTab();
    }
    unsubscribeData = subscribeDataChanged((detail) => {
      const entities = detail.entities;
      const all = entities.length === 0;
      if (all || entities.some((entity) => GUIDE_ENTITIES.includes(entity))) {
        void loadGuide();
      }
      if (all || entities.some((entity) => TEMPLATE_ENTITIES.includes(entity))) {
        void loadTemplateCheck();
      }
    });
    unsubscribeTab = subscribeTabChanged((tab) => {
      if (tab === "onboarding") {
        refreshForTab();
      }
    });
  });

  onBeforeUnmount(() => {
    unsubscribeData?.();
    unsubscribeData = null;
    unsubscribeTab?.();
    unsubscribeTab = null;
  });

  return {
    onboarding,
    guideLoaded,
    guideError,
    steps,
    showSkip,
    showComplete,
    showReopen,
    check,
    checkState,
    checkError,
    templateCheck,
    templateCheckState,
    templateCheckError,
    templateCheckName,
    templateCheckVersion,
    start: () => void step("template_import"),
    handleStep: (action: OnboardingStepID) => void step(action),
    recheck: () => {
      void runCheck();
      void loadTemplateCheck();
    },
    refreshTemplateCheck: () => void loadTemplateCheck(),
    complete: () => void setAction("complete"),
    skip: () => void setAction("skip"),
    reopen: () => void setAction("reopen"),
    goFields,
    goCalibrate,
    openCheckTarget,
  };
}

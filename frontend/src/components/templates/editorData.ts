// 标定编辑器依赖的参考数据与只读面板：字段/公章下拉、合同选择、影响范围与
// 模板体检。逐条对应迁移前 loadEditorContracts / loadTemplateUsage /
// loadTemplateCheckPanel 的请求与文案。
import { ref } from "vue";
import {
  checkTemplate,
  getTemplateUsage,
  listContracts,
  listFields,
  listSeals,
} from "../../api/endpoints";
import type {
  Contract,
  FieldsByEntityResponse,
  Seal,
  TemplateCheck,
} from "../../api/types";
import { errorMessage } from "../../bridge/app";
import type { CheckView } from "./types";

export function useEditorData(currentTemplateId: () => string) {
  const fields = ref<FieldsByEntityResponse>({ contract: [], party: [] });
  const seals = ref<Seal[]>([]);
  const contracts = ref<Contract[]>([]);
  const contractsLoaded = ref(false);
  const contractId = ref("");
  const usageText = ref("正在统计影响范围…");
  const check = ref<CheckView>({ kind: "loading" });

  async function loadUsage(): Promise<void> {
    try {
      const usage = await getTemplateUsage(currentTemplateId());
      usageText.value =
        `影响范围：${usage.family_contracts} 份合同使用该模板系列` +
        `（其中 ${usage.other_version_contracts} 份仍用旧版本）· 已生成 ${usage.documents} 份 PDF`;
    } catch (error) {
      usageText.value = `影响范围统计失败：${errorMessage(error)}`;
    }
  }

  async function loadCheck(): Promise<void> {
    try {
      const data: TemplateCheck = await checkTemplate(currentTemplateId());
      const issues = data.issues ?? [];
      check.value = issues.length ? { kind: "issues", issues } : { kind: "ok" };
    } catch (error) {
      check.value = { kind: "error", text: `模板体检失败：${errorMessage(error)}` };
    }
  }

  async function loadReferenceData(): Promise<void> {
    const [fieldData, sealData] = await Promise.all([
      listFields({ archived: "1" }).catch(() => null),
      listSeals({ limit: 500 }).catch(() => null),
    ]);
    if (fieldData) {
      fields.value = {
        contract: fieldData.contract ?? [],
        party: fieldData.party ?? [],
      };
    }
    if (sealData) {
      seals.value = sealData.items ?? [];
    }
  }

  async function loadContracts(): Promise<void> {
    try {
      const data = await listContracts({ limit: 500 });
      const items = data.items ?? [];
      contracts.value = items;
      // 迁移前 <select> 会默认选中第一份合同，合同预览无需再手动选择。
      if (!items.some((item) => item.id === contractId.value)) {
        contractId.value = items[0]?.id ?? "";
      }
    } catch {
      contracts.value = [];
    } finally {
      contractsLoaded.value = true;
    }
  }

  function initialize(): void {
    void loadReferenceData();
    void loadContracts();
    void loadUsage();
    void loadCheck();
  }

  return {
    fields,
    seals,
    contracts,
    contractsLoaded,
    contractId,
    usageText,
    check,
    loadUsage,
    loadCheck,
    initialize,
  };
}

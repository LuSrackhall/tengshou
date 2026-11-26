<script setup lang="ts">
// 合同台账（阶段 1 迁移）：年度/状态/关键词筛选、动态字段列、行操作
// （生成 PDF / 扫码签名 / 编辑 / 删除）、批量勾选与批量生成、Excel 模板与
// 台账导出，以及新建/编辑/续签表单的打开逻辑。
//
// 结构与 app.js renderContracts 的输出一致（toolbar / summary / data-table /
// actions 类名不变），数据走 frontend/src/api 的 typed 端点。
//
// 跨域：Excel 导入由 components/imports 的 Vue 组件承接，经 bridge/app.ts
// 调用；「以此档案新建合同」「历史合同」等入口经 tengshou:open-contract-form
// 事件打开本域表单。扫码
// 签名对话框与签名摘要已迁到 Vue（components/signatures/），由本岛打开、
// 合同表单内渲染。合同详情里的生成面板与批量生成对话框由本域 Vue 组件渲染
// （模板页的「批量生成 / 整改重出」也经 tengshou:open-batch-generate 落到
// 这里）；外部写入经 shell/realtime.ts 派发的 tengshou:data-changed 到达后重取。
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  deleteContract as deleteContractRequest,
  downloadExcelLedger,
  downloadExcelTemplate,
  getBootstrap,
  getContract,
  getRenewalDraft,
  listAssets,
  listContracts,
  listFields,
  listParties,
} from "../../api/endpoints";
import type {
  Asset,
  Bootstrap,
  Contract,
  ContractDetail,
  ContractStatus,
  FieldDefinition,
  Party,
  RenewalDraft,
  YearCount,
} from "../../api/types";
import {
  errorMessage,
  openBatchGenerateDialog,
  openExcelImport,
  refreshAfterWrite,
  subscribeDataChanged,
  subscribeOpenBatchGenerate,
  subscribeOpenContractForm,
  toast,
  type BatchGenerateOptions,
  type ContractFormOptions,
  type LedgerBridge,
  type PartyRef,
} from "../../bridge/app";
import SignatureDialog from "../signatures/SignatureDialog.vue";
import { useSignatureDialog } from "../signatures/useSignatureDialog";
import BatchGenerateDialog from "./BatchGenerateDialog.vue";
import ContractFormModal from "./ContractFormModal.vue";
import { saveDownloadedFile } from "./download";
import { contractFieldValue, statusLabel } from "./format";

type StatusFilter = "" | ContractStatus;

interface FormRequest {
  key: number;
  mode: "create" | "edit";
  contract: Contract | null;
  detail: ContractDetail | null;
  renewal: RenewalDraft | null;
  partyB: PartyRef | null;
  focusGenerate: boolean;
}

interface FormDraft {
  mode: "create" | "edit";
  contract?: Contract | null;
  detail?: ContractDetail | null;
  renewal?: RenewalDraft | null;
  partyB?: PartyRef | null;
  focusGenerate?: boolean;
}

const bootstrap = ref<Bootstrap | null>(null);
const fields = ref<FieldDefinition[]>([]);
const parties = ref<Party[]>([]);
const assets = ref<Asset[]>([]);
const contracts = ref<Contract[]>([]);
const total = ref(0);
const years = ref<YearCount[]>([]);
const year = ref("");
const statusFilter = ref<StatusFilter>("");
const search = ref("");
const activeQuery = ref("");
const loaded = ref(false);
const selectedIds = ref<string[]>([]);
const formRequest = ref<FormRequest | null>(null);
const batchRequest = ref<{ key: number; options: BatchGenerateOptions } | null>(null);
const {
  signRequest,
  open: openSignature,
  close: closeSignature,
} = useSignatureDialog();

let yearInitialized = false;
let formSeq = 0;
let batchSeq = 0;
let loadSeq = 0;
let searchTimer: ReturnType<typeof setTimeout> | undefined;
let reloadTimer: ReturnType<typeof setTimeout> | undefined;
let unsubData: (() => void) | null = null;
let unsubOpenForm: (() => void) | null = null;
let unsubBatch: (() => void) | null = null;

const tableFields = computed(() => fields.value.filter((field) => field.in_ledger));

const summary = computed(
  () =>
    `${year.value ? `${year.value} 年度` : "全部年度"} · 共 ${total.value} 份` +
    (statusFilter.value ? ` · 状态：${statusLabel(statusFilter.value)}` : "") +
    (activeQuery.value ? ` · 搜索「${activeQuery.value}」` : ""),
);

const yearOptions = computed(() => {
  const options = years.value.map((item) => ({
    value: String(item.year),
    label: `${item.year} 年（${item.count} 份）`,
  }));
  if (!options.length && bootstrap.value) {
    const current = bootstrap.value.current_year;
    options.push({ value: String(current), label: `${current} 年` });
  }
  return options;
});

const visibleIds = computed(() => contracts.value.map((contract) => contract.id));
const selectedVisible = computed(
  () => visibleIds.value.filter((id) => selectedIds.value.includes(id)).length,
);
const allSelected = computed(
  () => visibleIds.value.length > 0 && selectedVisible.value === visibleIds.value.length,
);
const indeterminate = computed(
  () => selectedVisible.value > 0 && selectedVisible.value < visibleIds.value.length,
);
const batchLabel = computed(() =>
  selectedIds.value.length
    ? `批量生成 PDF（${selectedIds.value.length}）`
    : "批量生成 PDF",
);
const exportTitle = computed(
  () => `${year.value ? `${year.value} 年度` : "全部年度"}台账 Excel`,
);
const currentYear = computed(() =>
  bootstrap.value ? bootstrap.value.current_year : null,
);

const ledgerBridge: LedgerBridge = {
  currentContracts: () => contracts.value.slice(),
};

function defaultYear(boot: Bootstrap): string {
  const list = boot.years ?? [];
  const hasCurrent = list.some((item) => item.year === boot.current_year);
  if (hasCurrent) {
    return String(boot.current_year);
  }
  return list.length ? String(list[0]?.year ?? "") : "";
}

function buildQuery(): Parameters<typeof listContracts>[0] {
  const query: Parameters<typeof listContracts>[0] = { limit: 500 };
  if (year.value) {
    query.year = Number(year.value);
  }
  if (statusFilter.value) {
    query.status = statusFilter.value;
  }
  if (activeQuery.value) {
    query.q = activeQuery.value;
  }
  return query;
}

async function reload(includeMeta: boolean): Promise<void> {
  const seq = ++loadSeq;
  try {
    if (includeMeta) {
      const [boot, fieldData, partyData, assetData] = await Promise.all([
        getBootstrap(),
        listFields({ archived: "1" }),
        listParties({ limit: 500 }),
        listAssets({ limit: 500 }),
      ]);
      if (seq !== loadSeq) {
        return;
      }
      bootstrap.value = boot;
      fields.value = (fieldData.contract ?? []).filter((field) => !field.archived);
      parties.value = partyData.items ?? [];
      assets.value = assetData.items ?? [];
      years.value = boot.years ?? [];
      if (!yearInitialized) {
        year.value = defaultYear(boot);
        yearInitialized = true;
      } else if (
        year.value &&
        !years.value.some((item) => String(item.year) === year.value)
      ) {
        year.value = "";
      }
    }
    const data = await listContracts(buildQuery());
    if (seq !== loadSeq) {
      return;
    }
    contracts.value = data.items ?? [];
    total.value = data.total ?? 0;
    const visible = new Set(contracts.value.map((contract) => contract.id));
    selectedIds.value = selectedIds.value.filter((id) => visible.has(id));
    loaded.value = true;
  } catch (error) {
    if (seq !== loadSeq) {
      return;
    }
    loaded.value = true;
    toast(errorMessage(error), true);
  }
}

function scheduleReload(delay: number): void {
  if (reloadTimer !== undefined) {
    clearTimeout(reloadTimer);
  }
  reloadTimer = setTimeout(() => {
    reloadTimer = undefined;
    void reload(true);
  }, delay);
}

/** 相对方增改只影响台账的名称列与新建表单的 datalist：单独重取即可。 */
async function refreshParties(): Promise<void> {
  try {
    const data = await listParties({ limit: 500 });
    parties.value = data.items ?? [];
  } catch {
    /* transient: keep the previous list */
  }
}

watch(search, () => {
  if (searchTimer !== undefined) {
    clearTimeout(searchTimer);
  }
  searchTimer = setTimeout(() => {
    searchTimer = undefined;
    activeQuery.value = search.value.trim();
    void reload(false);
  }, 250);
});

function isSelected(id: string): boolean {
  return selectedIds.value.includes(id);
}

function toggleSelect(id: string): void {
  selectedIds.value = isSelected(id)
    ? selectedIds.value.filter((item) => item !== id)
    : [...selectedIds.value, id];
}

function toggleAll(event: Event): void {
  const checked = (event.target as HTMLInputElement).checked;
  const visible = visibleIds.value;
  selectedIds.value = checked
    ? Array.from(new Set([...selectedIds.value, ...visible]))
    : selectedIds.value.filter((id) => !visible.includes(id));
}

function cellValue(contract: Contract, field: FieldDefinition) {
  return contractFieldValue(contract, field, parties.value);
}

function cellClass(contract: Contract, field: FieldDefinition): string {
  const value = cellValue(contract, field);
  if (value.badge) {
    return "";
  }
  return [value.mono ? "mono" : "", value.money ? "money" : "", value.wrap ? "wrap" : ""]
    .filter(Boolean)
    .join(" ");
}

function openForm(draft: FormDraft): void {
  formRequest.value = {
    key: ++formSeq,
    mode: draft.mode,
    contract: draft.contract ?? null,
    detail: draft.detail ?? null,
    renewal: draft.renewal ?? null,
    partyB: draft.partyB ?? null,
    focusGenerate: draft.focusGenerate ?? false,
  };
}

function openCreate(partyB: PartyRef | null = null): void {
  openForm({ mode: "create", partyB });
}

async function openEdit(contract: Contract, focusGenerate = false): Promise<void> {
  try {
    const detail = await getContract(contract.id);
    openForm({ mode: "edit", contract, detail, focusGenerate });
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

async function openRenew(contract: Contract): Promise<void> {
  try {
    const draft = await getRenewalDraft(contract.id);
    openForm({ mode: "create", renewal: draft });
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

function openFromBridge(contract: Contract | null, options: ContractFormOptions): void {
  if (options.renew) {
    if (contract) {
      void openRenew(contract);
    }
    return;
  }
  if (contract) {
    void openEdit(contract, Boolean(options.focusGenerate));
    return;
  }
  openCreate(options.partyB ?? null);
}

function closeForm(): void {
  formRequest.value = null;
}

function onCreated(contract: Contract): void {
  void openEdit(contract);
}

function onRenew(contract: Contract): void {
  closeForm();
  void openRenew(contract);
}

function onOpenContract(contract: Contract): void {
  void openEdit(contract);
}

async function remove(contract: Contract): Promise<void> {
  if (
    !window.confirm(
      `确定删除合同 ${contract.contract_no}「${contract.title}」吗？删除会记入变更记录。`,
    )
  ) {
    return;
  }
  try {
    await deleteContractRequest(contract.id);
    toast("合同已删除");
    await refreshAfterWrite();
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

function batchGenerate(): void {
  const selected = contracts.value.filter((contract) =>
    selectedIds.value.includes(contract.id),
  );
  if (!selected.length) {
    toast("请先在台账中勾选要生成 PDF 的合同", true);
    return;
  }
  openBatchGenerateDialog({
    title: `批量生成 PDF · ${selected.length} 份合同`,
    contracts: selected,
  });
}

/** 批量生成事件（本岛与模板页共用）：对话框由本岛渲染，无合同时按旧行为提示。 */
function openBatchFromBridge(options: BatchGenerateOptions): void {
  if (!options.contracts.length) {
    toast("请选择要生成 PDF 的合同", true);
    return;
  }
  batchRequest.value = { key: ++batchSeq, options };
}

function closeBatch(): void {
  batchRequest.value = null;
}

async function downloadTemplate(): Promise<void> {
  try {
    const file = await downloadExcelTemplate();
    saveDownloadedFile(file, "tengshou-entry-template.xlsx");
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

async function exportLedger(): Promise<void> {
  try {
    const file = await downloadExcelLedger({
      year: year.value ? Number(year.value) : undefined,
      status: statusFilter.value || undefined,
      q: activeQuery.value || undefined,
    });
    saveDownloadedFile(file, "tengshou-ledger.xlsx");
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

onMounted(() => {
  window.__tengshouContracts = ledgerBridge;
  void reload(true);
  unsubData = subscribeDataChanged((detail) => {
    const entities = detail.entities;
    if (entities.includes("party") && !entities.includes("contract")) {
      void refreshParties();
    }
    if (
      entities.length === 0 ||
      entities.includes("contract") ||
      entities.includes("field") ||
      entities.includes("asset")
    ) {
      scheduleReload(150);
    }
  });
  unsubOpenForm = subscribeOpenContractForm((detail) => {
    openFromBridge(detail.contract, detail.options ?? {});
  });
  unsubBatch = subscribeOpenBatchGenerate(openBatchFromBridge);
});

onBeforeUnmount(() => {
  if (window.__tengshouContracts === ledgerBridge) {
    delete window.__tengshouContracts;
  }
  unsubData?.();
  unsubData = null;
  unsubOpenForm?.();
  unsubOpenForm = null;
  unsubBatch?.();
  unsubBatch = null;
  if (searchTimer !== undefined) {
    clearTimeout(searchTimer);
    searchTimer = undefined;
  }
  if (reloadTimer !== undefined) {
    clearTimeout(reloadTimer);
    reloadTimer = undefined;
  }
});
</script>

<template>
  <div class="toolbar">
    <label class="inline">年度
      <select id="yearSelect" v-model="year" @change="reload(false)">
        <option value="">全部年度（跨年查询）</option>
        <option v-for="option in yearOptions" :key="option.value" :value="option.value">
          {{ option.label }}
        </option>
      </select>
    </label>
    <input
      id="contractSearch"
      v-model="search"
      type="search"
      placeholder="搜索编号 / 名称 / 相对方"
      autocomplete="off"
    >
    <select id="statusFilter" v-model="statusFilter" aria-label="合同状态" @change="reload(false)">
      <option value="">全部状态</option>
      <option value="draft">草稿</option>
      <option value="active">生效</option>
      <option value="archived">归档</option>
    </select>
    <span class="spacer"></span>
    <button type="button" id="newContractBtn" class="primary" @click="openCreate(null)">新建合同</button>
    <button type="button" id="batchGenerateBtn" :disabled="!selectedIds.length" @click="batchGenerate">
      {{ batchLabel }}
    </button>
    <button type="button" id="downloadTemplateBtn" @click="downloadTemplate">Excel 模板</button>
    <button type="button" id="importBtn" @click="openExcelImport">导入 Excel</button>
    <button type="button" id="exportLedgerBtn" :title="exportTitle" @click="exportLedger">导出台账</button>
  </div>
  <div id="contractSummary" class="summary">{{ loaded ? summary : "" }}</div>
  <div class="table-wrap">
    <table id="contractTable" class="data-table">
      <template v-if="loaded">
        <tbody v-if="!tableFields.length">
          <tr><td class="empty">没有可显示的台账字段，请先在「字段」页新增字段。</td></tr>
        </tbody>
        <template v-else>
          <thead>
            <tr>
              <th class="select-col">
                <input
                  id="contractSelectAll"
                  type="checkbox"
                  title="全选当前台账合同"
                  aria-label="全选当前台账合同"
                  :checked="allSelected"
                  :indeterminate="indeterminate"
                  @change="toggleAll"
                >
              </th>
              <th v-for="field in tableFields" :key="field.key">{{ field.label }}</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="contract in contracts"
              :key="contract.id"
              :data-id="contract.id"
              @click="openEdit(contract)"
            >
              <td class="select-col">
                <input
                  type="checkbox"
                  :data-select="contract.id"
                  :checked="isSelected(contract.id)"
                  :aria-label="`选择合同 ${contract.contract_no}`"
                  @click.stop
                  @change="toggleSelect(contract.id)"
                >
              </td>
              <td v-for="field in tableFields" :key="field.key" :class="cellClass(contract, field)">
                <span v-if="cellValue(contract, field).badge" class="badge" :class="cellValue(contract, field).badge">
                  {{ cellValue(contract, field).text }}
                </span>
                <template v-else>{{ cellValue(contract, field).text }}</template>
              </td>
              <td>
                <div class="actions">
                  <button type="button" data-action="generate" @click.stop="openEdit(contract, true)">生成 PDF</button>
                  <button type="button" data-action="sign" @click.stop="openSignature(contract)">扫码签名</button>
                  <button type="button" data-action="edit" @click.stop="openEdit(contract)">编辑</button>
                  <button type="button" class="danger" data-action="delete" @click.stop="remove(contract)">删除</button>
                </div>
              </td>
            </tr>
            <tr v-if="!contracts.length">
              <td class="empty" :colspan="tableFields.length + 2">还没有合同。可以新建，或下载 Excel 模板填写后导入。</td>
            </tr>
          </tbody>
        </template>
      </template>
    </table>
  </div>
  <ContractFormModal
    v-if="formRequest"
    :key="formRequest.key"
    :mode="formRequest.mode"
    :contract="formRequest.contract"
    :detail="formRequest.detail"
    :renewal="formRequest.renewal"
    :party-b="formRequest.partyB"
    :fields="fields"
    :parties="parties"
    :assets="assets"
    :current-year="currentYear"
    :focus-generate="formRequest.focusGenerate"
    @close="closeForm"
    @created="onCreated"
    @renew="onRenew"
    @open-contract="onOpenContract"
  />
  <BatchGenerateDialog
    v-if="batchRequest"
    :key="batchRequest.key"
    :title="batchRequest.options.title"
    :contracts="batchRequest.options.contracts"
    :template-id="batchRequest.options.templateId"
    :kind="batchRequest.options.kind"
    @close="closeBatch"
  />
  <SignatureDialog
    v-if="signRequest"
    :key="signRequest.key"
    :contract="signRequest.contract"
    :detail="signRequest.detail"
    @close="closeSignature"
  />
</template>

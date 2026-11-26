<script setup lang="ts">
// 合同新建/编辑弹窗（阶段 1 迁移 + v0.2 三方选择）：甲方档案默认带出、
// 乙方从租户档案选择（搜索/最近使用/快速新建）、租赁房产从房产档案选择，
// 租期默认一年；其余字段仍由动态字段定义驱动，422 逐字段错误与续签草稿
// 语义不变。
//
// 跨域：扫码签名与签名摘要已迁到 Vue——摘要由 components/signatures 的
// SignatureSummary 在本组件内渲染，扫码签名对话框由合同台账岛打开。
// 「生成 PDF」面板与批量生成对话框已迁到 Vue
// （ContractGenerationPanel / BatchGenerateDialog），「已关联公章」摘要用
// 公章域的 SealSummary 渲染。
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { ApiError } from "../../api/client";
import {
  createContract,
  deleteContract as deleteContractRequest,
  getContract,
  updateContract,
} from "../../api/endpoints";
import type {
  ApiFieldError,
  Asset,
  Contract,
  ContractDetail,
  FieldDefinition,
  Party,
  RenewalDraft,
} from "../../api/types";
import {
  errorMessage,
  refreshAfterWrite,
  subscribeChange,
  toast,
  type AppChangeNotice,
  type PartyRef,
} from "../../bridge/app";
import SealSummary from "../seals/SealSummary.vue";
import SignatureSummary from "../signatures/SignatureSummary.vue";
import AssetPicker from "./AssetPicker.vue";
import ContractAuditHistory from "./ContractAuditHistory.vue";
import ContractFieldControl from "./ContractFieldControl.vue";
import ContractGenerationPanel from "./ContractGenerationPanel.vue";
import PartyBPicker from "./PartyBPicker.vue";
import { defaultPartyAID, oneYearLeaseEnd, rememberPartyA, todayISO } from "./lease";
import { isAgentActor, kindLabel } from "../../ui/format";
import { partyName } from "./format";
import {
  collectContractInput,
  renewalSeed,
  syncContractValues,
  type ContractSeed,
} from "./form";

const props = defineProps<{
  mode: "create" | "edit";
  contract: Contract | null;
  detail: ContractDetail | null;
  renewal: RenewalDraft | null;
  partyB: PartyRef | null;
  fields: FieldDefinition[];
  parties: Party[];
  assets: Asset[];
  currentYear: number | null;
  focusGenerate: boolean;
}>();

const emit = defineEmits<{
  close: [];
  renew: [contract: Contract];
  created: [contract: Contract];
  "open-contract": [contract: Contract];
}>();

const values = reactive<Record<string, string>>({});
const saving = ref(false);
const fieldErrors = ref<ApiFieldError[]>([]);
const errorText = ref("");
const bodyEl = ref<HTMLDivElement | null>(null);
const renewalSource = ref<Contract | null>(null);
const renewalSourceMissing = ref(false);
let unsubscribe: (() => void) | null = null;

const contractId = computed(() => props.contract?.id ?? "");

const seed = computed<ContractSeed | null>(() => {
  if (props.mode === "edit") {
    return props.contract;
  }
  if (props.renewal) {
    return renewalSeed(props.renewal);
  }
  return null;
});

const createdParties = ref<Party[]>([]);
const createdAssets = ref<Asset[]>([]);

function dedupeByID<T extends { id: string }>(items: Array<T | null | undefined>): T[] {
  const seen = new Set<string>();
  const out: T[] = [];
  for (const item of items) {
    if (item && !seen.has(item.id)) {
      seen.add(item.id);
      out.push(item);
    }
  }
  return out;
}

const allParties = computed(() =>
  dedupeByID([...props.parties, ...createdParties.value, props.detail?.party_b, props.detail?.party_a]),
);

const allAssets = computed(() =>
  dedupeByID([...props.assets, ...createdAssets.value, props.detail?.asset]),
);

function initialPartyAID(): string {
  if (props.mode === "edit") {
    return props.contract?.party_a_id ?? "";
  }
  if (props.renewal) {
    return props.renewal.input.party_a_id ?? "";
  }
  return defaultPartyAID(props.parties);
}

const partyAID = ref(initialPartyAID());

// 甲方固定档案默认带出；档案异步到达时补一次默认值，用户已选择不覆盖。
watch(
  () => props.parties,
  (parties) => {
    if (props.mode === "create" && !props.renewal && !partyAID.value) {
      partyAID.value = defaultPartyAID(parties);
    }
  },
);

watch(partyAID, (id) => {
  if (props.mode === "create" && !props.renewal) {
    rememberPartyA(id);
  }
});

/** 编辑时甲方档案不在列表（已删除 / 列表截断）也显示可读名称。 */
const partyALabel = computed(() => {
  if (!partyAID.value) {
    return "";
  }
  const party = allParties.value.find((item) => item.id === partyAID.value);
  if (party) {
    return `${party.name}（${kindLabel(party.kind)}）`;
  }
  return props.detail?.party_a?.name ?? partyAID.value;
});

const partyAInList = computed(() =>
  props.parties.some((item) => item.id === partyAID.value),
);

function initialPartyBID(): string {
  if (props.mode === "edit") {
    return props.detail?.party_b?.id ?? props.contract?.party_b_id ?? "";
  }
  if (props.renewal) {
    return props.renewal.input.party_b_id ?? "";
  }
  return props.partyB?.id ?? "";
}

const partyBID = ref(initialPartyBID());

const partyBName = computed(() => {
  const party = allParties.value.find((item) => item.id === partyBID.value);
  if (party) {
    return party.name;
  }
  if (props.mode === "edit") {
    return (
      props.detail?.party_b?.name ??
      partyName(props.parties, props.contract?.party_b_id)
    );
  }
  if (props.renewal) {
    return (
      props.renewal.input.party_b ||
      partyName(props.parties, props.renewal.input.party_b_id)
    );
  }
  return props.partyB?.name ?? "";
});

watch(
  partyBName,
  (name) => {
    values.party_b = name;
  },
  { immediate: true },
);

// v0.2：选中（或打开）缺少证件号的存量档案时提示一次补录；补录后永久有效，
// 同一档案在同一浏览器只提示一次，不打断合同录入。
watch(
  [partyBID, () => props.parties],
  () => {
    const match = allParties.value.find((item) => item.id === partyBID.value);
    if (!match || (match.id_number ?? "").trim() !== "") {
      return;
    }
    const key = `tengshou:party-id-backfill:${match.id}`;
    try {
      if (window.localStorage.getItem(key) === "1") {
        return;
      }
      window.localStorage.setItem(key, "1");
    } catch {
      /* 隐私模式禁用 localStorage 时仍提示，但不落标记 */
    }
    toast(
      `相对方「${match.name}」还没有证件号，请在「相对方档案」页补录一次，补录后长期有效`,
      true,
    );
  },
  { immediate: true },
);

function initialAssetID(): string {
  if (props.mode === "edit") {
    return props.contract?.asset_id ?? props.detail?.asset?.id ?? "";
  }
  if (props.renewal) {
    return props.renewal.input.asset_id ?? "";
  }
  return "";
}

const assetID = ref(initialAssetID());

const title = computed(() => {
  if (props.mode === "edit" && props.contract) {
    return `编辑合同 · ${props.contract.contract_no}`;
  }
  if (props.renewal) {
    return `新建合同 · 续签自 ${props.renewal.source.contract_no}`;
  }
  return "新建合同";
});

const reusedCount = computed(() => props.renewal?.reused?.length ?? 0);
const renewalAlreadyClosed = computed(() => renewalSourceMissing.value && !renewalSource.value);

/** 乙方选择器替代内置 party_b 输入框，其余字段仍由字段定义驱动。 */
const contractFields = computed(() =>
  props.fields.filter((field) => field.key !== "party_b"),
);

syncContractValues(values, props.fields, seed.value, partyBName.value, props.currentYear);

// 租期默认一年：新建（含续签）时带出今天 → 一年后的前一天，用户可改。
const leaseAutoEnd = ref("");
function applyLeaseDefaults(): void {
  if (props.mode !== "create" || !("lease_start" in values) || values.lease_start) {
    return;
  }
  values.lease_start = todayISO();
  values.lease_end = oneYearLeaseEnd(values.lease_start);
  leaseAutoEnd.value = values.lease_end;
}
applyLeaseDefaults();

function setFieldValue(key: string, value: string): void {
  if (key === "lease_start") {
    const previousAuto = leaseAutoEnd.value;
    values.lease_start = value;
    if (!values.lease_end || values.lease_end === previousAuto) {
      values.lease_end = value ? oneYearLeaseEnd(value) : "";
      leaseAutoEnd.value = values.lease_end;
    }
    return;
  }
  if (key === "lease_end") {
    leaseAutoEnd.value = "";
  }
  values[key] = value;
}

function onPartyCreated(party: Party): void {
  createdParties.value = [party, ...createdParties.value];
}

function onAssetCreated(asset: Asset): void {
  createdAssets.value = [asset, ...createdAssets.value];
}

function onKeydown(event: KeyboardEvent): void {
  if (event.key === "Escape") {
    emit("close");
  }
}

// 外部写入本合同时提示核对；审计区块负责自身刷新。
function onChange(notice: AppChangeNotice): void {
  if (
    props.mode === "edit" &&
    contractId.value &&
    notice.entity === "contract" &&
    notice.entity_id === contractId.value &&
    isAgentActor(notice.actor)
  ) {
    toast("Agent（MCP）修改了当前打开的记录，变更记录已刷新；请核对后再保存");
  }
}

function showError(error: unknown): void {
  fieldErrors.value = [];
  errorText.value = "";
  if (error instanceof ApiError && error.fields.length) {
    fieldErrors.value = error.fields;
  } else {
    errorText.value = errorMessage(error);
  }
  void nextTick(() => {
    if (bodyEl.value) {
      bodyEl.value.scrollTop = 0;
    }
  });
}

async function submit(): Promise<void> {
  if (saving.value) {
    return;
  }
  saving.value = true;
  fieldErrors.value = [];
  errorText.value = "";
  const input = collectContractInput(
    props.fields,
    values,
    seed.value,
    partyAID.value,
    partyBID.value,
    partyBName.value,
    assetID.value,
  );
  try {
    if (props.mode === "edit" && props.contract) {
      const result = await updateContract(props.contract.id, input);
      emit("close");
      toast("合同已保存");
      const warnings = result.warnings ?? [];
      if (warnings.length) {
        window.setTimeout(() => toast(warnings.join("；")), 350);
      }
      await refreshAfterWrite();
      return;
    }
    if (props.renewal) {
      input.source_contract_id = props.renewal.source.id;
    }
    const result = await createContract(input);
    if (props.renewal) {
      toast(`合同已创建（续签自 ${props.renewal.source.contract_no}）`);
      const warnings = result.warnings ?? [];
      if (warnings.length) {
        window.setTimeout(() => toast(warnings.join("；")), 350);
      }
      await refreshAfterWrite();
      emit("created", result.item);
      return;
    }
    emit("close");
    toast("合同已保存");
    const warnings = result.warnings ?? [];
    if (warnings.length) {
      window.setTimeout(() => toast(warnings.join("；")), 350);
    }
    await refreshAfterWrite();
  } catch (error) {
    saving.value = false;
    showError(error);
  }
}

async function remove(): Promise<void> {
  const contract = props.contract;
  if (!contract) {
    return;
  }
  if (
    !window.confirm(
      `确定删除合同 ${contract.contract_no}「${contract.title}」吗？删除会记入变更记录。`,
    )
  ) {
    return;
  }
  try {
    await deleteContractRequest(contract.id);
    emit("close");
    toast("合同已删除");
    await refreshAfterWrite();
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

function renew(): void {
  if (props.contract) {
    emit("renew", props.contract);
  }
}

function openSource(): void {
  if (renewalSource.value) {
    emit("open-contract", renewalSource.value);
  }
}

async function loadRenewalSource(): Promise<void> {
  const sourceId = props.contract?.source_contract_id;
  if (!sourceId) {
    return;
  }
  try {
    const source = await getContract(sourceId);
    renewalSource.value = source.contract;
  } catch {
    renewalSourceMissing.value = true;
  }
}

// 行内「生成 PDF」入口：生成面板完成首次渲染（模板加载后高度稳定）再滚动。
function onGenerationReady(): void {
  if (!props.focusGenerate) {
    return;
  }
  void nextTick(() => {
    const panel = bodyEl.value?.querySelector("#contractGeneration");
    if (panel instanceof HTMLElement) {
      panel.scrollIntoView({ block: "start", behavior: "smooth" });
    }
  });
}

onMounted(() => {
  document.addEventListener("keydown", onKeydown);
  unsubscribe = subscribeChange(onChange);
  if (props.contract?.source_contract_id) {
    void loadRenewalSource();
  }
});

onBeforeUnmount(() => {
  document.removeEventListener("keydown", onKeydown);
  unsubscribe?.();
  unsubscribe = null;
});
</script>

<template>
  <Teleport to="body">
    <div class="modal-backdrop">
      <div class="modal wide" role="dialog" aria-modal="true">
        <div class="modal-head">
          <h2>{{ title }}</h2>
          <button type="button" class="close" aria-label="关闭" @click="emit('close')">×</button>
        </div>
        <div ref="bodyEl" class="modal-body">
          <div id="contractErrors">
            <div v-if="fieldErrors.length" class="form-errors">
              请修正以下问题：<ul>
                <li v-for="problem in fieldErrors" :key="problem.field">
                  {{ problem.label || problem.field }}：{{ problem.message }}
                </li>
              </ul>
            </div>
            <div v-else-if="errorText" class="form-errors">{{ errorText }}</div>
          </div>

          <div
            v-if="mode === 'edit' && contract && contract.source_contract_id"
            id="renewalSource"
            class="renewal-banner"
          >
            <template v-if="renewalSource">续签自 <strong>{{ renewalSource.contract_no }}「{{ renewalSource.title }}」</strong>
              <button type="button" id="openSourceContract" @click="openSource">查看来源合同</button>
            </template>
            <template v-else-if="renewalAlreadyClosed">续签来源合同已不存在（来源编号仍保留在本合同记录中）。</template>
            <template v-else>正在加载续签来源…</template>
          </div>

          <div v-if="renewal" class="renewal-banner">
            续签自 <strong>{{ renewal.source.contract_no }}「{{ renewal.source.title }}」</strong>：
            已带出 {{ reusedCount }} 项信息，均可修改。合同编号自动分配，签订 / 生效 / 到期日期与状态已重置；
            签名与公章不会沿用，生成 PDF 前需按新内容重新采集。
          </div>
          <div v-else-if="partyB" class="renewal-banner">
            以相对方档案「{{ partyB.name }}」新建合同：
            仅带出档案信息，不复制旧合同内容；模板与字段由你选择，保存后按正常流程采集签名。
          </div>

          <div v-if="mode === 'edit'" class="warning-list">
            合同编号创建后不可修改。修改会写入变更记录。
          </div>

          <div v-if="detail && detail.signatures && detail.signatures.length" id="contractSignatures">
            <SignatureSummary :detail="detail" />
          </div>
          <div v-if="detail && detail.seals && detail.seals.length" id="contractSealSummary">
            <SealSummary :seals="detail.seals" />
          </div>

          <div class="form-grid">
            <div class="field">
              <label for="f_contract_party_a">甲方（本方）</label>
              <select id="f_contract_party_a" v-model="partyAID">
                <option value="">（未设置）</option>
                <option v-if="partyAID && !partyAInList" :value="partyAID">{{ partyALabel }}</option>
                <option v-for="party in parties" :key="party.id" :value="party.id">
                  {{ party.name }}（{{ kindLabel(party.kind) }}）
                </option>
              </select>
              <span class="help">本方档案默认带出，可切换；模板里的甲方名称、证件号与甲方公章按此档案取用。</span>
            </div>
            <PartyBPicker
              v-model="partyBID"
              :parties="allParties"
              :fallback="detail?.party_b ?? null"
              @created="onPartyCreated"
            />
            <AssetPicker
              v-model="assetID"
              :assets="allAssets"
              :fallback="detail?.asset ?? null"
              @created="onAssetCreated"
            />
            <ContractFieldControl
              v-for="field in contractFields"
              :key="field.key"
              :field="field"
              :model-value="values[field.key] ?? ''"
              :readonly="mode === 'edit' && field.key === 'contract_no'"
              @update:model-value="setFieldValue(field.key, $event)"
            />
          </div>

          <ContractGenerationPanel
            v-if="mode === 'edit' && contract && detail"
            :contract="contract"
            :detail="detail"
            @ready="onGenerationReady"
          />
          <div v-if="mode === 'edit'" id="contractHistory" class="audit-host">
            <ContractAuditHistory :contract-id="contractId" />
          </div>
        </div>
        <div class="modal-foot">
          <button v-if="mode === 'edit'" type="button" @click="renew">续签</button>
          <button type="button" @click="emit('close')">取消</button>
          <button v-if="mode === 'edit'" type="button" class="danger" @click="remove">删除</button>
          <button type="button" class="primary" :disabled="saving" @click="submit">保存</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

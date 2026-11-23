<script setup lang="ts">
// 合同新建/编辑弹窗（阶段 1 迁移）：DOM 结构与存量 app.js openContractForm /
// openContractCreateForm 一致（modal-backdrop / modal wide / form-grid /
// renewal-banner / audit-host 类名不变），含动态字段、相对方 datalist、
// 续签草稿与来源横幅、删除确认、422 逐字段错误。
//
// 跨域：扫码签名与签名摘要已迁到 Vue——摘要由 components/signatures 的
// SignatureSummary 在本组件内渲染，扫码签名对话框由合同台账岛打开。
// 「生成 PDF」面板与批量生成对话框已迁到 Vue
// （ContractGenerationPanel / BatchGenerateDialog），「已关联公章」摘要用
// 公章域的 SealSummary 渲染。
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from "vue";
import { ApiError } from "../../api/client";
import {
  createContract,
  deleteContract as deleteContractRequest,
  getContract,
  updateContract,
} from "../../api/endpoints";
import type {
  ApiFieldError,
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
import ContractAuditHistory from "./ContractAuditHistory.vue";
import ContractFieldControl from "./ContractFieldControl.vue";
import ContractGenerationPanel from "./ContractGenerationPanel.vue";
import { isAgentActor } from "../../ui/format";
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

const partyBID = computed(() => {
  if (props.mode === "edit") {
    return props.detail?.party_b?.id ?? props.contract?.party_b_id ?? "";
  }
  if (props.renewal) {
    return props.renewal.input.party_b_id ?? "";
  }
  return props.partyB?.id ?? "";
});

const partyBName = computed(() => {
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

syncContractValues(values, props.fields, seed.value, partyBName.value, props.currentYear);

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
    partyBID.value,
    partyBName.value,
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

          <datalist id="partyNameList">
            <option v-for="party in parties" :key="party.id" :value="party.name"></option>
          </datalist>

          <div class="form-grid">
            <ContractFieldControl
              v-for="field in fields"
              :key="field.key"
              :field="field"
              :model-value="values[field.key] ?? ''"
              :readonly="mode === 'edit' && field.key === 'contract_no'"
              @update:model-value="values[field.key] = $event"
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

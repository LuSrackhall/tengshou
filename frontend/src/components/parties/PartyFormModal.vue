<script setup lang="ts">
// 相对方新建/编辑弹窗：DOM 结构与存量 app.js openPartyForm 一致
// （toolbar 之外的 modal-backdrop / modal / form-grid / audit-host 类名不变），
// 含动态字段控件、删除确认、历史合同与审计历史区块、422 逐字段错误。
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { ApiError } from "../../api/client";
import { createParty, deleteParty as deletePartyRequest, updateParty } from "../../api/endpoints";
import type { ApiFieldError, Contract, FieldDefinition, Party, PartyInput } from "../../api/types";
import {
  errorMessage,
  refreshAfterWrite,
  subscribeChange,
  toast,
  type AppChangeNotice,
} from "../../bridge/app";
import PartyAuditHistory from "./PartyAuditHistory.vue";
import PartyAssetsPanel from "./PartyAssetsPanel.vue";
import PartyFieldControl from "./PartyFieldControl.vue";
import PartyHistoryList from "./PartyHistoryList.vue";
import { isAgentActor } from "../../ui/format";
import { collectPartyInput, syncControlValues } from "./form";

const props = defineProps<{
  party: Party | null;
  fields: FieldDefinition[];
}>();

const emit = defineEmits<{
  close: [];
  "open-contract": [contract: Contract, renew: boolean];
}>();

const values = reactive<Record<string, string>>({});
const saving = ref(false);
const fieldErrors = ref<ApiFieldError[]>([]);
const errorText = ref("");
const bodyEl = ref<HTMLDivElement | null>(null);
const localParty = ref<Party | null>(props.party ? { ...props.party } : null);
let unsubscribe: (() => void) | null = null;

const title = computed(() =>
  props.party ? `编辑相对方 · ${props.party.name}` : "新建相对方",
);

// v0.2：新建档案时证件号必填（服务端同样校验）；存量档案允许为空、可稍后补录。
const displayFields = computed(() =>
  props.fields.map((field) =>
    !props.party && field.key === "id_number" ? { ...field, required: true } : field,
  ),
);

function onAssetsUpdated(updated: Party): void {
  localParty.value = updated;
}

syncControlValues(values, props.fields, props.party);
watch(
  () => props.fields,
  () => syncControlValues(values, props.fields, props.party),
);

function setValue(key: string, value: string): void {
  values[key] = value;
}

function onKeydown(event: KeyboardEvent): void {
  if (event.key === "Escape") {
    emit("close");
  }
}

// 外部写入本档案时提示核对；审计区块负责自身刷新。
function onChange(notice: AppChangeNotice): void {
  if (!props.party) {
    return;
  }
  if (
    notice.entity === "party" &&
    notice.entity_id === props.party.id &&
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
  try {
    const input: PartyInput = collectPartyInput(props.fields, values);
    const result = props.party
      ? await updateParty(props.party.id, input)
      : await createParty(input);
    emit("close");
    toast("相对方档案已保存");
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
  const party = props.party;
  if (!party) {
    return;
  }
  if (!window.confirm(`确定删除相对方「${party.name}」吗？`)) {
    return;
  }
  try {
    await deletePartyRequest(party.id);
    emit("close");
    toast("相对方已删除");
    await refreshAfterWrite();
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

function onOpenContract(contract: Contract, renew: boolean): void {
  emit("open-contract", contract, renew);
}

onMounted(() => {
  document.addEventListener("keydown", onKeydown);
  unsubscribe = subscribeChange(onChange);
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
      <div class="modal" role="dialog" aria-modal="true">
        <div class="modal-head">
          <h2>{{ title }}</h2>
          <button type="button" class="close" aria-label="关闭" @click="emit('close')">×</button>
        </div>
        <div ref="bodyEl" class="modal-body">
          <div id="partyErrors">
            <div v-if="fieldErrors.length" class="form-errors">
              请修正以下问题：<ul>
                <li v-for="problem in fieldErrors" :key="problem.field">
                  {{ problem.label || problem.field }}：{{ problem.message }}
                </li>
              </ul>
            </div>
            <div v-else-if="errorText" class="form-errors">{{ errorText }}</div>
          </div>
          <div class="form-grid">
            <PartyFieldControl
              v-for="field in displayFields"
              :key="field.key"
              :field="field"
              :model-value="values[field.key] ?? ''"
              @update:model-value="setValue(field.key, $event)"
            />
          </div>
          <template v-if="party && localParty">
            <PartyAssetsPanel :party="localParty" @updated="onAssetsUpdated" />
            <div class="audit-host">
              <PartyHistoryList :party-id="party.id" @open-contract="onOpenContract" />
            </div>
            <div class="audit-host">
              <PartyAuditHistory :party-id="party.id" />
            </div>
          </template>
        </div>
        <div class="modal-foot">
          <button type="button" @click="emit('close')">取消</button>
          <button v-if="party" type="button" class="danger" @click="remove">删除</button>
          <button type="button" class="primary" :disabled="saving" @click="submit">保存</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
// 相对方档案（阶段 1 迁移）：工具栏搜索、台账表格、行操作与新建/编辑弹窗。
// 结构与 app.js renderParties 的输出一致（toolbar / summary / data-table /
// actions 类名不变），数据走 frontend/src/api 的 typed 端点。
//
// 实时刷新：shell/realtime.ts 持有 SSE/轮询调度，收到变更后派发
// tengshou:data-changed；本组件据此重取字段与档案（外部写入数秒内可见）。
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { deleteParty as deletePartyRequest, listFields, listParties } from "../../api/endpoints";
import type { Contract, FieldDefinition, Party } from "../../api/types";
import {
  errorMessage,
  openContractFormForHistory,
  openContractFormForParty,
  refreshAfterWrite,
  subscribeDataChanged,
  toast,
} from "../../bridge/app";
import PartyFormModal from "./PartyFormModal.vue";
import { partyFieldValue } from "./format";

const fields = ref<FieldDefinition[]>([]);
const parties = ref<Party[]>([]);
const total = ref(0);
const activeQuery = ref("");
const search = ref("");
const loaded = ref(false);
const formOpen = ref(false);
const editingParty = ref<Party | null>(null);

let loadSeq = 0;
let reloadTimer: ReturnType<typeof setTimeout> | undefined;
let unsubscribe: (() => void) | null = null;

const tableFields = computed(() => fields.value.filter((field) => field.in_ledger));
const summary = computed(
  () =>
    `共 ${total.value} 个相对方` +
    (activeQuery.value ? ` · 搜索「${activeQuery.value}」` : ""),
);

async function fetchData(): Promise<void> {
  const seq = ++loadSeq;
  const query = search.value.trim();
  try {
    const [fieldData, partyData] = await Promise.all([
      listFields({ archived: "1" }),
      listParties(query ? { limit: 500, q: query } : { limit: 500 }),
    ]);
    if (seq !== loadSeq) {
      return;
    }
    fields.value = (fieldData.party ?? []).filter((field) => !field.archived);
    parties.value = partyData.items ?? [];
    total.value = partyData.total ?? 0;
    activeQuery.value = query;
    loaded.value = true;
  } catch (error) {
    if (seq !== loadSeq) {
      return;
    }
    loaded.value = true;
    toast(errorMessage(error), true);
  }
}

function scheduleFetch(delay: number): void {
  if (reloadTimer !== undefined) {
    clearTimeout(reloadTimer);
  }
  reloadTimer = setTimeout(() => {
    reloadTimer = undefined;
    void fetchData();
  }, delay);
}

// 搜索输入与外部变更事件共用同一个防抖计时器：150ms 内相继触发时只取一次数。
watch(search, () => {
  scheduleFetch(250);
});

function openCreate(): void {
  editingParty.value = null;
  formOpen.value = true;
}

function openEdit(party: Party): void {
  editingParty.value = party;
  formOpen.value = true;
}

function closeForm(): void {
  formOpen.value = false;
}

function newContract(party: Party): void {
  openContractFormForParty(party);
}

async function remove(party: Party): Promise<void> {
  if (!window.confirm(`确定删除相对方「${party.name}」吗？`)) {
    return;
  }
  try {
    await deletePartyRequest(party.id);
    toast("相对方已删除");
    await refreshAfterWrite();
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

// 历史合同的查看/续签由合同域的 Vue 表单承接：先关弹窗再跨域调用。
function onOpenContract(contract: Contract, renew: boolean): void {
  closeForm();
  openContractFormForHistory(contract, renew);
}

onMounted(() => {
  void fetchData();
  unsubscribe = subscribeDataChanged((detail) => {
    const entities = detail.entities;
    if (
      entities.length === 0 ||
      entities.includes("party") ||
      entities.includes("field")
    ) {
      scheduleFetch(150);
    }
  });
});

onBeforeUnmount(() => {
  unsubscribe?.();
  unsubscribe = null;
  if (reloadTimer !== undefined) {
    clearTimeout(reloadTimer);
    reloadTimer = undefined;
  }
});
</script>

<template>
  <div class="toolbar">
    <input
      id="partySearch"
      v-model="search"
      type="search"
      placeholder="搜索名称 / 证件号 / 电话"
      autocomplete="off"
    >
    <span class="spacer"></span>
    <button type="button" id="newPartyBtn" class="primary" @click="openCreate">新建相对方</button>
  </div>
  <div id="partySummary" class="summary">{{ loaded ? summary : "" }}</div>
  <div class="table-wrap">
    <table id="partyTable" class="data-table">
      <template v-if="loaded">
        <template v-if="!tableFields.length">
          <tbody>
            <tr><td class="empty">没有可显示的档案字段，请先在「字段」页新增。</td></tr>
          </tbody>
        </template>
        <template v-else>
          <thead>
            <tr>
              <th v-for="field in tableFields" :key="field.key">{{ field.label }}</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="party in parties" :key="party.id" @click="openEdit(party)">
              <td
                v-for="field in tableFields"
                :key="field.key"
                :class="{ wrap: field.key === 'note' || field.key === 'address' }"
              >{{ partyFieldValue(party, field) }}</td>
              <td>
                <div class="actions">
                  <button type="button" @click.stop="newContract(party)">以此档案新建合同</button>
                  <button type="button" @click.stop="openEdit(party)">编辑</button>
                  <button type="button" class="danger" @click.stop="remove(party)">删除</button>
                </div>
              </td>
            </tr>
            <tr v-if="!parties.length">
              <td class="empty" :colspan="tableFields.length + 1">还没有相对方档案。</td>
            </tr>
          </tbody>
        </template>
      </template>
    </table>
  </div>
  <PartyFormModal
    v-if="formOpen"
    :party="editingParty"
    :fields="fields"
    @close="closeForm"
    @open-contract="onOpenContract"
  />
</template>

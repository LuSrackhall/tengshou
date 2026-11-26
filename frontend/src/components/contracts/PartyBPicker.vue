<script setup lang="ts">
// 乙方（租户）选择器：从租户档案搜索/最近使用中选择，选中即带出姓名、
// 证件号与电话；没有档案时可在弹窗内快速建档（证件号必填，与档案页一致）。
// 证件号判重与档案页规则一致：重复只提示，不阻断（判重以证件号为准）。
import { computed, ref } from "vue";
import { createParty } from "../../api/endpoints";
import type { Party } from "../../api/types";
import { errorMessage, refreshAfterWrite, toast } from "../../bridge/app";
import { kindLabel } from "../../ui/format";

const RECENT_KEY = "tengshou:recent-party-b";
const RECENT_LIMIT = 5;

const props = defineProps<{
  modelValue: string;
  parties: Party[];
  fallback?: Party | null;
}>();

const emit = defineEmits<{
  "update:modelValue": [value: string];
  created: [party: Party];
}>();

const query = ref("");
const open = ref(false);
const creating = ref(false);
const saving = ref(false);
const quickName = ref("");
const quickIDNumber = ref("");
const quickPhone = ref("");
const createdParties = ref<Party[]>([]);

const allParties = computed(() => {
  const seen = new Set<string>();
  const merged: Party[] = [];
  for (const party of [
    ...props.parties,
    props.fallback,
    ...createdParties.value,
  ]) {
    if (party && !seen.has(party.id)) {
      seen.add(party.id);
      merged.push(party);
    }
  }
  return merged;
});

const selected = computed(
  () => allParties.value.find((party) => party.id === props.modelValue) ?? null,
);

function readRecent(): string[] {
  try {
    const raw = JSON.parse(window.localStorage.getItem(RECENT_KEY) ?? "[]");
    return Array.isArray(raw) ? raw.filter((id): id is string => typeof id === "string") : [];
  } catch {
    return [];
  }
}

function rememberRecent(id: string): void {
  const ids = [id, ...readRecent().filter((item) => item !== id)].slice(0, RECENT_LIMIT);
  try {
    window.localStorage.setItem(RECENT_KEY, JSON.stringify(ids));
  } catch {
    /* 隐私模式禁用 localStorage 时仍可选择，只是不记最近 */
  }
}

const recentParties = computed(() => {
  const byID = new Map(allParties.value.map((party) => [party.id, party]));
  return readRecent()
    .map((id) => byID.get(id))
    .filter((party): party is Party => Boolean(party));
});

const filtered = computed(() => {
  const keyword = query.value.trim().toLowerCase();
  if (!keyword) {
    return allParties.value.slice(0, 20);
  }
  return allParties.value
    .filter((party) =>
      [party.name, party.id_number ?? "", party.phone ?? ""]
        .some((value) => value.toLowerCase().includes(keyword)),
    )
    .slice(0, 20);
});

const duplicate = computed(() => {
  const party = selected.value;
  const idNumber = (party?.id_number ?? "").trim();
  if (!party || !idNumber) {
    return null;
  }
  const other = allParties.value.find(
    (item) => item.id !== party.id && (item.id_number ?? "").trim() === idNumber,
  );
  return other ?? null;
});

function choose(party: Party): void {
  emit("update:modelValue", party.id);
  rememberRecent(party.id);
  open.value = false;
  query.value = "";
}

function clear(): void {
  emit("update:modelValue", "");
  open.value = true;
}

async function submitQuick(): Promise<void> {
  if (saving.value) {
    return;
  }
  const name = quickName.value.trim();
  const idNumber = quickIDNumber.value.trim();
  if (!name) {
    toast("请填写租户姓名", true);
    return;
  }
  if (!idNumber) {
    toast("证件号必填：与档案页规则一致，新建租户档案必须填写证件号", true);
    return;
  }
  saving.value = true;
  try {
    const result = await createParty({
      kind: "person",
      name,
      id_number: idNumber,
      phone: quickPhone.value.trim(),
    });
    createdParties.value = [result.item, ...createdParties.value];
    choose(result.item);
    // 一键带入姓名证件号后立即选中，后续合同保存无需重复录入。
    toast("租户档案已新建并选中");
    const warnings = result.warnings ?? [];
    if (warnings.length) {
      window.setTimeout(() => toast(warnings.join("；"), true), 350);
    }
    creating.value = false;
    quickName.value = "";
    quickIDNumber.value = "";
    quickPhone.value = "";
    emit("created", result.item);
    await refreshAfterWrite();
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <div class="field picker-field" data-key="party_b">
    <label for="f_contract_party_b">乙方（租户）<span class="req">*</span></label>

    <template v-if="selected">
      <div class="picker-selected">
        <div class="picker-selected-head">
          <strong>{{ selected.name }}</strong>
          <span class="badge">{{ kindLabel(selected.kind) }}</span>
          <button type="button" class="link" @click="clear">更换</button>
        </div>
        <span class="help">
          证件号：{{ selected.id_number || "未补录" }}
          <template v-if="selected.phone"> · 电话：{{ selected.phone }}</template>
        </span>
        <span v-if="!selected.id_number" class="help warn">
          该档案还没有证件号，请在「相对方档案」页补录一次，补录后长期有效。
        </span>
        <span v-if="duplicate" class="help warn">
          证件号 {{ selected.id_number }} 同时存在于档案「{{ duplicate.name }}」，
          判重以证件号为准，请确认选择正确。
        </span>
      </div>
    </template>

    <template v-else>
      <input
        id="f_contract_party_b"
        v-model="query"
        type="search"
        placeholder="搜索姓名 / 证件号 / 电话，或选择最近使用"
        autocomplete="off"
        @focus="open = true"
      >
      <ul v-if="open" id="partyBPickerList" class="picker-list">
        <template v-if="!query && recentParties.length">
          <li class="picker-caption">最近使用</li>
          <li v-for="party in recentParties" :key="`recent-${party.id}`">
            <button type="button" :data-party-id="party.id" @click="choose(party)">
              <span>{{ party.name }}</span>
              <span class="picker-meta">{{ kindLabel(party.kind) }} · {{ party.id_number || "无证件号" }}</span>
            </button>
          </li>
          <li class="picker-caption">全部档案</li>
        </template>
        <li v-for="party in filtered" :key="party.id">
          <button type="button" :data-party-id="party.id" @click="choose(party)">
            <span>{{ party.name }}</span>
            <span class="picker-meta">{{ kindLabel(party.kind) }} · {{ party.id_number || "无证件号" }}</span>
          </button>
        </li>
        <li v-if="!filtered.length" class="picker-empty">没有匹配的租户档案</li>
      </ul>
      <div class="picker-actions">
        <button type="button" id="quickCreatePartyBtn" class="link" @click="creating = !creating">
          {{ creating ? "收起快速新建" : "快速新建租户档案" }}
        </button>
      </div>
      <div v-if="creating" class="picker-create">
        <input id="quickPartyName" v-model="quickName" type="text" placeholder="租户姓名（必填）" autocomplete="off">
        <input id="quickPartyIDNumber" v-model="quickIDNumber" type="text" placeholder="证件号（必填）" autocomplete="off">
        <input id="quickPartyPhone" v-model="quickPhone" type="text" placeholder="电话（选填）" autocomplete="off">
        <button type="button" class="primary" :disabled="saving" @click="submitQuick">建档并选中</button>
      </div>
    </template>

    <span class="help">从租户档案选择，选中即带出档案信息，不需要重复手输。</span>
  </div>
</template>

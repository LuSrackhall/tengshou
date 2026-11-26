<script setup lang="ts">
// 租赁房产选择器：从房产档案搜索/选择，显示空置/在租与启用状态；
// 没有档案时可快速新建（名称必填）。在租/空置由后端按生效合同派生。
import { computed, ref } from "vue";
import { createAsset } from "../../api/endpoints";
import type { Asset } from "../../api/types";
import { errorMessage, refreshAfterWrite, toast } from "../../bridge/app";
import { leaseStatusLabel } from "../assets/format";

const props = defineProps<{
  modelValue: string;
  assets: Asset[];
  fallback?: Asset | null;
}>();

const emit = defineEmits<{
  "update:modelValue": [value: string];
  created: [asset: Asset];
}>();

const query = ref("");
const open = ref(false);
const creating = ref(false);
const saving = ref(false);
const quickName = ref("");
const quickAddress = ref("");
const quickRoomNo = ref("");
const createdAssets = ref<Asset[]>([]);

const allAssets = computed(() => {
  const seen = new Set<string>();
  const merged: Asset[] = [];
  for (const asset of [...props.assets, props.fallback, ...createdAssets.value]) {
    if (asset && !seen.has(asset.id)) {
      seen.add(asset.id);
      merged.push(asset);
    }
  }
  return merged;
});

const selected = computed(
  () => allAssets.value.find((asset) => asset.id === props.modelValue) ?? null,
);

const filtered = computed(() => {
  const keyword = query.value.trim().toLowerCase();
  if (!keyword) {
    return allAssets.value.slice(0, 20);
  }
  return allAssets.value
    .filter((asset) =>
      [asset.name, asset.address ?? "", asset.room_no ?? ""]
        .some((value) => value.toLowerCase().includes(keyword)),
    )
    .slice(0, 20);
});

function describe(asset: Asset): string {
  const parts = [leaseStatusLabel(asset.lease_status)];
  if (asset.address) {
    parts.push(asset.address);
  }
  if (asset.room_no) {
    parts.push(asset.room_no);
  }
  if (!asset.enabled) {
    parts.push("已停用");
  }
  return parts.join(" · ");
}

function choose(asset: Asset): void {
  emit("update:modelValue", asset.id);
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
  if (!name) {
    toast("请填写房产名称", true);
    return;
  }
  saving.value = true;
  try {
    const result = await createAsset({
      asset_type: "house",
      name,
      address: quickAddress.value.trim(),
      room_no: quickRoomNo.value.trim(),
    });
    createdAssets.value = [result.item, ...createdAssets.value];
    choose(result.item);
    toast("房产档案已新建并选中");
    const warnings = result.warnings ?? [];
    if (warnings.length) {
      window.setTimeout(() => toast(warnings.join("；"), true), 350);
    }
    creating.value = false;
    quickName.value = "";
    quickAddress.value = "";
    quickRoomNo.value = "";
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
  <div class="field picker-field" data-key="asset">
    <label for="f_contract_asset">租赁房产</label>

    <template v-if="selected">
      <div class="picker-selected">
        <div class="picker-selected-head">
          <strong>{{ selected.name }}</strong>
          <span class="badge" :class="selected.lease_status === 'rented' ? 'active' : 'archived'">
            {{ leaseStatusLabel(selected.lease_status) }}
          </span>
          <button type="button" class="link" @click="clear">更换</button>
        </div>
        <span class="help">{{ describe(selected) }}</span>
        <span v-if="selected.lease_status === 'rented'" class="help warn">
          该房产当前在租（存在生效中的合同），请确认新租期不与现有合同重叠。
        </span>
      </div>
    </template>

    <template v-else>
      <input
        id="f_contract_asset"
        v-model="query"
        type="search"
        placeholder="搜索房产名称 / 地址 / 房号"
        autocomplete="off"
        @focus="open = true"
      >
      <ul v-if="open" id="assetPickerList" class="picker-list">
        <li v-for="asset in filtered" :key="asset.id">
          <button type="button" :data-asset-id="asset.id" @click="choose(asset)">
            <span>{{ asset.name }}</span>
            <span class="picker-meta">{{ describe(asset) }}</span>
          </button>
        </li>
        <li v-if="!filtered.length" class="picker-empty">
          没有匹配的房产档案
        </li>
      </ul>
      <div class="picker-actions">
        <button type="button" id="quickCreateAssetBtn" class="link" @click="creating = !creating">
          {{ creating ? "收起快速新建" : "快速新建房产档案" }}
        </button>
      </div>
      <div v-if="creating" class="picker-create">
        <input id="quickAssetName" v-model="quickName" type="text" placeholder="房产名称（必填）" autocomplete="off">
        <input id="quickAssetAddress" v-model="quickAddress" type="text" placeholder="地址（选填）" autocomplete="off">
        <input id="quickAssetRoomNo" v-model="quickRoomNo" type="text" placeholder="房号（选填）" autocomplete="off">
        <button type="button" class="primary" :disabled="saving" @click="submitQuick">建档并选中</button>
      </div>
    </template>

    <span class="help">空闲/在租由「生效中且未终止的合同」自动派生；历史合同可不选房产。</span>
  </div>
</template>

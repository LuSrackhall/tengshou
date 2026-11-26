<script setup lang="ts">
// 房产档案（te-to7tr）：工具栏搜索 / 启用状态筛选、台账表格、新建/编辑与
// 行内停用/启用。在租/空置由后端按「存在生效中且未终止的合同」派生
// （te-thrth 接入合同引用），本页只读展示。
//
// 实时刷新：shell/realtime.ts 收到变更后派发 tengshou:data-changed，本组件
// 据此重取，外部（后续 MCP/agent）写入数秒内可见。
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { listAssets, updateAsset } from "../../api/endpoints";
import type { Asset, AssetInput, AssetStatusFilter, AssetsQuery } from "../../api/types";
import {
  errorMessage,
  refreshAfterWrite,
  subscribeDataChanged,
  toast,
} from "../../bridge/app";
import AssetFormModal from "./AssetFormModal.vue";
import { toggleAssetInput } from "./form";
import {
  areaLabel,
  enabledLabel,
  hasActiveFilter,
  leaseStatusLabel,
  optionalText,
} from "./format";

const assets = ref<Asset[]>([]);
const total = ref(0);
const activeQuery = ref("");
const search = ref("");
const status = ref<AssetStatusFilter | "">("");
const loaded = ref(false);
const formOpen = ref(false);
const editingAsset = ref<Asset | null>(null);

let loadSeq = 0;
let reloadTimer: ReturnType<typeof setTimeout> | undefined;
let unsubscribe: (() => void) | null = null;

const summary = computed(() => {
  const rented = assets.value.filter((asset) => asset.lease_status === "rented").length;
  const vacant = assets.value.filter((asset) => asset.lease_status === "vacant").length;
  const parts = [`共 ${total.value} 套房产`, `在租 ${rented}`, `空置 ${vacant}`];
  if (activeQuery.value) {
    parts.push(`搜索「${activeQuery.value}」`);
  }
  if (status.value === "enabled") {
    parts.push("仅启用");
  } else if (status.value === "disabled") {
    parts.push("仅停用");
  }
  return parts.join(" · ");
});

const emptyText = computed(() =>
  hasActiveFilter(activeQuery.value, status.value)
    ? "没有符合条件的房产档案。"
    : "还没有房产档案。点击「新建房产」建立第一套。",
);

async function fetchData(): Promise<void> {
  const seq = ++loadSeq;
  const query: AssetsQuery = { limit: 500 };
  const keyword = search.value.trim();
  if (keyword) {
    query.q = keyword;
  }
  if (status.value !== "") {
    query.status = status.value;
  }
  try {
    const data = await listAssets(query);
    if (seq !== loadSeq) {
      return;
    }
    assets.value = data.items ?? [];
    total.value = data.total ?? assets.value.length;
    activeQuery.value = keyword;
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

watch([search, status], () => {
  scheduleFetch(250);
});

function openCreate(): void {
  editingAsset.value = null;
  formOpen.value = true;
}

function openEdit(asset: Asset): void {
  editingAsset.value = asset;
  formOpen.value = true;
}

function closeForm(): void {
  formOpen.value = false;
}

async function toggleEnabled(asset: Asset): Promise<void> {
  const next = !asset.enabled;
  const verb = next ? "启用" : "停用";
  const tail = next ? "" : "停用后档案继续保留，可随时重新启用。";
  if (!window.confirm(`确定${verb}房产「${asset.name}」吗？${tail}`)) {
    return;
  }
  const input: AssetInput = toggleAssetInput(asset, next);
  try {
    await updateAsset(asset.id, input);
    toast(`房产已${verb}`);
    await refreshAfterWrite();
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

onMounted(() => {
  void fetchData();
  unsubscribe = subscribeDataChanged((detail) => {
    const entities = detail.entities;
    if (entities.length === 0 || entities.includes("asset")) {
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
      id="assetSearch"
      v-model="search"
      type="search"
      placeholder="搜索名称 / 地址 / 房号"
      autocomplete="off"
    >
    <select id="assetStatusFilter" v-model="status" aria-label="启用状态">
      <option value="">全部状态</option>
      <option value="enabled">启用</option>
      <option value="disabled">停用</option>
    </select>
    <span class="spacer"></span>
    <button type="button" id="newAssetBtn" class="primary" @click="openCreate">新建房产</button>
  </div>
  <div id="assetSummary" class="summary">{{ loaded ? summary : "" }}</div>
  <p class="hint">在租 / 空置由「生效中且未终止的合同」自动派生，不手工维护；合同到期不会自动释放房产，需显式归档/终止合同。</p>
  <div class="table-wrap">
    <table id="assetTable" class="data-table">
      <template v-if="loaded">
        <thead>
          <tr>
            <th>名称</th>
            <th>地址</th>
            <th>房号</th>
            <th>面积</th>
            <th>租赁状态</th>
            <th>状态</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="asset in assets" :key="asset.id" :data-id="asset.id" @click="openEdit(asset)">
            <td>{{ asset.name }}</td>
            <td class="wrap">{{ optionalText(asset.address) }}</td>
            <td>{{ optionalText(asset.room_no) }}</td>
            <td>{{ areaLabel(asset.area_sqm) }}</td>
            <td>
              <span class="badge" :class="asset.lease_status === 'rented' ? 'active' : 'archived'">
                {{ leaseStatusLabel(asset.lease_status) }}
              </span>
            </td>
            <td>
              <span class="badge" :class="asset.enabled ? 'active' : 'draft'">
                {{ enabledLabel(asset.enabled) }}
              </span>
            </td>
            <td>
              <div class="actions">
                <button type="button" @click.stop="openEdit(asset)">编辑</button>
                <button
                  type="button"
                  :class="asset.enabled ? 'danger' : ''"
                  @click.stop="toggleEnabled(asset)"
                >{{ asset.enabled ? "停用" : "启用" }}</button>
              </div>
            </td>
          </tr>
          <tr v-if="!assets.length">
            <td class="empty" colspan="7">{{ emptyText }}</td>
          </tr>
        </tbody>
      </template>
    </table>
  </div>
  <AssetFormModal v-if="formOpen" :asset="editingAsset" @close="closeForm" />
</template>

<script setup lang="ts">
// 公章（阶段 1 迁移）：搜索、卡片列表、导入/替换/删除、预览与关联合同。
// 结构与 app.js renderSeals 的输出一致（toolbar / hint / summary / seal-grid /
// seal-card 类名不变），数据走 frontend/src/api 的 typed 端点。
//
// 实时刷新：shell/realtime.ts 持有 SSE/轮询调度，收到变更后派发
// tengshou:data-changed；本组件在 entities 含 seal 或为空时重取（外部写入
// 数秒内可见）。写操作后调用 refreshAfterWrite()，由 bridge/app.ts 派发该
// 事件，本域与其它已迁移域随之刷新。
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  deleteSeal as deleteSealRequest,
  getSealContracts,
  listContracts,
  listParties,
  listSeals,
  replaceSealImage as replaceSealImageRequest,
  sealImageUrl,
} from "../../api/endpoints";
import type { Contract, Party, Seal } from "../../api/types";
import {
  errorMessage,
  refreshAfterWrite,
  subscribeDataChanged,
  toast,
} from "../../bridge/app";
import SealAttachModal from "./SealAttachModal.vue";
import SealImportModal from "./SealImportModal.vue";
import SealPreviewModal from "./SealPreviewModal.vue";
import { readFileBase64, sealOwner } from "./format";

/** 导入上限与后端/迁移前一致：12MB。 */
const IMPORT_MAX_BYTES = 12 * 1024 * 1024;

interface ImportPayload {
  file: File;
  /** 图片内容的 base64（不含 data: 前缀）。 */
  base64: string;
}

interface AttachPayload {
  seal: Seal;
  linkedIds: string[];
  contracts: Contract[];
  contractTotal: number;
}

const seals = ref<Seal[]>([]);
const parties = ref<Party[]>([]);
const total = ref(0);
const activeQuery = ref("");
const search = ref("");
const loaded = ref(false);

const importPayload = ref<ImportPayload | null>(null);
const previewSeal = ref<Seal | null>(null);
const attachPayload = ref<AttachPayload | null>(null);

const importInput = ref<HTMLInputElement | null>(null);
const replaceInput = ref<HTMLInputElement | null>(null);
let pendingReplace: Seal | null = null;

let loadSeq = 0;
let searchTimer: ReturnType<typeof setTimeout> | undefined;
let reloadTimer: ReturnType<typeof setTimeout> | undefined;
let unsubscribe: (() => void) | null = null;

const summary = computed(
  () =>
    `共 ${total.value} 枚公章图片` +
    (activeQuery.value ? ` · 搜索「${activeQuery.value}」` : ""),
);

function ownerOf(seal: Seal): string {
  return sealOwner(seal, parties.value);
}

async function fetchData(): Promise<void> {
  const seq = ++loadSeq;
  const query = search.value.trim();
  try {
    // 相对方只用于卡片/预览里的归属文案，取不到时不影响公章列表本身。
    const [sealData, partyData] = await Promise.all([
      listSeals(query ? { limit: 500, q: query } : { limit: 500 }),
      listParties({ limit: 500 }).catch(() => null),
    ]);
    if (seq !== loadSeq) {
      return;
    }
    seals.value = sealData.items ?? [];
    total.value = sealData.total ?? 0;
    if (partyData) {
      parties.value = partyData.items ?? [];
    }
    activeQuery.value = query;
    loaded.value = true;
  } catch (error) {
    if (seq !== loadSeq) {
      return;
    }
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

watch(search, () => {
  if (searchTimer !== undefined) {
    clearTimeout(searchTimer);
  }
  searchTimer = setTimeout(() => {
    searchTimer = undefined;
    void fetchData();
  }, 250);
});

function triggerImport(): void {
  const input = importInput.value;
  if (!input) {
    return;
  }
  input.value = "";
  input.click();
}

async function onImportFile(event: Event): Promise<void> {
  const file = (event.target as HTMLInputElement).files?.[0];
  if (!file) {
    return;
  }
  if (file.size > IMPORT_MAX_BYTES) {
    toast("公章图片文件过大，请压缩到 12MB 以内", true);
    return;
  }
  let base64: string;
  try {
    base64 = await readFileBase64(file);
  } catch (error) {
    toast(errorMessage(error), true);
    return;
  }
  importPayload.value = { file, base64 };
}

function openPreview(seal: Seal): void {
  previewSeal.value = seal;
}

async function openAttach(seal: Seal): Promise<void> {
  try {
    const [linked, contracts] = await Promise.all([
      getSealContracts(seal.id),
      listContracts({ limit: 500 }),
    ]);
    const items = contracts.items ?? [];
    attachPayload.value = {
      seal,
      linkedIds: linked.contract_ids ?? [],
      contracts: items,
      contractTotal: contracts.total || items.length,
    };
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

function replaceImage(seal: Seal): void {
  if (
    !window.confirm(
      `确定替换公章图片「${seal.name}」吗？替换后生成的新 PDF 使用新图片，历史文件不受影响；替换会记入变更记录。`,
    )
  ) {
    return;
  }
  const input = replaceInput.value;
  if (!input) {
    return;
  }
  pendingReplace = seal;
  input.value = "";
  input.click();
}

async function onReplaceFile(event: Event): Promise<void> {
  const file = (event.target as HTMLInputElement).files?.[0];
  const seal = pendingReplace;
  pendingReplace = null;
  if (!file || !seal) {
    return;
  }
  let base64: string;
  try {
    base64 = await readFileBase64(file);
  } catch (error) {
    toast(errorMessage(error), true);
    return;
  }
  try {
    await replaceSealImageRequest(seal.id, { file_base64: base64 });
    toast("公章图片已替换");
    await refreshAfterWrite();
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

async function remove(seal: Seal): Promise<void> {
  const linked = seal.contract_count || 0;
  const extra = linked ? `，并解除 ${linked} 份合同的关联` : "";
  if (!window.confirm(`确定删除公章图片「${seal.name}」吗${extra}？删除会记入变更记录。`)) {
    return;
  }
  try {
    await deleteSealRequest(seal.id);
    toast("公章图片已删除");
    await refreshAfterWrite();
  } catch (error) {
    toast(errorMessage(error), true);
  }
}

onMounted(() => {
  void fetchData();
  unsubscribe = subscribeDataChanged((detail) => {
    const entities = detail.entities;
    if (entities.length === 0 || entities.includes("seal")) {
      scheduleFetch(150);
    }
  });
});

onBeforeUnmount(() => {
  unsubscribe?.();
  unsubscribe = null;
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
    <input
      id="sealSearch"
      v-model="search"
      type="search"
      placeholder="搜索公章名称 / 说明"
      autocomplete="off"
    >
    <span class="spacer"></span>
    <button type="button" id="importSealBtn" class="primary" @click="triggerImport">导入公章图片</button>
  </div>
  <p class="hint">公章以图片文件保存：记录文件路径与 SHA-256 摘要，不塞进数据库单元格。可关联到单位与多份合同；模板中的盖章占位决定它在 PDF 中的位置与大小。</p>
  <div id="sealSummary" class="summary">{{ loaded ? summary : "" }}</div>
  <div id="sealGrid" class="seal-grid">
    <div
      v-for="seal in seals"
      :key="seal.id"
      class="seal-card"
      :data-id="seal.id"
      @click="openPreview(seal)"
    >
      <div class="seal-thumb">
        <img :key="seal.sha256" :src="sealImageUrl(seal.id)" :alt="seal.name" loading="lazy">
      </div>
      <div class="seal-card-body">
        <div class="seal-name">{{ seal.name }}</div>
        <div class="seal-meta">{{ ownerOf(seal) }} · {{ seal.width_px }}×{{ seal.height_px }}px</div>
        <div class="seal-meta mono">sha256 {{ seal.sha256.slice(0, 12) }}…</div>
        <div class="seal-meta">已关联 {{ seal.contract_count || 0 }} 份合同</div>
        <div class="seal-actions">
          <button type="button" data-action="preview" @click.stop="openPreview(seal)">预览</button>
          <button type="button" data-action="attach" @click.stop="openAttach(seal)">关联合同</button>
          <button type="button" data-action="replace" @click.stop="replaceImage(seal)">替换</button>
          <button type="button" class="danger" data-action="delete" @click.stop="remove(seal)">删除</button>
        </div>
      </div>
    </div>
    <div v-if="loaded && !seals.length" class="empty">还没有公章图片。点击「导入公章图片」上传 PNG / JPG。</div>
  </div>
  <input
    ref="importInput"
    type="file"
    accept="image/png,image/jpeg"
    hidden
    @change="onImportFile"
  >
  <input
    ref="replaceInput"
    type="file"
    accept="image/png,image/jpeg"
    hidden
    @change="onReplaceFile"
  >
  <SealImportModal
    v-if="importPayload"
    :file="importPayload.file"
    :base64="importPayload.base64"
    :parties="parties"
    @close="importPayload = null"
  />
  <SealPreviewModal
    v-if="previewSeal"
    :seal="previewSeal"
    :parties="parties"
    @close="previewSeal = null"
  />
  <SealAttachModal
    v-if="attachPayload"
    :seal="attachPayload.seal"
    :linked-ids="attachPayload.linkedIds"
    :contracts="attachPayload.contracts"
    :contract-total="attachPayload.contractTotal"
    @close="attachPayload = null"
  />
</template>

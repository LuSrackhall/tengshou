<script setup lang="ts">
// 扫码签名对话框（阶段 1 迁移）：签署人选择、会话创建、二维码与局域网地址
// 展示（多网卡切换）、2 秒轮询状态、过期/失效提示、签名完成后的内容版本
// 核对与整体刷新。会话生命周期（创建/轮询/取消/地址选择）由 useQrSession
// 共用；关闭对话框（含 Esc）时取消未完成的会话（DELETE）。
//
// v0.2 档案复用（D3=B）：签名人档案已有长期签名时可直接「使用租户档案签名」
// 套印到本合同，不再逐份确认，也不绑定合同内容版本；扫码手写保留为备选。
//
// 数据走 frontend/src/api 的 typed 端点；二维码继续用服务端生成的 PNG。
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import {
  cancelSignatureSession,
  createSignatureSession,
  getContract,
  getSignatureSession,
  reusePartySignature,
  signatureSessionQrUrl,
} from "../../api/endpoints";
import type {
  Contract,
  ContractDetail,
  SignatureSession,
} from "../../api/types";
import { errorMessage, refreshAfterWrite, toast } from "../../bridge/app";
import { fmtDate } from "../../ui/format";
import SignatureSummary from "./SignatureSummary.vue";
import { useQrSession } from "./useQrSession";

const props = defineProps<{
  contract: Contract;
  detail: ContractDetail;
}>();

const emit = defineEmits<{ close: [] }>();

interface Signer {
  id: string;
  role: string;
  name: string;
}

// 与迁移前一致：乙方在前、甲方在后，默认选中第一项。
const signers: Signer[] = [];
if (props.detail.party_b) {
  signers.push({
    id: props.detail.party_b.id,
    role: "乙方",
    name: props.detail.party_b.name,
  });
}
if (props.detail.party_a) {
  signers.push({
    id: props.detail.party_a.id,
    role: "甲方",
    name: props.detail.party_a.name,
  });
}

const signerId = ref(signers[0]?.id ?? "");
const expiryText = ref("");
const reusing = ref(false);
const reuseMessage = ref("");
// 本地合同详情：档案复用或扫码完成后重取，签名摘要即时刷新。
const detail = ref<ContractDetail>(props.detail);

const {
  session,
  starting,
  startError,
  stateText,
  stateClass,
  qrIndex,
  urls,
  currentUrl,
  qrSrc,
  startSession,
} = useQrSession<SignatureSession>({
  create: async () =>
    (
      await createSignatureSession({
        contract_id: props.contract.id,
        party_id: signerId.value,
      })
    ).item,
  get: async (id) => (await getSignatureSession(id)).item,
  cancel: (id) => cancelSignatureSession(id),
  qrUrl: signatureSessionQrUrl,
  messages: {
    waiting: "等待手机扫码并提交签名…",
    lost: "签名会话已失效（可能服务已重启），请重新生成二维码。",
    expired: "二维码已过期，请重新生成。",
  },
  onStarted: (item) => {
    expiryText.value = expiryLabel(item.expires_at);
  },
  onCompleted: (item) => {
    expiryText.value = "";
    const version = item.content_sha256
      ? `（内容版本 ${item.content_sha256.slice(0, 12)}）`
      : "";
    const changed = item.content_changed
      ? "。注意：合同内容在生成二维码后已修改，这份签名对应旧内容版本"
      : "";
    const warn = Boolean(item.content_changed);
    toast(warn ? "签名已保存，但合同内容已变更，请核对" : "签名已保存到本机", warn);
    refreshAfterWrite().catch((error: unknown) => toast(errorMessage(error), true));
    void refreshDetail();
    return {
      text: `签名已保存：${item.signer_name} · ${fmtDate(item.completed_at)}${version}${changed}`,
      tone: warn ? "warn" : "ok",
    };
  },
});

const title = computed(() => `扫码签名 · ${props.contract.contract_no}`);
const startLabel = computed(() =>
  session.value ? "重新生成二维码" : "生成签名二维码",
);

/** 重新拉取合同详情，让签名摘要展示刚落库的签名记录。 */
async function refreshDetail(): Promise<void> {
  try {
    detail.value = await getContract(props.contract.id);
  } catch {
    /* transient：保留上一次详情，签名摘要仍可用 */
  }
}

/** 当前签名人档案；档案里有长期签名时可直接套印到本合同。 */
const selectedParty = computed(() => {
  if (detail.value.party_a?.id === signerId.value) {
    return detail.value.party_a;
  }
  if (detail.value.party_b?.id === signerId.value) {
    return detail.value.party_b;
  }
  return null;
});
const archiveSignatureAvailable = computed(
  () => Boolean(selectedParty.value?.signature_id),
);
/** 乙方档案即租户档案；甲方档案同样可复用，按钮文案按角色区分。 */
const reuseLabel = computed(() =>
  signers.find((signer) => signer.id === signerId.value)?.role === "甲方"
    ? "使用甲方档案签名"
    : "使用租户档案签名",
);

async function reuseArchiveSignature(): Promise<void> {
  const party = selectedParty.value;
  if (!party?.signature_id || reusing.value) {
    return;
  }
  reusing.value = true;
  reuseMessage.value = "";
  try {
    await reusePartySignature(props.contract.id, party.id);
    reuseMessage.value = "已使用档案签名（来源：档案复用，已记入变更记录），可以直接生成合同。";
    toast("已使用档案签名");
    await refreshAfterWrite();
    await refreshDetail();
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    reusing.value = false;
  }
}

function expiryLabel(expiresAt: string): string {
  const when = new Date(expiresAt);
  if (Number.isNaN(when.getTime())) {
    return "";
  }
  return `有效期至 ${when.toLocaleString("zh-CN", { hour12: false })}`;
}

function onKeydown(event: KeyboardEvent): void {
  if (event.key === "Escape") {
    emit("close");
  }
}

onMounted(() => {
  document.addEventListener("keydown", onKeydown);
});

onBeforeUnmount(() => {
  document.removeEventListener("keydown", onKeydown);
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
        <div class="modal-body">
          <div class="signature-panel">
            <div class="field">
              <label for="signParty">签名人</label>
              <select id="signParty" v-model="signerId">
                <option v-for="signer in signers" :key="signer.id" :value="signer.id">
                  {{ signer.role }} · {{ signer.name }}
                </option>
              </select>
              <span class="help">手机与电脑需连接同一局域网；二维码一次性有效，过期或提交后失效。手机打不开时，请确认连接的是同一 Wi-Fi（非访客网络），也可直接输入下方地址。</span>
            </div>
            <div v-if="archiveSignatureAvailable" class="field">
              <label>档案签名</label>
              <span class="help">该档案已采集长期签名，可直接套印到本合同（来源记为「档案复用」）；签署事实由你自行确认。</span>
              <button type="button" class="reuse-btn" :disabled="reusing" @click="reuseArchiveSignature">
                {{ reusing ? "正在套印…" : reuseLabel }}
              </button>
              <div v-if="reuseMessage" class="sign-state ok">{{ reuseMessage }}</div>
            </div>
            <SignatureSummary v-if="detail.signatures.length" :detail="detail" />
            <div id="signQrArea" class="sign-qr-empty">
              <template v-if="session">
                <div class="sign-qr">
                  <img
                    id="signQrImg"
                    :src="qrSrc"
                    alt="签名二维码"
                    width="300"
                    height="300"
                  >
                  <div class="sign-qr-meta">
                    <label v-if="urls.length > 1" class="inline sign-url-pick">电脑有多张网卡时请选择手机能访问的地址：
                      <select id="signUrlPick" v-model="qrIndex">
                        <option v-for="(url, index) in urls" :key="index" :value="index">{{ url }}</option>
                      </select>
                    </label>
                    <div id="signUrlText" class="sign-url mono">{{ currentUrl }}</div>
                    <div id="signState" class="sign-state" :class="stateClass">{{ stateText }}</div>
                    <div id="signExpiry" class="sign-expiry">{{ expiryText }}</div>
                  </div>
                </div>
              </template>
              <template v-else-if="starting"><div class="sign-qr-empty">正在生成二维码…</div></template>
              <template v-else-if="startError">
                <div class="sign-qr-empty error">{{ startError }}</div>
              </template>
              <template v-else>点击下方「生成签名二维码」，让签名人用手机扫码手写。</template>
            </div>
          </div>
        </div>
        <div class="modal-foot">
          <button type="button" @click="emit('close')">关闭</button>
          <button type="button" class="primary" :disabled="starting" @click="startSession">
            {{ startLabel }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

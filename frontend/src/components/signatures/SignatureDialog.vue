<script setup lang="ts">
// 扫码签名对话框（阶段 1 迁移）：签署人选择、会话创建、二维码与局域网地址
// 展示（多网卡切换）、2 秒轮询状态、过期/失效提示、签名完成后的内容版本
// 核对与整体刷新。行为同存量 app.js openSignatureDialog /
// startSignatureSession / renderSignExpiry / pollSignatureSession；关闭对话框
// （含 Esc）时停止轮询并取消未完成的会话（DELETE）。
//
// 数据走 frontend/src/api 的 typed 端点；二维码继续用服务端生成的 PNG。
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { ApiError } from "../../api/client";
import {
  cancelSignatureSession,
  createSignatureSession,
  getSignatureSession,
  signatureSessionQrUrl,
} from "../../api/endpoints";
import type {
  Contract,
  ContractDetail,
  SignatureSession,
} from "../../api/types";
import { errorMessage, refreshAfterWrite, toast } from "../../bridge/app";
import { fmtDate } from "../contracts/format";
import SignatureSummary from "./SignatureSummary.vue";

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
const session = ref<SignatureSession | null>(null);
const starting = ref(false);
const startLabel = ref("生成签名二维码");
const generating = ref(false);
const startError = ref("");
const stateText = ref("等待手机扫码并提交签名…");
const stateClass = ref("");
const expiryText = ref("");
/** 多网卡时用户选择的地址下标；null 表示还未手动选择。 */
const qrIndex = ref<number | null>(null);

let pollTimer: ReturnType<typeof setInterval> | null = null;
let activeId: string | null = null;
let closed = false;

const title = computed(() => `扫码签名 · ${props.contract.contract_no}`);
const urls = computed(() => session.value?.urls ?? []);
const currentUrl = computed(() => {
  const list = urls.value;
  return list[qrIndex.value ?? 0] || list[0] || "";
});
const qrSrc = computed(() => {
  const item = session.value;
  if (!item) {
    return "";
  }
  return qrIndex.value === null
    ? item.qr_url
    : signatureSessionQrUrl(item.id, qrIndex.value);
});

function stopPolling(): void {
  if (pollTimer !== null) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
}

function cancelActive(): void {
  if (!activeId) {
    return;
  }
  const id = activeId;
  activeId = null;
  cancelSignatureSession(id).catch(() => {
    /* 会话可能已完成或已过期：取消失败不影响界面 */
  });
}

function resetState(): void {
  stateText.value = "等待手机扫码并提交签名…";
  stateClass.value = "";
}

function expiryLabel(expiresAt: string): string {
  const when = new Date(expiresAt);
  if (Number.isNaN(when.getTime())) {
    return "";
  }
  return `有效期至 ${when.toLocaleString("zh-CN", { hour12: false })}`;
}

function finishWithMessage(message: string, cls: string): void {
  stopPolling();
  stateText.value = message;
  stateClass.value = cls;
}

function pollSession(id: string): void {
  stopPolling();
  activeId = id;
  const tick = async (): Promise<void> => {
    if (activeId !== id) {
      stopPolling();
      return;
    }
    let item: SignatureSession;
    try {
      item = (await getSignatureSession(id)).item;
    } catch (error) {
      if (activeId !== id) {
        return;
      }
      if (error instanceof ApiError && error.code === "not_found") {
        finishWithMessage(
          "签名会话已失效（可能服务已重启），请重新生成二维码。",
          "error",
        );
      }
      return; // 其余错误是瞬时的：继续轮询
    }
    if (activeId !== id) {
      return;
    }
    if (item.state === "completed") {
      stopPolling();
      const version = item.content_sha256
        ? `（内容版本 ${item.content_sha256.slice(0, 12)}）`
        : "";
      const changed = item.content_changed
        ? "。注意：合同内容在生成二维码后已修改，这份签名对应旧内容版本"
        : "";
      stateText.value = `签名已保存：${item.signer_name} · ${fmtDate(item.completed_at)}${version}${changed}`;
      stateClass.value = item.content_changed ? "warn" : "ok";
      expiryText.value = "";
      const warn = Boolean(item.content_changed);
      toast(warn ? "签名已保存，但合同内容已变更，请核对" : "签名已保存到本机", warn);
      refreshAfterWrite().catch((error: unknown) => toast(errorMessage(error), true));
    } else if (item.state === "expired") {
      finishWithMessage("二维码已过期，请重新生成。", "error");
    }
  };
  void tick();
  pollTimer = setInterval(() => {
    void tick();
  }, 2000);
}

async function startSession(): Promise<void> {
  stopPolling();
  cancelActive();
  generating.value = true;
  startError.value = "";
  session.value = null;
  qrIndex.value = null;
  starting.value = true;
  try {
    const item = (
      await createSignatureSession({
        contract_id: props.contract.id,
        party_id: signerId.value,
      })
    ).item;
    if (closed) {
      cancelSignatureSession(item.id).catch(() => {
        /* 已关闭：尽力取消，不更新界面 */
      });
      return;
    }
    activeId = item.id;
    session.value = item;
    startLabel.value = "重新生成二维码";
    generating.value = false;
    resetState();
    expiryText.value = expiryLabel(item.expires_at);
    pollSession(item.id);
  } catch (error) {
    activeId = null;
    generating.value = false;
    startError.value = errorMessage(error);
    toast(errorMessage(error), true);
  } finally {
    starting.value = false;
  }
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
  closed = true;
  document.removeEventListener("keydown", onKeydown);
  stopPolling();
  cancelActive();
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
              <template v-else-if="generating"><div class="sign-qr-empty">正在生成二维码…</div></template>
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

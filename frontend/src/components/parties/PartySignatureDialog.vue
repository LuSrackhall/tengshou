<script setup lang="ts">
// 档案级签名采集对话框（v0.2）：手机与电脑同一局域网，扫码手写一次，
// 签名存入相对方档案并长期复用。一次性令牌与合同签名共用 /sign/* 基础设施。
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { ApiError } from "../../api/client";
import {
  cancelSignatureSession,
  createPartySignatureSession,
  getSignatureSession,
  signatureSessionQrUrl,
} from "../../api/endpoints";
import type { Party, SignatureSession } from "../../api/types";
import { errorMessage, toast } from "../../bridge/app";

const props = defineProps<{ party: Party }>();
const emit = defineEmits<{ close: []; done: [] }>();

const session = ref<SignatureSession | null>(null);
const starting = ref(false);
const startError = ref("");
const stateText = ref("等待手机扫码并提交签名…");
const stateClass = ref("");
const qrIndex = ref<number | null>(null);

let pollTimer: ReturnType<typeof setInterval> | null = null;
let activeId: string | null = null;
let closed = false;

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
        stopPolling();
        stateText.value = "签名会话已失效（可能服务已重启），请重新生成二维码。";
        stateClass.value = "error";
      }
      return; // 其余错误是瞬时的：继续轮询
    }
    if (activeId !== id) {
      return;
    }
    if (item.state === "completed") {
      stopPolling();
      activeId = null;
      stateText.value = "签名已保存到档案。";
      stateClass.value = "ok";
      toast("档案签名已保存到本机");
      emit("done");
    } else if (item.state === "expired") {
      stopPolling();
      activeId = null;
      stateText.value = "二维码已过期，请重新生成。";
      stateClass.value = "error";
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
  startError.value = "";
  session.value = null;
  qrIndex.value = null;
  starting.value = true;
  try {
    const item = (await createPartySignatureSession(props.party.id)).item;
    if (closed) {
      cancelSignatureSession(item.id).catch(() => {
        /* 已关闭：尽力取消 */
      });
      return;
    }
    activeId = item.id;
    session.value = item;
    stateText.value = "等待手机扫码并提交签名…";
    stateClass.value = "";
    pollSession(item.id);
  } catch (error) {
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
  void startSession();
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
          <h2>档案签名 · {{ props.party.name }}</h2>
          <button type="button" class="close" aria-label="关闭" @click="emit('close')">×</button>
        </div>
        <div class="modal-body">
          <div class="signature-panel">
            <p class="help">
              手机与电脑需连接同一局域网；二维码一次性有效，提交后失效。手机打不开时，请确认连接的是同一 Wi-Fi（非访客网络），也可直接输入下方地址。
            </p>
            <div class="sign-qr-empty">
              <template v-if="session">
                <div class="sign-qr">
                  <img id="partySignQrImg" :src="qrSrc" alt="档案签名二维码" width="300" height="300">
                  <div class="sign-qr-meta">
                    <label v-if="urls.length > 1" class="inline sign-url-pick">电脑有多张网卡时请选择手机能访问的地址：
                      <select v-model="qrIndex">
                        <option v-for="(url, index) in urls" :key="index" :value="index">{{ url }}</option>
                      </select>
                    </label>
                    <div class="sign-url mono">{{ currentUrl }}</div>
                    <div class="sign-state" :class="stateClass">{{ stateText }}</div>
                  </div>
                </div>
              </template>
              <template v-else-if="starting"><div class="sign-qr-empty">正在生成二维码…</div></template>
              <template v-else-if="startError">
                <div class="sign-qr-empty error">{{ startError }}</div>
              </template>
              <template v-else>点击「重新生成二维码」开始采集。</template>
            </div>
          </div>
        </div>
        <div class="modal-foot">
          <button type="button" @click="emit('close')">关闭</button>
          <button type="button" class="primary" :disabled="starting" @click="startSession">重新生成二维码</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

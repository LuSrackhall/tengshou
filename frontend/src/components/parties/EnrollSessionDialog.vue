<script setup lang="ts">
// 局域网自助建档二维码（v0.2）：出租方生成一次性二维码，租户在同一 Wi-Fi
// 下扫码填写姓名、证件号、证件照正反面与手写签名，提交即建档；档案列表
// 经 SSE/轮询自动刷新。会话一次性、LAN only。
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { ApiError } from "../../api/client";
import {
  cancelEnrollSession,
  createEnrollSession,
  enrollSessionQrUrl,
  getEnrollSession,
} from "../../api/endpoints";
import type { EnrollSession } from "../../api/types";
import { errorMessage, refreshAfterWrite, toast } from "../../bridge/app";

const emit = defineEmits<{ close: [] }>();

const session = ref<EnrollSession | null>(null);
const starting = ref(false);
const startError = ref("");
const stateText = ref("等待租户扫码填写并提交…");
const stateClass = ref("");
const qrIndex = ref<number | null>(null);
const completed = ref(false);

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
    : enrollSessionQrUrl(item.id, qrIndex.value);
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
  cancelEnrollSession(id).catch(() => {
    /* 会话可能已完成或已过期 */
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
    let item: EnrollSession;
    try {
      item = (await getEnrollSession(id)).item;
    } catch (error) {
      if (activeId !== id) {
        return;
      }
      if (error instanceof ApiError && error.code === "not_found") {
        stopPolling();
        activeId = null;
        stateText.value = "自助建档会话已失效（可能服务已重启），请重新生成二维码。";
        stateClass.value = "error";
      }
      return;
    }
    if (activeId !== id) {
      return;
    }
    if (item.state === "completed") {
      stopPolling();
      activeId = null;
      completed.value = true;
      stateText.value = "租户已提交档案，档案列表即将刷新。";
      stateClass.value = "ok";
      toast("收到自助建档，档案已保存");
      refreshAfterWrite().catch((error: unknown) => toast(errorMessage(error), true));
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
  completed.value = false;
  starting.value = true;
  try {
    const item = (await createEnrollSession()).item;
    if (closed) {
      cancelEnrollSession(item.id).catch(() => {
        /* 已关闭：尽力取消 */
      });
      return;
    }
    activeId = item.id;
    session.value = item;
    stateText.value = "等待租户扫码填写并提交…";
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
          <h2>租户自助建档二维码</h2>
          <button type="button" class="close" aria-label="关闭" @click="emit('close')">×</button>
        </div>
        <div class="modal-body">
          <div class="signature-panel">
            <p class="help">
              让租户连接与电脑相同的 Wi-Fi 后扫码，填写姓名、证件号（必填）、证件照正反面与手写签名，提交即建档；证件号已存在时会提示联系出租方，不能重复建档。地址仅局域网可访问，照片与签名只保存在本机。
            </p>
            <div class="sign-qr-empty">
              <template v-if="session">
                <div class="sign-qr">
                  <img id="enrollQrImg" :src="qrSrc" alt="自助建档二维码" width="300" height="300">
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
              <template v-else>点击「重新生成二维码」开始。</template>
            </div>
          </div>
        </div>
        <div class="modal-foot">
          <button type="button" @click="emit('close')">{{ completed ? "关闭" : "取消" }}</button>
          <button type="button" class="primary" :disabled="starting" @click="startSession">
            {{ session ? "重新生成二维码" : "生成二维码" }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

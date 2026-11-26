<script setup lang="ts">
// 局域网自助建档二维码（v0.2）：出租方生成一次性二维码，租户在同一 Wi-Fi
// 下扫码填写姓名、证件号、证件照正反面与手写签名，提交即建档；档案列表
// 经 SSE/轮询自动刷新。会话一次性、LAN only；生命周期由 useQrSession 共用，
// 弹窗外壳用 ui/ModalDialog。
import { onMounted, ref } from "vue";
import {
  cancelEnrollSession,
  createEnrollSession,
  enrollSessionQrUrl,
  getEnrollSession,
} from "../../api/endpoints";
import type { EnrollSession } from "../../api/types";
import { errorMessage, refreshAfterWrite, toast } from "../../bridge/app";
import ModalDialog from "../../ui/ModalDialog.vue";
import { useQrSession } from "../signatures/useQrSession";

const emit = defineEmits<{ close: [] }>();

const completed = ref(false);

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
} = useQrSession<EnrollSession>({
  create: async () => (await createEnrollSession()).item,
  get: async (id) => (await getEnrollSession(id)).item,
  cancel: (id) => cancelEnrollSession(id),
  qrUrl: enrollSessionQrUrl,
  messages: {
    waiting: "等待租户扫码填写并提交…",
    lost: "自助建档会话已失效（可能服务已重启），请重新生成二维码。",
    expired: "二维码已过期，请重新生成。",
  },
  onStarted: () => {
    completed.value = false;
  },
  onCompleted: () => {
    completed.value = true;
    toast("收到自助建档，档案已保存");
    refreshAfterWrite().catch((error: unknown) => toast(errorMessage(error), true));
    return { text: "租户已提交档案，档案列表即将刷新。", tone: "ok" };
  },
});

onMounted(() => {
  void startSession();
});
</script>

<template>
  <ModalDialog title="租户自助建档二维码" @close="emit('close')">
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
    <template #foot>
      <button type="button" @click="emit('close')">{{ completed ? "关闭" : "取消" }}</button>
      <button type="button" class="primary" :disabled="starting" @click="startSession">
        {{ session ? "重新生成二维码" : "生成二维码" }}
      </button>
    </template>
  </ModalDialog>
</template>

<script setup lang="ts">
// 档案级签名采集对话框（v0.2）：手机与电脑同一局域网，扫码手写一次，
// 签名存入相对方档案并长期复用。一次性令牌与合同签名共用 /sign/* 基础设施；
// 会话生命周期由 useQrSession 共用，弹窗外壳用 ui/ModalDialog。
import { onMounted } from "vue";
import {
  cancelSignatureSession,
  createPartySignatureSession,
  getSignatureSession,
  signatureSessionQrUrl,
} from "../../api/endpoints";
import type { Party, SignatureSession } from "../../api/types";
import { toast } from "../../bridge/app";
import ModalDialog from "../../ui/ModalDialog.vue";
import { useQrSession } from "../signatures/useQrSession";

const props = defineProps<{ party: Party }>();
// done 携带会话完成时返回的真实 signature_id，档案页据此展示新签名。
const emit = defineEmits<{ close: []; done: [signatureId: string] }>();

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
  create: async () => (await createPartySignatureSession(props.party.id)).item,
  get: async (id) => (await getSignatureSession(id)).item,
  cancel: (id) => cancelSignatureSession(id),
  qrUrl: signatureSessionQrUrl,
  messages: {
    waiting: "等待手机扫码并提交签名…",
    lost: "签名会话已失效（可能服务已重启），请重新生成二维码。",
    expired: "二维码已过期，请重新生成。",
  },
  onCompleted: (item) => {
    toast("档案签名已保存到本机");
    emit("done", item.signature_id ?? "");
    return { text: "签名已保存到档案。", tone: "ok" };
  },
});

onMounted(() => {
  void startSession();
});
</script>

<template>
  <ModalDialog :title="`档案签名 · ${props.party.name}`" @close="emit('close')">
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
    <template #foot>
      <button type="button" @click="emit('close')">关闭</button>
      <button type="button" class="primary" :disabled="starting" @click="startSession">重新生成二维码</button>
    </template>
  </ModalDialog>
</template>

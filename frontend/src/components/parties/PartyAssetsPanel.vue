<script setup lang="ts">
// 相对方档案升级（v0.2）：证件照正反面与档案级手写签名。
//
// 证件照经 POST /parties/{id}/id-photos/{side} 上传，服务端压缩为 JPEG；
// 档案签名通过局域网扫码采集（POST /parties/{id}/signature-sessions），
// 一次采集长期复用。所有资产只保存在本机数据目录，图片读取时服务端校验摘要。
import { computed, ref } from "vue";
import {
  partyIDPhotoUrl,
  partySignatureImageUrl,
  uploadPartyIDPhoto,
} from "../../api/endpoints";
import type { Party, PartyPhotoSide } from "../../api/types";
import { errorMessage, refreshAfterWrite, toast } from "../../bridge/app";
import PartySignatureDialog from "./PartySignatureDialog.vue";

const props = defineProps<{ party: Party }>();
const emit = defineEmits<{ updated: [party: Party] }>();

const item = ref<Party>({ ...props.party });
// 同一路径的图片会被浏览器缓存；上传后用版本号强制重载。
const photoVer = ref(0);
const busySide = ref<PartyPhotoSide | "">("");
const showSignature = ref(false);
const frontInput = ref<HTMLInputElement | null>(null);
const backInput = ref<HTMLInputElement | null>(null);

const frontUrl = computed(() =>
  item.value.id_front_path ? partyIDPhotoUrl(item.value.id, "front") : "",
);
const backUrl = computed(() =>
  item.value.id_back_path ? partyIDPhotoUrl(item.value.id, "back") : "",
);
const signatureUrl = computed(() =>
  item.value.signature_id ? partySignatureImageUrl(item.value.id) : "",
);

function photoSrc(side: PartyPhotoSide): string {
  const base = side === "front" ? frontUrl.value : backUrl.value;
  return base ? `${base}?v=${photoVer.value}` : "";
}

function pick(side: PartyPhotoSide): void {
  const input = side === "front" ? frontInput.value : backInput.value;
  input?.click();
}

function readAsDataUrl(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result ?? ""));
    reader.onerror = () => reject(new Error("图片读取失败，请重新选择"));
    reader.readAsDataURL(file);
  });
}

async function onFile(side: PartyPhotoSide, event: Event): Promise<void> {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file) {
    return;
  }
  if (busySide.value) {
    return;
  }
  busySide.value = side;
  try {
    const dataUrl = await readAsDataUrl(file);
    const result = await uploadPartyIDPhoto(item.value.id, side, dataUrl);
    item.value = result.item;
    photoVer.value += 1;
    emit("updated", result.item);
    toast(side === "front" ? "证件照正面已保存" : "证件照反面已保存");
    await refreshAfterWrite();
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    busySide.value = "";
  }
}

function onSignatureDone(): void {
  // 重新取一次档案以拿到新的 signature_id（轮询完成事件不带 item）。
  item.value = { ...item.value, signature_id: "pending" };
  photoVer.value += 1;
  showSignature.value = false;
  toast("档案签名已保存");
  refreshAfterWrite().catch((error: unknown) => toast(errorMessage(error), true));
}
</script>

<template>
  <section class="party-assets" aria-label="档案附件">
    <h3>证件与签名</h3>
    <p class="help">
      证件照与签名仅保存在本机数据目录；新建档案必须填写证件号，存量档案可在此补录，补录后长期有效。
    </p>
    <div class="asset-grid">
      <div class="asset-card">
        <div class="asset-title">证件照正面</div>
        <img
          v-if="frontUrl"
          :key="`front-${photoVer}`"
          class="asset-photo"
          :src="photoSrc('front')"
          alt="证件照正面"
        >
        <div v-else class="asset-empty">尚未上传</div>
        <button
          type="button"
          class="asset-btn"
          :disabled="busySide !== ''"
          @click="pick('front')"
        >{{ frontUrl ? "更换正面" : "上传正面" }}</button>
        <input
          ref="frontInput"
          class="asset-file"
          type="file"
          accept="image/*"
          aria-label="选择证件照正面"
          @change="onFile('front', $event)"
        >
      </div>
      <div class="asset-card">
        <div class="asset-title">证件照反面</div>
        <img
          v-if="backUrl"
          :key="`back-${photoVer}`"
          class="asset-photo"
          :src="photoSrc('back')"
          alt="证件照反面"
        >
        <div v-else class="asset-empty">尚未上传</div>
        <button
          type="button"
          class="asset-btn"
          :disabled="busySide !== ''"
          @click="pick('back')"
        >{{ backUrl ? "更换反面" : "上传反面" }}</button>
        <input
          ref="backInput"
          class="asset-file"
          type="file"
          accept="image/*"
          aria-label="选择证件照反面"
          @change="onFile('back', $event)"
        >
      </div>
      <div class="asset-card">
        <div class="asset-title">档案签名</div>
        <img
          v-if="signatureUrl"
          :key="`sig-${photoVer}`"
          class="asset-signature"
          :src="`${signatureUrl}?v=${photoVer}`"
          alt="档案签名"
        >
        <div v-else class="asset-empty">尚未采集</div>
        <button type="button" class="asset-btn" @click="showSignature = true">
          {{ signatureUrl ? "重新采集" : "扫码采集" }}
        </button>
        <span class="help">手机与电脑需在同一局域网；二维码一次性有效。</span>
      </div>
    </div>
    <PartySignatureDialog
      v-if="showSignature"
      :party="item"
      @close="showSignature = false"
      @done="onSignatureDone"
    />
  </section>
</template>

<style scoped>
.party-assets {
  margin-top: 18px;
  padding-top: 14px;
  border-top: 1px dashed var(--border, #d8ddda);
}
.party-assets h3 {
  margin: 0 0 4px;
  font-size: 15px;
}
.asset-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
  margin-top: 10px;
}
.asset-card {
  border: 1px solid var(--border, #d8ddda);
  border-radius: 12px;
  padding: 10px;
  background: #fff;
}
.asset-title {
  font-weight: 600;
  font-size: 14px;
  margin-bottom: 8px;
}
.asset-photo {
  display: block;
  width: 100%;
  max-height: 160px;
  object-fit: contain;
  border-radius: 8px;
  background: #f4f6f4;
}
.asset-signature {
  display: block;
  width: 100%;
  max-height: 90px;
  object-fit: contain;
  border-radius: 8px;
  background: #f4f6f4;
}
.asset-empty {
  color: #8b928c;
  font-size: 13px;
  padding: 18px 0;
}
.asset-btn {
  margin-top: 8px;
  min-height: 36px;
  padding: 0 12px;
}
.asset-file {
  display: none;
}
</style>

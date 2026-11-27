<script setup lang="ts">
// 相对方档案升级（v0.2）：证件照正反面与档案级手写签名。
//
// 证件照经 POST /parties/{id}/id-photos/{side} 上传，服务端压缩为 JPEG；
// 档案签名通过局域网扫码采集（POST /parties/{id}/signature-sessions），
// 一次采集长期复用。所有资产只保存在本机数据目录，图片读取时服务端校验摘要。
import { computed, ref } from "vue";
import {
  clearPartySeal,
  clearPartySignatureSeal,
  partyIDPhotoUrl,
  partySealImageUrl,
  partySignatureImageUrl,
  partySignatureSealImageUrl,
  uploadPartyIDPhoto,
  uploadPartySeal,
  uploadPartySignatureSeal,
} from "../../api/endpoints";
import type { Party, PartyPhotoSide } from "../../api/types";
import { errorMessage, refreshAfterWrite, toast } from "../../bridge/app";
import PartySignatureDialog from "./PartySignatureDialog.vue";

const props = defineProps<{ party: Party }>();
const emit = defineEmits<{ updated: [party: Party] }>();

const item = ref<Party>({ ...props.party });
// 同一路径的图片会被浏览器缓存；上传/采集后用各自的版本号强制重载。
const photoVer = ref(0);
const signatureVer = ref(0);
const sealVer = ref(0);
const sigSealVer = ref(0);
const busySide = ref<PartyPhotoSide | "">("");
const busyImage = ref<"" | "seal" | "signature-seal">("");
const showSignature = ref(false);
const frontInput = ref<HTMLInputElement | null>(null);
const backInput = ref<HTMLInputElement | null>(null);
const sealInput = ref<HTMLInputElement | null>(null);
const sigSealInput = ref<HTMLInputElement | null>(null);

const frontUrl = computed(() =>
  item.value.id_front_path ? partyIDPhotoUrl(item.value.id, "front") : "",
);
const backUrl = computed(() =>
  item.value.id_back_path ? partyIDPhotoUrl(item.value.id, "back") : "",
);
const signatureUrl = computed(() =>
  item.value.signature_id ? partySignatureImageUrl(item.value.id) : "",
);
const signatureSrc = computed(() =>
  signatureUrl.value ? `${signatureUrl.value}?v=${signatureVer.value}` : "",
);
const sealUrl = computed(() =>
  item.value.seal_asset_id ? partySealImageUrl(item.value.id) : "",
);
const sealSrc = computed(() =>
  sealUrl.value ? `${sealUrl.value}?v=${sealVer.value}` : "",
);
const sigSealUrl = computed(() =>
  item.value.signature_seal_id ? partySignatureSealImageUrl(item.value.id) : "",
);
const sigSealSrc = computed(() =>
  sigSealUrl.value ? `${sigSealUrl.value}?v=${sigSealVer.value}` : "",
);

/** 图片来源（lan_enroll = 手机自助录入，app_upload = 桌面导入）。 */
function sourceLabel(source: string | undefined): string {
  if (source === "lan_enroll") {
    return "手机自助录入";
  }
  if (source === "app_upload") {
    return "桌面导入";
  }
  return "";
}

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

function onSignatureDone(signatureId: string): void {
  // 会话完成视图带真实 signature_id；写回档案对象，不再伪造占位值。
  if (signatureId) {
    item.value = { ...item.value, signature_id: signatureId };
  }
  signatureVer.value += 1;
  showSignature.value = false;
  toast("档案签名已保存");
  refreshAfterWrite().catch((error: unknown) => toast(errorMessage(error), true));
}

function pickSeal(): void {
  sealInput.value?.click();
}

function pickSigSeal(): void {
  sigSealInput.value?.click();
}

async function onArchiveImage(
  kind: "seal" | "signature-seal",
  event: Event,
): Promise<void> {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file || busyImage.value) {
    return;
  }
  busyImage.value = kind;
  try {
    const dataUrl = await readAsDataUrl(file);
    const result =
      kind === "seal"
        ? await uploadPartySeal(item.value.id, dataUrl)
        : await uploadPartySignatureSeal(item.value.id, dataUrl);
    item.value = result.item;
    if (kind === "seal") {
      sealVer.value += 1;
      toast("档案公章已保存");
    } else {
      sigSealVer.value += 1;
      toast("档案签名章已保存");
    }
    emit("updated", result.item);
    await refreshAfterWrite();
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    busyImage.value = "";
  }
}

async function disableArchiveImage(kind: "seal" | "signature-seal"): Promise<void> {
  const label = kind === "seal" ? "档案公章" : "档案签名章";
  if (!window.confirm(`确定停用${label}吗？停用后新建合同不再默认复用，图片资产仍保留。`)) {
    return;
  }
  if (busyImage.value) {
    return;
  }
  busyImage.value = kind;
  try {
    const result =
      kind === "seal"
        ? await clearPartySeal(item.value.id)
        : await clearPartySignatureSeal(item.value.id);
    item.value = result.item;
    emit("updated", result.item);
    toast(`${label}已停用`);
    await refreshAfterWrite();
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    busyImage.value = "";
  }
}
</script>

<template>
  <section class="party-assets" aria-label="档案附件">
    <h3>证件、签名与公章</h3>
    <p class="help">
      证件照、签名与公章仅保存在本机数据目录；新建档案必须填写证件号，存量档案可在此补录，补录后长期有效。
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
          :key="`sig-${signatureVer}`"
          class="asset-signature"
          :src="signatureSrc"
          alt="档案签名"
        >
        <div v-else class="asset-empty">尚未采集</div>
        <button type="button" class="asset-btn" @click="showSignature = true">
          {{ signatureUrl ? "重新采集" : "扫码采集" }}
        </button>
        <span class="help">手机与电脑需在同一局域网；二维码一次性有效。</span>
      </div>
      <div class="asset-card">
        <div class="asset-title">档案签名章（私章）</div>
        <img
          v-if="sigSealUrl"
          :key="`sigseal-${sigSealVer}`"
          class="asset-signature"
          :src="sigSealSrc"
          alt="档案签名章"
        >
        <div v-else class="asset-empty">尚未上传</div>
        <div class="asset-actions">
          <button
            type="button"
            class="asset-btn"
            :disabled="busyImage !== ''"
            @click="pickSigSeal"
          >{{ sigSealUrl ? "更换签名章" : "上传签名章" }}</button>
          <button
            v-if="sigSealUrl"
            type="button"
            class="asset-btn danger"
            :disabled="busyImage !== ''"
            @click="disableArchiveImage('signature-seal')"
          >停用</button>
        </div>
        <input
          ref="sigSealInput"
          class="asset-file"
          type="file"
          accept="image/*"
          aria-label="选择签名章图片"
          @change="onArchiveImage('signature-seal', $event)"
        >
        <span v-if="sourceLabel(item.signature_seal_source)" class="help">
          来源：{{ sourceLabel(item.signature_seal_source) }}；合同可选用签名章代替手写签名。
        </span>
        <span v-else class="help">上传后可在合同里代替手写签名（二选一）。</span>
      </div>
      <div class="asset-card">
        <div class="asset-title">档案公章（企业公章）</div>
        <img
          v-if="sealUrl"
          :key="`seal-${sealVer}`"
          class="asset-signature"
          :src="sealSrc"
          alt="档案公章"
        >
        <div v-else class="asset-empty">尚未上传</div>
        <div class="asset-actions">
          <button
            type="button"
            class="asset-btn"
            :disabled="busyImage !== ''"
            @click="pickSeal"
          >{{ sealUrl ? "更换公章" : "上传公章" }}</button>
          <button
            v-if="sealUrl"
            type="button"
            class="asset-btn danger"
            :disabled="busyImage !== ''"
            @click="disableArchiveImage('seal')"
          >停用</button>
        </div>
        <input
          ref="sealInput"
          class="asset-file"
          type="file"
          accept="image/*"
          aria-label="选择档案公章图片"
          @change="onArchiveImage('seal', $event)"
        >
        <span v-if="sourceLabel(item.seal_source)" class="help">
          来源：{{ sourceLabel(item.seal_source) }}；启用档案复用时合同默认套印。
        </span>
        <span v-else class="help">企业租户的公章；启用档案复用时合同默认套印。</span>
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
.asset-actions {
  display: flex;
  gap: 8px;
}
.asset-actions .danger {
  color: var(--danger, #b4342c);
}
.asset-file {
  display: none;
}
</style>

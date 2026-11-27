<script setup lang="ts">
// 合同表单「档案复用」区块（te-48hkl，Q6 修正）：复用功能始终可用——选好
// 租户档案后默认勾选其可复用的签名（手写签名或签名章二选一）与公章，保存
// 合同后由本组件调用复用接口，变更记录标注来源。设置页开关只控制是否展示
// 告知提示：开启时显示「将复用…（来源：档案）」与回退提示；关闭时不显示
// 任何来源/回退文案，但仍按勾选静默复用。取消勾选则回退现场扫码手写。
import { computed, onMounted, ref, watch } from "vue";
import { attachSeal } from "../../api/endpoints";
import { getArchiveReuseSettings, reusePartySignature } from "../../api/endpoints";
import type { ArchiveSignatureKind, Party } from "../../api/types";
import { errorMessage, refreshAfterWrite, toast } from "../../bridge/app";

const props = defineProps<{ party: Party | null }>();

const notifyEnabled = ref(false);
const useSignature = ref(false);
const signatureKind = ref<ArchiveSignatureKind>("archive");
const useSeal = ref(false);
const applying = ref(false);

const hasSignature = computed(() => Boolean(props.party?.signature_id));
const hasSignatureSeal = computed(() => Boolean(props.party?.signature_seal_id));
const hasSeal = computed(() => Boolean(props.party?.seal_asset_id));
const hasAny = computed(() => hasSignature.value || hasSignatureSeal.value || hasSeal.value);

/** 告知提示：只列出该档案实际可复用的资产（来源标注为档案）。 */
const notifyText = computed(() => {
  const parts: string[] = [];
  if (hasSignature.value || hasSignatureSeal.value) {
    parts.push("档案签名（来源：档案）");
  }
  if (hasSeal.value) {
    parts.push("档案公章（来源：档案）");
  }
  return (
    `保存合同时将复用${parts.join("与")}。如对方不同意，请取消勾选并改用现场扫码手写。` +
    "复用档案签名、签名章与公章的法律风险由使用者自行承担。"
  );
});

function applyDefaults(): void {
  const party = props.party;
  useSignature.value = Boolean(
    party && (party.signature_id || party.signature_seal_id),
  );
  signatureKind.value = party?.signature_id ? "archive" : "archive_seal";
  useSeal.value = Boolean(party?.seal_asset_id);
}

watch(() => props.party?.id, applyDefaults, { immediate: true });

onMounted(() => {
  getArchiveReuseSettings()
    .then((settings) => {
      notifyEnabled.value = settings.notify;
    })
    .catch(() => {
      // 读取失败按「不告知」处理：复用照常可用，只是不展示来源与回退提示。
      notifyEnabled.value = false;
    });
});

/** 保存合同成功后调用：按勾选项复用档案签名与公章。失败只提示，不回滚合同。 */
async function apply(contractId: string): Promise<void> {
  if (!props.party || applying.value) {
    return;
  }
  const failures: string[] = [];
  let applied = false;
  if (
    useSignature.value &&
    ((signatureKind.value === "archive" && hasSignature.value) ||
      (signatureKind.value === "archive_seal" && hasSignatureSeal.value))
  ) {
    try {
      await reusePartySignature(
        contractId,
        props.party.id,
        signatureKind.value === "archive_seal" ? "archive_seal" : undefined,
      );
      applied = true;
    } catch (error) {
      failures.push(`签名：${errorMessage(error)}`);
    }
  }
  if (useSeal.value && hasSeal.value && props.party.seal_asset_id) {
    try {
      await attachSeal(props.party.seal_asset_id, [contractId]);
      applied = true;
    } catch (error) {
      failures.push(`公章：${errorMessage(error)}`);
    }
  }
  if (failures.length) {
    toast(`合同已保存，但档案复用未完成：${failures.join("；")}`, true);
  } else if (applied) {
    toast("已按档案复用签名/公章（变更记录已标注来源）");
    await refreshAfterWrite();
  }
}

defineExpose({ apply });
</script>

<template>
  <section v-if="party && hasAny" id="contractArchiveReuse" class="archive-reuse">
    <h3>档案复用</h3>
    <p v-if="notifyEnabled" class="help">
      {{ notifyText }}
    </p>
    <label v-if="hasSignature || hasSignatureSeal" class="switch archive-reuse-row">
      <input v-model="useSignature" type="checkbox">
      <span>复用档案{{ signatureKind === "archive_seal" ? "签名章" : "签名" }}<template v-if="notifyEnabled">（来源：档案）</template></span>
    </label>
    <div v-if="hasSignature && hasSignatureSeal && useSignature" class="archive-reuse-kinds">
      <label class="inline">
        <input v-model="signatureKind" type="radio" value="archive">
        <span>手写签名</span>
      </label>
      <label class="inline">
        <input v-model="signatureKind" type="radio" value="archive_seal">
        <span>签名章（私章）</span>
      </label>
    </div>
    <label v-if="hasSeal" class="switch archive-reuse-row">
      <input v-model="useSeal" type="checkbox">
      <span>使用档案公章<template v-if="notifyEnabled">（来源：档案）</template></span>
    </label>
  </section>
</template>

<style scoped>
.archive-reuse {
  margin: 6px 0 16px;
  padding: 12px 14px;
  border: 1px dashed var(--border, #d8ddda);
  border-radius: 12px;
}
.archive-reuse h3 {
  margin: 0 0 6px;
  font-size: 15px;
}
.archive-reuse-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 8px 0;
}
.archive-reuse-kinds {
  display: flex;
  gap: 16px;
  margin: 0 0 8px 26px;
}
</style>

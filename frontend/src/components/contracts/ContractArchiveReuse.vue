<script setup lang="ts">
// 合同表单「档案复用」区块（te-48hkl）：总开关开启时，按租户档案默认勾选
// 复用签名（手写签名或签名章二选一）与公章；保存合同后由本组件调用复用
// 接口，变更记录标注来源。总开关关闭时不显示勾选项，合同行为与旧版一致
// （现场扫码手写 / 上传公章）。
import { computed, onMounted, ref, watch } from "vue";
import { attachSeal } from "../../api/endpoints";
import { getArchiveReuseSettings, reusePartySignature } from "../../api/endpoints";
import type { ArchiveSignatureKind, Party } from "../../api/types";
import { errorMessage, refreshAfterWrite, toast } from "../../bridge/app";

const props = defineProps<{ party: Party | null }>();

const settingsEnabled = ref(false);
const settingsLoaded = ref(false);
const useSignature = ref(false);
const signatureKind = ref<ArchiveSignatureKind>("archive");
const useSeal = ref(false);
const applying = ref(false);

const hasSignature = computed(() => Boolean(props.party?.signature_id));
const hasSignatureSeal = computed(() => Boolean(props.party?.signature_seal_id));
const hasSeal = computed(() => Boolean(props.party?.seal_asset_id));
const hasAny = computed(() => hasSignature.value || hasSignatureSeal.value || hasSeal.value);

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
      settingsEnabled.value = settings.enabled;
      if (!settings.enabled) {
        useSignature.value = false;
        useSeal.value = false;
      }
    })
    .catch(() => {
      // 读取失败按关闭处理：不确定时不做默认复用，用户可到档案页或签名对话框手动复用。
      settingsEnabled.value = false;
    })
    .finally(() => {
      settingsLoaded.value = true;
    });
});

/** 保存合同成功后调用：按勾选项复用档案签名与公章。失败只提示，不回滚合同。 */
async function apply(contractId: string): Promise<void> {
  if (!settingsLoaded.value || !settingsEnabled.value || !props.party || applying.value) {
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
    toast("已按档案复用签名/公章（合同详情会标注来源）");
    await refreshAfterWrite();
  }
}

defineExpose({ apply });
</script>

<template>
  <section v-if="party && hasAny && settingsLoaded" id="contractArchiveReuse" class="archive-reuse">
    <h3>档案复用</h3>
    <template v-if="settingsEnabled">
      <p class="help">
        总开关已开启：保存合同时按下列勾选复用租户档案。复用档案签名、签名章与公章的法律风险由使用者自行承担；不认可时取消勾选，改为现场扫码手写或上传公章。
      </p>
      <label v-if="hasSignature || hasSignatureSeal" class="switch archive-reuse-row">
        <input v-model="useSignature" type="checkbox">
        <span>复用档案{{ signatureKind === "archive_seal" ? "签名章" : "签名" }}（来源：档案）</span>
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
        <span>使用档案公章（来源：档案）</span>
      </label>
    </template>
    <p v-else class="hint">
      设置页已关闭「档案签名/公章复用」：本合同不会默认复用档案资产；需要时可在签名对话框手动选择档案签名。
    </p>
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

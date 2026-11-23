<script setup lang="ts">
// 已采集签名摘要（阶段 1 迁移）：合同详情与扫码签名对话框共用。
// DOM、类名与文案同存量 app.js signaturesSummaryHTML；导出仍走
// POST /signatures/{id}/export 的二次确认，落盘文件名沿用迁移前的
// `${id}.png`（忽略服务端 Content-Disposition 的文件名）。
import { computed } from "vue";
import { exportSignature } from "../../api/endpoints";
import type { ContractDetail } from "../../api/types";
import { errorMessage, toast } from "../../bridge/app";
import { fmtDate } from "../../ui/format";
import { saveDownloadedFile } from "../../ui/download";
import { shortDigest } from "../contracts/format";

const props = defineProps<{ detail: ContractDetail }>();

interface SummaryItem {
  id: string;
  role: string;
  signerName: string;
  capturedAt: string;
  version: string;
}

function roleOf(partyID: string | undefined): string {
  if (props.detail.party_a && props.detail.party_a.id === partyID) {
    return "甲方";
  }
  if (props.detail.party_b && props.detail.party_b.id === partyID) {
    return "乙方";
  }
  return "签名";
}

const items = computed<SummaryItem[]>(() =>
  props.detail.signatures.map((sig) => ({
    id: sig.id,
    role: roleOf(sig.party_id),
    signerName: sig.signer_name,
    capturedAt: fmtDate(sig.captured_at),
    version: sig.content_sha256 ? shortDigest(sig.content_sha256) : "",
  })),
);

async function exportOne(id: string): Promise<void> {
  const confirmed = window.confirm(
    "签名图片是敏感资产：导出后可能被复制、传播，离开本机数据目录。\n\n确认导出这份签名？",
  );
  if (!confirmed) {
    return;
  }
  try {
    const file = await exportSignature(id);
    saveDownloadedFile(file, `${id}.png`, true);
    toast("签名图片已导出；导出操作已写入变更记录");
  } catch (error) {
    toast(errorMessage(error), true);
  }
}
</script>

<template>
  <div class="signature-summary">
    <strong>已采集签名 {{ detail.signatures.length }} 个</strong>
    <ul>
      <li v-for="item in items" :key="item.id">
        <span class="badge active">{{ item.role }}</span> {{ item.signerName }} · {{ item.capturedAt }}<template v-if="item.version"> · 内容版本 <code>{{ item.version }}</code></template>
        <button type="button" :data-export-signature="item.id" @click="exportOne(item.id)">导出</button>
      </li>
    </ul>
  </div>
</template>

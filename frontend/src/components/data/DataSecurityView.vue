<script setup lang="ts">
// 数据安全（阶段 1 收口）：迁移 app.js 的备份导出、恢复与一致性检查。
//
// 文案、确认弹窗、按钮忙碌态与报告结构逐字保留；下载/恢复走 typed 端点。
// 切换主 tab 时重取备份状态（等价于迁移前 switchTab("data")）。

import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import {
  downloadBackup,
  getBackupStatus,
  getIntegrityReport,
  restoreBackup,
} from "../../api/endpoints";
import type {
  AssetIssue,
  BackupStatus,
  IntegrityReport,
} from "../../api/types";
import { errorMessage, subscribeTabChanged, toast } from "../../bridge/app";
import { currentTab } from "../../shell/tabs";
import { saveDownloadedFile } from "../../ui/download";
import { fmtDate } from "../../ui/format";

const status = ref<BackupStatus | null>(null);
const exporting = ref(false);
const restoring = ref(false);
const checking = ref(false);
const integrity = ref<IntegrityReport | null>(null);
const integrityLoading = ref(false);
const integrityError = ref("");
const backupInput = ref<HTMLInputElement | null>(null);

let unsubTabs: (() => void) | null = null;

const missingPaths = computed<string[]>(() =>
  (integrity.value?.missing_files ?? []).map((issue: AssetIssue) => issue.path),
);
const orphanPaths = computed<string[]>(() =>
  (integrity.value?.orphan_files ?? []).map((item) => item),
);

async function loadStatus(silent = false): Promise<void> {
  try {
    status.value = await getBackupStatus();
  } catch (error) {
    if (!silent) {
      toast(errorMessage(error), true);
    }
  }
}

async function exportBackup(): Promise<void> {
  if (exporting.value) {
    return;
  }
  exporting.value = true;
  try {
    const file = await downloadBackup();
    saveDownloadedFile(file, "tengshou-backup.zip");
    toast("完整备份包已开始下载；导出记录已写入变更记录");
    await loadStatus(true);
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    exporting.value = false;
  }
}

function chooseBackup(): void {
  const input = backupInput.value;
  if (!input) {
    return;
  }
  input.value = "";
  input.click();
}

async function restoreFromFile(event: Event): Promise<void> {
  const input = event.target as HTMLInputElement;
  const file = input.files && input.files[0];
  input.value = "";
  if (!file) {
    return;
  }
  if (!/\.zip$/i.test(file.name)) {
    toast("请选择 .zip 备份包", true);
    return;
  }
  const ok = window.confirm(
    `将使用「${file.name}」整体替换当前数据：数据库、签名/公章图片、模板、已生成 PDF 和配置。\n\n` +
      "恢复包会先做完整性与摘要校验；校验通过后暂存，重启腾手才生效。\n" +
      "当前数据会先自动留一份“恢复前副本”；校验失败时原数据完全不动。\n\n确认恢复？",
  );
  if (!ok) {
    return;
  }
  if (restoring.value) {
    return;
  }
  restoring.value = true;
  try {
    await restoreBackup(file, file.name);
    toast("备份包校验通过并已暂存。请重启腾手，恢复才会生效。");
    await loadStatus(true);
  } catch (error) {
    toast(errorMessage(error), true);
  } finally {
    restoring.value = false;
  }
}

async function checkIntegrity(): Promise<void> {
  if (checking.value) {
    return;
  }
  checking.value = true;
  integrity.value = null;
  integrityError.value = "";
  integrityLoading.value = true;
  try {
    integrity.value = await getIntegrityReport();
  } catch (error) {
    integrityError.value = errorMessage(error);
  } finally {
    integrityLoading.value = false;
    checking.value = false;
  }
}

onMounted(() => {
  if (currentTab.value === "data") {
    void loadStatus();
  }
  unsubTabs = subscribeTabChanged((tab) => {
    if (tab === "data") {
      void loadStatus();
    }
  });
});

onBeforeUnmount(() => {
  unsubTabs?.();
  unsubTabs = null;
});
</script>

<template>
  <div class="toolbar">
    <button type="button" id="exportBackupBtn" class="primary" :disabled="exporting" @click="exportBackup">
      {{ exporting ? "正在打包…" : "导出完整备份包" }}
    </button>
    <button type="button" id="restoreBackupBtn" :disabled="restoring" @click="chooseBackup">
      {{ restoring ? "正在校验…" : "从备份包恢复…" }}
    </button>
    <button type="button" id="checkIntegrityBtn" :disabled="checking" @click="checkIntegrity">数据一致性检查</button>
  </div>
  <p class="hint">所有数据都在本机数据目录：数据库、签名与公章图片、模板文件、已生成的 PDF 和配置。备份包包含上述全部内容（不含日志），换电脑时可在新机器上恢复。备份包未加密，请像保管合同原件一样保管它。</p>
  <div id="backupStatus" class="summary">
    <template v-if="status">
      <span>数据目录：<code>{{ status.data_dir || "" }}</code></span>
      <span v-if="status.pending">
        <span class="badge update">待重启生效</span>
        备份包 {{ status.pending.id }}（{{ fmtDate(status.pending.staged_at) }} 校验通过，{{ status.pending.file_count }} 个文件）
      </span>
      <span v-else>
        <span class="badge active">已就绪</span> 恢复包会在校验通过后暂存，重启腾手才生效
      </span>
    </template>
  </div>
  <div id="integrityReport">
    <p v-if="integrityLoading" class="hint">正在检查数据库与资产文件…</p>
    <div v-else-if="integrityError" class="form-errors">{{ integrityError }}</div>
    <div v-else-if="integrity" class="change-card">
      <div class="change-head">
        <span v-if="integrity.database_ok" class="badge active">数据库完整性检查通过</span>
        <template v-else>
          <span class="badge error">数据库完整性检查失败</span> {{ integrity.database_error || "" }}
        </template>
        <span class="who">{{ fmtDate(integrity.checked_at) }} · 已核对 {{ integrity.checked_files || 0 }} 个文件</span>
      </div>
      <h3>数据库有记录但文件缺失（{{ missingPaths.length }}）</h3>
      <ul v-if="missingPaths.length">
        <li v-for="path in missingPaths" :key="path"><code>{{ path }}</code></li>
      </ul>
      <p v-else class="hint">无</p>
      <h3>文件存在但数据库没有记录（{{ orphanPaths.length }}）</h3>
      <ul v-if="orphanPaths.length">
        <li v-for="path in orphanPaths" :key="path"><code>{{ path }}</code></li>
      </ul>
      <p v-else class="hint">无</p>
      <p class="hint">此检查只报告，不自动删除任何文件。</p>
    </div>
  </div>
  <input
    ref="backupInput"
    id="backupFileInput"
    type="file"
    accept=".zip,application/zip"
    hidden
    @change="restoreFromFile"
  >
</template>

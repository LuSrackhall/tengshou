<script setup lang="ts">
// Excel 导入文件选择宿主：挂在应用壳里，监听 tengshou:open-excel-import。
//
// 迁移前由 app.js 的隐藏 <input id="importFile"> 承接；现在文件选择与弹窗都
// 由 Vue 负责，点击入口仍来自合同台账的「导入 Excel」按钮。
import { onBeforeUnmount, onMounted, ref } from "vue";
import { OPEN_EXCEL_IMPORT_EVENT } from "../../shell/events";
import ExcelImportDialog from "./ExcelImportDialog.vue";

const inputEl = ref<HTMLInputElement | null>(null);
const file = ref<File | null>(null);

function openPicker(): void {
  const input = inputEl.value;
  if (!input) {
    return;
  }
  input.value = "";
  input.click();
}

function onFileChange(event: Event): void {
  const input = event.target as HTMLInputElement;
  const picked = input.files && input.files[0];
  input.value = "";
  if (picked) {
    file.value = picked;
  }
}

onMounted(() => {
  window.addEventListener(OPEN_EXCEL_IMPORT_EVENT, openPicker);
});

onBeforeUnmount(() => {
  window.removeEventListener(OPEN_EXCEL_IMPORT_EVENT, openPicker);
});
</script>

<template>
  <input
    ref="inputEl"
    id="importFile"
    type="file"
    accept=".xlsx"
    hidden
    @change="onFileChange"
  >
  <ExcelImportDialog
    v-if="file"
    :key="file.name"
    :file="file"
    @close="file = null"
  />
</template>

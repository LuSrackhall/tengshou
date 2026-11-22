// app.js → Vue 的最小反向桥（仅公章摘要）。
//
// 合同详情弹窗仍由 app.js 渲染，其中「已关联公章」的摘要属于公章域。为把
// 公章渲染完全移出 app.js，main.ts 在这里注册 window.__tengshouSealSummary；
// app.js 打开合同详情时把摘要宿主和 detail.seals 交给 Vue 挂载。Vue 侧组件
// 不直接读 window，退役时本文件随 app.js 一起删除。
import { createApp } from "vue";
import type { Seal } from "../api/types";
import SealSummary from "../components/seals/SealSummary.vue";

declare global {
  interface Window {
    /** 迁移期由 main.ts 注册；app.js 打开合同详情时调用。 */
    __tengshouSealSummary?: (host: Element | null, seals: Seal[]) => void;
  }
}

export function installSealSummaryBridge(): void {
  window.__tengshouSealSummary = (host, seals) => {
    if (!host) {
      return;
    }
    createApp(SealSummary, { seals }).mount(host);
  };
}

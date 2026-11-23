// 外壳/各域共用的 window 事件契约（迁移期 app.js 派发的事件保持不变）。
//
// app.js 退役后由 shell/realtime.ts、shell/tabs.ts 与 bridge/app.ts 派发：
//   tengshou:change       一条变更（detail 是变更记录的紧凑子集）
//   tengshou:data-changed 需要重取的实体列表（detail.entities，空数组表示全部）
//   tengshou:tab-change   主 tab 切换（detail.tab）
//   tengshou:open-*       跨域打开表单/对话框的最小桥

export const DATA_CHANGED_EVENT = "tengshou:data-changed";
export const CHANGE_EVENT = "tengshou:change";
export const TAB_CHANGE_EVENT = "tengshou:tab-change";
export const OPEN_CONTRACT_FORM_EVENT = "tengshou:open-contract-form";
export const OPEN_BATCH_GENERATE_EVENT = "tengshou:open-batch-generate";
export const OPEN_TEMPLATE_EDITOR_EVENT = "tengshou:open-template-editor";
export const OPEN_TEMPLATE_IMPORT_EVENT = "tengshou:open-template-import";
export const OPEN_EXCEL_IMPORT_EVENT = "tengshou:open-excel-import";

export interface DataChangedDetail {
  entities: string[];
}

/** 通知各 Vue 域重取自己的数据（等价于迁移前 app.js notifyDataChanged）。 */
export function notifyDataChanged(entities: string[]): void {
  window.dispatchEvent(
    new CustomEvent<DataChangedDetail>(DATA_CHANGED_EVENT, {
      detail: { entities },
    }),
  );
}

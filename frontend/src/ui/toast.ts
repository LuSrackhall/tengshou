// 顶栏 toast 的公共状态：取代 app.js 里的全局 toast()。
//
// 文案时长与迁移前一致：普通提示 3.2 秒、错误提示 6 秒；新提示会覆盖旧提示
// 并重置计时。DOM 仍由 ToastHost.vue 以 #toast / .toast.show / .toast.error
// 输出，CSS 不改。

import { readonly, ref } from "vue";

export interface ToastState {
  message: string;
  isError: boolean;
  visible: boolean;
}

const state = ref<ToastState>({ message: "", isError: false, visible: false });
let timer: ReturnType<typeof setTimeout> | null = null;

export const toastState = readonly(state);

/** 显示一条 toast；isError 决定红色样式与停留时长。 */
export function showToast(message: string, isError = false): void {
  state.value = { message, isError, visible: true };
  if (timer !== null) {
    clearTimeout(timer);
  }
  timer = setTimeout(() => {
    state.value = { ...state.value, visible: false };
    timer = null;
  }, isError ? 6000 : 3200);
}

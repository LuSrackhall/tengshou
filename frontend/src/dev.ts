import { createApp } from "vue";
import LiveStatusBadge from "./components/LiveStatusBadge.vue";
import {
  isLiveStatusMode,
  publishLiveStatus,
} from "./bridge/live-status";
// 开发预览直接复用仓库存量样式，保证与真实页面观感一致；
// 生产入口 main.ts 不引入这份样式。
import "../../internal/httpapi/web/app/app.css";

createApp(LiveStatusBadge).mount("#vue-live-status");

// 开发预览按钮：与生产调度器共用 bridge/live-status 的发布契约，
// 不再在 index.html 内联重复实现事件名与 detail 结构。
for (const button of document.querySelectorAll<HTMLElement>("[data-mode]")) {
  button.addEventListener("click", () => {
    const mode = button.dataset.mode;
    if (!isLiveStatusMode(mode)) {
      return;
    }
    publishLiveStatus(mode, button.dataset.detail ?? "");
  });
}

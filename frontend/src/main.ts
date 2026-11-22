import { createApp } from "vue";
import LiveStatusBadge from "./components/LiveStatusBadge.vue";
import "./styles/island.css";

// 生产入口：只挂载已迁移的 Vue 岛。
// 旧 app.js 先于本模块执行（classic script），因此这里挂载时，
// window.__tengshouLiveStatus 里可能已有最近一次同步状态。
const host = document.getElementById("vue-live-status");

if (host) {
  createApp(LiveStatusBadge).mount(host);
}

import { createApp } from "vue";
import LiveStatusBadge from "./components/LiveStatusBadge.vue";
import PartiesView from "./components/parties/PartiesView.vue";
import "./styles/island.css";

// 生产入口：只挂载已迁移的 Vue 岛。
// 旧 app.js 先于本模块执行（classic script），因此这里挂载时，
// window.__tengshouLiveStatus 里可能已有最近一次同步状态。

const liveStatusHost = document.getElementById("vue-live-status");
if (liveStatusHost) {
  createApp(LiveStatusBadge).mount(liveStatusHost);
}

// 相对方档案（阶段 1 迁出 app.js）：<section id="view-parties"> 保留为宿主，
// 其 class="view active" 的切换仍由 app.js 的 switchTab 负责。
const partiesHost = document.getElementById("view-parties");
if (partiesHost) {
  createApp(PartiesView).mount(partiesHost);
}

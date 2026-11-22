import { createApp } from "vue";
import LiveStatusBadge from "./components/LiveStatusBadge.vue";
// 开发预览直接复用仓库存量样式，保证与真实页面观感一致；
// 生产入口 main.ts 不引入这份样式。
import "../../internal/httpapi/web/app/app.css";
import "./styles/island.css";

createApp(LiveStatusBadge).mount("#vue-live-status");

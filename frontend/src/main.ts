import { createApp } from "vue";
import App from "./App.vue";
import "./styles/shell.css";

// 生产入口：整个界面由一个 Vue 应用渲染（阶段 1 收口，app.js 已退役）。
// index.html 只保留 #app 宿主、本地 pdf.js vendor 与 Vite 产物。
createApp(App).mount("#app");

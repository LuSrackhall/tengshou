import { createApp } from "vue";
import LiveStatusBadge from "./components/LiveStatusBadge.vue";
import ContractsView from "./components/contracts/ContractsView.vue";
import FieldsView from "./components/fields/FieldsView.vue";
import OnboardingView from "./components/onboarding/OnboardingView.vue";
import PartiesView from "./components/parties/PartiesView.vue";
import SealsView from "./components/seals/SealsView.vue";
import TemplatesView from "./components/templates/TemplatesView.vue";
import { installSealSummaryBridge } from "./bridge/seal-summary";
import "./styles/island.css";

// 生产入口：只挂载已迁移的 Vue 岛。
// 旧 app.js 先于本模块执行（classic script），因此这里挂载时，
// window.__tengshouLiveStatus 里可能已有最近一次同步状态。

const liveStatusHost = document.getElementById("vue-live-status");
if (liveStatusHost) {
  createApp(LiveStatusBadge).mount(liveStatusHost);
}

// 合同台账（阶段 1 迁出 app.js）：<section id="view-contracts"> 保留为宿主，
// 其 class="view active" 的切换仍由 app.js 的 switchTab 负责。合同详情里的
// 生成面板与批量生成对话框已是 Vue 组件；扫码签名与 Excel 导入仍由 app.js
// 实现，经 bridge/app.ts 双向调用。
const contractsHost = document.getElementById("view-contracts");
if (contractsHost) {
  createApp(ContractsView).mount(contractsHost);
}

// 相对方档案（阶段 1 迁出 app.js）：<section id="view-parties"> 保留为宿主，
// 其 class="view active" 的切换仍由 app.js 的 switchTab 负责。
const partiesHost = document.getElementById("view-parties");
if (partiesHost) {
  createApp(PartiesView).mount(partiesHost);
}

// 字段管理（阶段 1 迁出 app.js）：<section id="view-fields"> 保留为宿主，
// tab 切换仍由 app.js 的 switchTab 负责。
const fieldsHost = document.getElementById("view-fields");
if (fieldsHost) {
  createApp(FieldsView).mount(fieldsHost);
}

// 公章（阶段 1 迁出 app.js）：<section id="view-seals"> 保留为宿主。
// 合同详情弹窗里的「已关联公章」摘要仍由 app.js 打开弹窗，但渲染交给 Vue：
// 这里注册反向桥，app.js 只负责把宿主元素交过来。
installSealSummaryBridge();
const sealsHost = document.getElementById("view-seals");
if (sealsHost) {
  createApp(SealsView).mount(sealsHost);
}

// 模板（阶段 1 迁出 app.js）：<section id="view-templates"> 保留为宿主。
// 列表、导入与标定编辑器都由 TemplatesView 渲染；引导步骤打开的编辑器经
// tengshou:open-template-editor 事件到达本岛。
const templatesHost = document.getElementById("view-templates");
if (templatesHost) {
  createApp(TemplatesView).mount(templatesHost);
}

// 新手引导（阶段 1 迁出 app.js）：<section id="view-onboarding"> 保留为宿主。
// 五步引导、完成/跳过/重新打开、本机自检与模板体检都由 OnboardingView 渲染；
// 深链经 bridge 切换主 tab 并派发 tengshou:open-* 事件。
const onboardingHost = document.getElementById("view-onboarding");
if (onboardingHost) {
  createApp(OnboardingView).mount(onboardingHost);
}

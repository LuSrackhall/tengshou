<script setup lang="ts">
// 应用外壳（阶段 1 收口）：顶栏（品牌 / tab / 统计 / 同步徽标）、九个视图
// 宿主、toast、Excel 导入宿主、agent 操作可见层与启动流程。
//
// 迁移前这些由 index.html + app.js 提供；现在 index.html 只保留 Vue 入口，
// tab 激活态、统计刷新与 boot 都在这里。各视图仍是原来的 Vue 域组件，DOM
// 结构与类名（topbar / tabs / meta / view active）保持不变。
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { getBootstrap } from "./api/endpoints";
import type { Bootstrap } from "./api/types";
import { errorMessage, subscribeDataChanged, toast } from "./bridge/app";
import LiveStatusBadge from "./components/LiveStatusBadge.vue";
import AssetsView from "./components/assets/AssetsView.vue";
import ChangelogView from "./components/changelog/ChangelogView.vue";
import ContractsView from "./components/contracts/ContractsView.vue";
import DataSecurityView from "./components/data/DataSecurityView.vue";
import FieldsView from "./components/fields/FieldsView.vue";
import ExcelImportHost from "./components/imports/ExcelImportHost.vue";
import OnboardingView from "./components/onboarding/OnboardingView.vue";
import PartiesView from "./components/parties/PartiesView.vue";
import SealsView from "./components/seals/SealsView.vue";
import SettingsView from "./components/settings/SettingsView.vue";
import AgentActivityOverlay from "./components/shell/AgentActivityOverlay.vue";
import TemplatesView from "./components/templates/TemplatesView.vue";
import { startRealtime } from "./shell/realtime";
import { TABS, currentTab, switchTab } from "./shell/tabs";
import ToastHost from "./ui/ToastHost.vue";

const bootstrap = ref<Bootstrap | null>(null);
const stats = computed(() => bootstrap.value?.stats ?? null);

let unsubData: (() => void) | null = null;
let metaTimer: ReturnType<typeof setTimeout> | undefined;

async function refreshMeta(): Promise<void> {
  try {
    bootstrap.value = await getBootstrap();
  } catch {
    /* transient: 保留上一份统计，等下次事件或重试 */
  }
}

/** 与迁移前 flushRealtimeRefresh 一致：合同/字段变化后 350ms 重取统计。 */
function scheduleMetaRefresh(): void {
  if (metaTimer !== undefined) {
    clearTimeout(metaTimer);
  }
  metaTimer = setTimeout(() => {
    metaTimer = undefined;
    void refreshMeta();
  }, 350);
}

onMounted(() => {
  void (async () => {
    try {
      bootstrap.value = await getBootstrap();
    } catch (error) {
      toast(errorMessage(error), true);
    }
    // B8：全新数据目录默认停在引导页，其它页仍一键可达。
    if (bootstrap.value?.onboarding?.show_guide) {
      switchTab("onboarding");
    }
    // 首次加载失败也启动变更流：agent 写入仍要能显示出来。
    startRealtime();
  })();
  unsubData = subscribeDataChanged((detail) => {
    const entities = detail.entities;
    if (
      entities.length === 0 ||
      entities.includes("contract") ||
      entities.includes("field")
    ) {
      scheduleMetaRefresh();
    }
  });
});

onBeforeUnmount(() => {
  unsubData?.();
  unsubData = null;
  if (metaTimer !== undefined) {
    clearTimeout(metaTimer);
    metaTimer = undefined;
  }
});
</script>

<template>
  <header class="topbar">
    <div class="brand">
      <h1>腾手</h1>
      <span class="tagline">合同数据录一次，台账、模板与导出共用</span>
    </div>
    <nav class="tabs" id="tabs" aria-label="功能页">
      <button
        v-for="tab in TABS"
        :key="tab.id"
        type="button"
        :data-tab="tab.id"
        :class="{ active: currentTab === tab.id }"
        @click="switchTab(tab.id)"
      >{{ tab.label }}</button>
    </nav>
    <div class="meta" id="meta">
      <template v-if="bootstrap">
        <span>操作者：{{ bootstrap.operator || "" }}</span>
        <span>合同 {{ stats?.contracts || 0 }} 份</span>
        <span>模板 {{ stats?.templates || 0 }} 版</span>
        <span>相对方 {{ stats?.parties || 0 }} 个</span>
        <span>公章 {{ stats?.seals || 0 }} 枚</span>
        <span>字段 {{ stats?.fields || 0 }} 个</span>
        <span v-if="stats?.expiring_contracts" class="meta-warn">
          即将到期 {{ stats.expiring_contracts }} 份
        </span>
        <span v-if="stats?.expired_contracts" class="meta-warn">
          已到期未续签 {{ stats.expired_contracts }} 份
        </span>
        <span :title="`应用版本（数据格式 v${bootstrap.schema_version || ''}）`">版本 {{ bootstrap.version || "未知" }}</span>
      </template>
    </div>
    <LiveStatusBadge />
  </header>

  <main>
    <section id="view-contracts" class="view" :class="{ active: currentTab === 'contracts' }" aria-label="合同台账">
      <ContractsView />
    </section>

    <section id="view-onboarding" class="view" :class="{ active: currentTab === 'onboarding' }" aria-label="新手引导">
      <OnboardingView />
    </section>

    <section id="view-templates" class="view" :class="{ active: currentTab === 'templates' }" aria-label="合同模板">
      <TemplatesView />
    </section>

    <section id="view-seals" class="view" :class="{ active: currentTab === 'seals' }" aria-label="公章图片">
      <SealsView />
    </section>

    <section id="view-parties" class="view" :class="{ active: currentTab === 'parties' }" aria-label="相对方档案">
      <PartiesView />
    </section>

    <section id="view-assets" class="view" :class="{ active: currentTab === 'assets' }" aria-label="房产档案">
      <AssetsView />
    </section>

    <section id="view-fields" class="view" :class="{ active: currentTab === 'fields' }" aria-label="字段管理">
      <FieldsView />
    </section>

    <section id="view-changelog" class="view" :class="{ active: currentTab === 'changelog' }" aria-label="变更记录">
      <ChangelogView />
    </section>

    <section id="view-data" class="view" :class="{ active: currentTab === 'data' }" aria-label="数据安全">
      <DataSecurityView />
    </section>

    <section id="view-settings" class="view" :class="{ active: currentTab === 'settings' }" aria-label="设置">
      <SettingsView />
    </section>
  </main>

  <ExcelImportHost />
  <ToastHost />
  <AgentActivityOverlay />
</template>

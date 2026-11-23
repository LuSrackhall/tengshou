<script setup lang="ts">
// 变更记录（阶段 1 收口）：迁移 app.js 的 renderChangelog /
// renderRecentActivity / loadChangelog。
//
// 筛选、刷新、最近操作与差异展示的文案、class 结构与迁移前一致；数据走
// typed listChangelog 端点。外部写入经 shell/realtime.ts 的 data-changed
// 到达本页，停留在本 tab 时自动重取。

import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { listChangelog } from "../../api/endpoints";
import type {
  ChangeEntity,
  ChangeLog,
  ChangeLogQuery,
  ChangeSource,
} from "../../api/types";
import {
  errorMessage,
  subscribeDataChanged,
  subscribeTabChanged,
  toast,
} from "../../bridge/app";
import { currentTab } from "../../shell/tabs";
import {
  ACTION_LABELS,
  ENTITY_LABELS,
  actorLabel,
  fmtDate,
  isAgentEntry,
} from "../../ui/format";

const ENTITIES: readonly ChangeEntity[] = [
  "contract",
  "party",
  "field",
  "seal",
  "template",
  "signature",
  "backup",
  "settings",
  "import",
];

const entity = ref<ChangeEntity | "">("");
const source = ref<ChangeSource | "">("");
const items = ref<ChangeLog[]>([]);
const recent = ref<ChangeLog[]>([]);
const total = ref(0);
const loaded = ref(false);

let unsubTabs: (() => void) | null = null;
let unsubData: (() => void) | null = null;

const summary = computed(
  () => `共 ${total.value} 条记录（显示最近 ${items.value.length} 条）`,
);

async function loadRecent(): Promise<void> {
  try {
    const data = await listChangelog({ limit: 8 });
    recent.value = data.items ?? [];
  } catch {
    /* 与迁移前一致：最近操作失败不打扰主列表 */
  }
}

async function load(silent = false): Promise<void> {
  const query: ChangeLogQuery = { limit: 100 };
  if (entity.value !== "") {
    query.entity = entity.value;
  }
  if (source.value !== "") {
    query.source = source.value;
  }
  try {
    const data = await listChangelog(query);
    items.value = data.items ?? [];
    total.value = data.total ?? 0;
    loaded.value = true;
    void loadRecent();
  } catch (error) {
    if (!silent) {
      toast(errorMessage(error), true);
    }
  }
}

function reload(): void {
  void load();
}

function onEntityChange(event: Event): void {
  const value = (event.target as HTMLSelectElement).value;
  entity.value = (ENTITIES as readonly string[]).includes(value)
    ? (value as ChangeEntity)
    : "";
  void load();
}

function onSourceChange(event: Event): void {
  const value = (event.target as HTMLSelectElement).value;
  source.value = value === "agent" || value === "human" ? value : "";
  void load();
}

onMounted(() => {
  if (currentTab.value === "changelog") {
    void load();
  }
  unsubTabs = subscribeTabChanged((tab) => {
    if (tab === "changelog") {
      void load();
    }
  });
  unsubData = subscribeDataChanged(() => {
    if (currentTab.value === "changelog") {
      void load(true);
    }
  });
});

onBeforeUnmount(() => {
  unsubTabs?.();
  unsubTabs = null;
  unsubData?.();
  unsubData = null;
});
</script>

<template>
  <div class="toolbar">
    <select id="changeEntityFilter" aria-label="对象类型" :value="entity" @change="onEntityChange">
      <option value="">全部对象</option>
      <option value="contract">合同</option>
      <option value="seal">公章</option>
      <option value="template">模板</option>
      <option value="party">相对方</option>
      <option value="field">字段</option>
      <option value="settings">设置</option>
    </select>
    <select id="changeActorFilter" aria-label="操作来源" :value="source" @change="onSourceChange">
      <option value="">全部来源</option>
      <option value="human">人工</option>
      <option value="agent">Agent（MCP）</option>
    </select>
    <span class="spacer"></span>
    <button type="button" id="refreshChangelogBtn" @click="reload">刷新</button>
  </div>
  <p class="hint">记录谁在何时改了什么。操作者取自本机系统用户，可在 config.json 的 operator 中指定；通过 MCP 连接器的写入标记为「Agent（MCP）」。外部写入会在数秒内自动反映到本页与各列表。</p>
  <div id="recentActivity" class="recent-activity">
    <template v-if="recent.length">
      <div class="recent-head">最近操作</div>
      <ul class="recent-list">
        <li v-for="entry in recent" :key="entry.id">
          <span class="recent-time">{{ fmtDate(entry.created_at) }}</span>
          <span
            class="badge source"
            :class="isAgentEntry(entry) ? 'agent' : 'human'"
            :title="`actor：${entry.actor || ''}`"
          >{{ actorLabel(entry.actor) }}</span>
          <span class="badge" :class="entry.action">{{ ACTION_LABELS[entry.action] || entry.action }}</span>
          <span class="recent-summary">{{ entry.summary }}</span>
        </li>
      </ul>
    </template>
  </div>
  <div id="changelogSummary" class="summary">{{ loaded ? summary : "" }}</div>
  <div id="changelogList" class="changelog">
    <div v-if="loaded && !items.length" class="empty">还没有变更记录。</div>
    <div v-for="entry in items" :key="entry.id" class="change-card">
      <div class="change-head">
        <span
          class="badge source"
          :class="isAgentEntry(entry) ? 'agent' : 'human'"
          :title="`actor：${entry.actor || ''}`"
        >{{ actorLabel(entry.actor) }}</span>
        <span class="badge" :class="entry.action">{{ ACTION_LABELS[entry.action] || entry.action }}</span>
        <span class="summary">{{ entry.summary }}</span>
        <span class="who">{{ ENTITY_LABELS[entry.entity] || entry.entity }} · {{ entry.actor || "" }} · {{ fmtDate(entry.created_at) }}</span>
      </div>
      <ul v-if="entry.changes && entry.changes.length" class="change-diffs">
        <li v-for="(change, index) in entry.changes" :key="index">
          {{ change.label || change.field }}：<span class="old">{{ change.old === "" ? "（空）" : change.old }}</span>
          <span class="arrow">→</span> <code>{{ change.new === "" ? "（空）" : change.new }}</code>
        </li>
      </ul>
    </div>
  </div>
</template>

<script setup lang="ts">
// 相对方编辑弹窗内的审计历史：与 app.js loadAuditHistory / auditHistoryHTML
// 和 changeDiffsHTML 的输出保持一致（加载中不占位，失败显示错误块）。
import { onBeforeUnmount, onMounted, ref } from "vue";
import { listChangelog } from "../../api/endpoints";
import type { ChangeLog } from "../../api/types";
import { errorMessage, subscribeChange } from "../../bridge/app";
import {
  ACTION_LABELS,
  actorLabel,
  fmtDate,
  isAgentActor,
} from "../../ui/format";

const props = defineProps<{ partyId: string }>();

const items = ref<ChangeLog[]>([]);
const loaded = ref(false);
const errorText = ref("");
let loadSeq = 0;
let unsubscribe: (() => void) | null = null;

async function load(): Promise<void> {
  const seq = ++loadSeq;
  try {
    const result = await listChangelog({
      entity: "party",
      entity_id: props.partyId,
      limit: 20,
    });
    if (seq !== loadSeq) {
      return;
    }
    items.value = result.items ?? [];
    loaded.value = true;
    errorText.value = "";
  } catch (error) {
    if (seq !== loadSeq) {
      return;
    }
    errorText.value = errorMessage(error);
  }
}

onMounted(() => {
  void load();
  unsubscribe = subscribeChange((notice) => {
    if (notice.entity === "party" && notice.entity_id === props.partyId) {
      void load();
    }
  });
});

onBeforeUnmount(() => {
  unsubscribe?.();
  unsubscribe = null;
});
</script>

<template>
  <div v-if="errorText" class="audit-block error">变更记录加载失败：{{ errorText }}</div>
  <template v-else-if="loaded">
    <div v-if="!items.length" class="audit-block">
      <div class="audit-head">变更记录</div>
      <div class="empty">这条记录还没有变更历史。</div>
    </div>
    <div v-else class="audit-block">
      <div class="audit-head">变更记录 <span class="muted">（最近 {{ items.length }} 条）</span></div>
      <ul class="audit-list">
        <li v-for="entry in items" :key="entry.id">
          <div class="audit-line">
            <span class="recent-time">{{ fmtDate(entry.created_at) }}</span>
            <span
              class="badge source"
              :class="isAgentActor(entry.actor) ? 'agent' : 'human'"
              :title="`actor：${entry.actor}`"
            >{{ actorLabel(entry.actor) }}</span>
            <span class="badge" :class="entry.action">{{ ACTION_LABELS[entry.action] ?? entry.action }}</span>
            <span class="audit-summary">{{ entry.summary }}</span>
          </div>
          <ul v-if="entry.changes && entry.changes.length" class="change-diffs">
            <li v-for="(change, index) in entry.changes" :key="index">
              {{ change.label || change.field }}：<span class="old">{{ change.old === "" ? "（空）" : change.old }}</span>
              <span class="arrow">→</span> <code>{{ change.new === "" ? "（空）" : change.new }}</code>
            </li>
          </ul>
        </li>
      </ul>
    </div>
  </template>
</template>

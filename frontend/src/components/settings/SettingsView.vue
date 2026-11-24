<script setup lang="ts">
// 设置 · MCP 连接器（阶段 1 收口）：迁移 app.js 的 loadMCPStatus /
// renderMCPPanel / setMCPWriteEnabled。
//
// 只展示状态与读写开关，不启动或停止 MCP 进程；文案、DOM 结构与迁移前一致。
// 外部 agent 写入经 tengshou:change / tengshou:data-changed 到达本页时重取。

import { onBeforeUnmount, onMounted, ref } from "vue";
import { getMCPStatus, updateMCPSettings } from "../../api/endpoints";
import type { MCPStatus } from "../../api/types";
import {
  errorMessage,
  subscribeChange,
  subscribeDataChanged,
  subscribeTabChanged,
  toast,
} from "../../bridge/app";
import { currentTab } from "../../shell/tabs";
import {
  agentActivityAnimationsEnabled,
  setAgentActivityAnimationsEnabled,
} from "../../shell/agent-activity";
import { MCP_ACTOR, fmtDate } from "../../ui/format";

const status = ref<MCPStatus | null>(null);
const errorText = ref("");
const toggling = ref(false);

let unsubTabs: (() => void) | null = null;
let unsubData: (() => void) | null = null;
let unsubChanges: (() => void) | null = null;

async function load(): Promise<void> {
  try {
    status.value = await getMCPStatus();
    errorText.value = "";
  } catch (error) {
    status.value = null;
    errorText.value = errorMessage(error);
  }
}

async function onToggle(event: Event): Promise<void> {
  const toggle = event.target as HTMLInputElement;
  const enabled = toggle.checked;
  if (
    !enabled &&
    !window.confirm(
      "关闭 MCP 写工具后，agent 只能读取数据（写工具会报错）。确认关闭？",
    )
  ) {
    toggle.checked = true;
    return;
  }
  if (toggling.value) {
    return;
  }
  toggling.value = true;
  try {
    await updateMCPSettings({ write_enabled: enabled });
    toast(enabled ? "已开启 MCP 写工具（即时生效）" : "已关闭 MCP 写工具（即时生效）");
    await load();
  } catch (error) {
    toggle.checked = !enabled;
    toast(errorMessage(error), true);
  } finally {
    toggling.value = false;
  }
}

/** 本地展示开关：只影响本机界面回放，不调用后端、不改 agent 读写权限。 */
function onAgentActivityToggle(event: Event): void {
  setAgentActivityAnimationsEnabled((event.target as HTMLInputElement).checked);
}

onMounted(() => {
  if (currentTab.value === "settings") {
    void load();
  }
  unsubTabs = subscribeTabChanged((tab) => {
    if (tab === "settings") {
      void load();
    }
  });
  unsubData = subscribeDataChanged((detail) => {
    if (currentTab.value === "settings" && detail.entities.includes("settings")) {
      void load();
    }
  });
  unsubChanges = subscribeChange((notice) => {
    if (currentTab.value === "settings" && notice.actor === MCP_ACTOR) {
      void load();
    }
  });
});

onBeforeUnmount(() => {
  unsubTabs?.();
  unsubTabs = null;
  unsubData?.();
  unsubData = null;
  unsubChanges?.();
  unsubChanges = null;
});
</script>

<template>
  <div class="toolbar">
    <span id="mcpStatusSummary" class="summary-inline">
      <template v-if="status">
        <span class="badge active">服务运行中</span>
        <span>读工具 {{ status.read_tools || 0 }} 个</span>
        <span>写工具 {{ status.write_tools || 0 }} 个</span>
      </template>
      <template v-else-if="errorText">MCP 状态不可用</template>
    </span>
    <span class="spacer"></span>
    <button type="button" id="refreshMCPStatusBtn" @click="load">刷新状态</button>
  </div>
  <p class="hint">MCP 连接器让本机 AI agent（Claude Code / Claude Desktop）读写腾手数据。连接器以 stdio 运行、只走本机回环，本面板不会启动或停止它。</p>
  <div id="mcpPanel" class="mcp-panel">
    <div v-if="errorText" class="form-errors">MCP 状态读取失败：{{ errorText }}</div>
    <div v-else-if="status" class="mcp-grid">
      <div class="mcp-card">
        <h3>连接器状态</h3>
        <dl>
          <dt>服务</dt><dd><span class="badge active">运行中</span></dd>
          <dt>连接器版本</dt><dd><code>{{ status.version || "未知" }}</code></dd>
          <dt>传输</dt><dd>stdio · 仅本机回环 · 不打开数据库</dd>
          <dt>读工具</dt><dd>{{ status.read_tools || 0 }} 个 <span class="muted">始终可用</span></dd>
          <dt>写工具</dt><dd>{{ status.write_tools || 0 }} 个 <span class="muted">新建字段 / 档案 / 合同草稿 / 生成合同 PDF，全部记入变更记录</span></dd>
          <dt>Agent 操作</dt><dd>累计 {{ status.agent_actions_total || 0 }} 次，近 24 小时 {{ status.agent_actions_recent || 0 }} 次</dd>
          <dt>最近一次</dt>
          <dd>
            <template v-if="status.last_agent_action">
              {{ status.last_agent_action.summary }}
              <span class="muted">（{{ fmtDate(status.last_agent_action.created_at) }}）</span>
            </template>
            <template v-else>还没有 agent 写入记录</template>
          </dd>
          <dt>数据目录</dt><dd><code>{{ status.data_dir || "" }}</code></dd>
        </dl>
      </div>
      <div class="mcp-card">
        <h3>读写开关</h3>
        <div class="switch-row">
          <label class="switch"><input type="checkbox" checked disabled>
            <span>读工具（默认开启）</span></label>
        </div>
        <div class="switch-row">
          <label class="switch">
            <input
              id="mcpWriteToggle"
              type="checkbox"
              :checked="status.write_enabled"
              :disabled="toggling"
              @change="onToggle"
            >
            <span>写工具（{{ status.write_enabled ? "已开启" : "已关闭" }}）</span>
          </label>
        </div>
        <p class="hint">关闭后：agent 仍可读取；写工具返回 <code>mcp_write_disabled</code>，桌面界面的写入不受影响。改动即时生效，并写入变更记录。</p>
        <p class="hint">配置文件：<code>{{ status.config_file || "config.json" }}</code>（键 <code>{{ status.settings_key || "mcp.write_enabled" }}</code>）。</p>
      </div>
      <div class="mcp-card full">
        <h3>连接说明</h3>
        <p>{{ status.connection_help || "" }}</p>
        <p class="hint">注册命令（填到 Claude Code / Claude Desktop 的 MCP 配置中）：</p>
        <pre class="mono mcp-command">{{ status.command || "tengshou-cli mcp" }}</pre>
        <p class="hint">本面板只展示状态与开关，不启动或停止 MCP 进程。</p>
      </div>
    </div>
    <p v-else class="hint">正在读取 MCP 状态…</p>
    <div class="mcp-grid agent-activity-grid">
      <div class="mcp-card">
        <h3>界面提示</h3>
        <div class="switch-row">
          <label class="switch">
            <input
              id="agentActivityToggle"
              type="checkbox"
              :checked="agentActivityAnimationsEnabled"
              @change="onAgentActivityToggle"
            >
            <span>显示 agent 操作动画（{{ agentActivityAnimationsEnabled ? "已开启" : "已关闭" }}）</span>
          </label>
        </div>
        <p class="hint">开启后，MCP agent 写入时界面会切到对应页面，并用模拟光标、点击波纹与目标行高亮回放这次操作；关闭后只保留顶部「agent 操作」徽标。本开关只影响本机界面的展示，不改变 agent 的读写权限。</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
// 设置页「MCP 接入」面板：显示当前可执行文件与实际数据目录，并生成
// WorkBuddy / 豆包 / DeepSeek Harness 三家可直接复制的 stdio 接入片段。
//
// 路径全部取自后端运行时返回值（exec_path / data_dir），不写死；第三方
// 字段依据公开文档整理，均标注「未实机验证」。

import { computed } from "vue";
import type { MCPStatus } from "../../api/types";
import { toast } from "../../bridge/app";
import { copyText } from "../../ui/clipboard";

const props = defineProps<{ status: MCPStatus }>();

interface AccessSnippet {
  id: string;
  title: string;
  note: string;
  text: string;
}

const exePath = computed(
  () => props.status.exec_path || "<腾手客户端完整路径>",
);
const dataDir = computed(() => props.status.data_dir || "<数据目录完整路径>");
const command = computed(() => props.status.command || "");

/** YAML 单引号标量：内部单引号写成两个，反斜杠保持原样。 */
function yamlQuote(value: string): string {
  return `'${value.replace(/'/g, "''")}'`;
}

const snippets = computed<AccessSnippet[]>(() => [
  {
    id: "workbuddy",
    title: "WorkBuddy（JSON · stdio）",
    note: "写入 WorkBuddy 的 mcpServers 配置（常见 ~/.workbuddy/mcp.json），或在「技能 → 连接器 → 自定义连接器」添加。未实机验证。",
    text: JSON.stringify(
      {
        mcpServers: {
          tengshou: {
            type: "stdio",
            command: exePath.value,
            args: ["mcp", "--data-dir", dataDir.value],
          },
        },
      },
      null,
      2,
    ),
  },
  {
    id: "doubao",
    title: "豆包（STDIO 表单）",
    note: "在「技能 · 连接器」添加自定义连接器并选择 STDIO，命令与参数逐个填入（每个参数一行）。豆包另支持 HTTP 连接器，腾手 v0.1 只保证 stdio 接入。未实机验证。",
    text: [
      "服务器名称：tengshou",
      "传输类型：STDIO",
      `命令：${exePath.value}`,
      "参数：mcp",
      "参数：--data-dir",
      `参数：${dataDir.value}`,
    ].join("\n"),
  },
  {
    id: "dsh",
    title: "DeepSeek Harness（插件 YAML）",
    note: "写入 DeepSeek Harness 的 cordis.patch.yml（Web 配置：~/.dsh/profiles/web/cordis.patch.yml），保留 - insert: 这一行。未实机验证。",
    text: [
      "- insert:",
      "    - id: mcp-tengshou",
      "      name: '@deepseek-ai/dsh-mcp-client'",
      "      config:",
      "        serverName: tengshou",
      "        transport: stdio",
      `        command: ${yamlQuote(exePath.value)}`,
      `        args: [${yamlQuote("mcp")}, ${yamlQuote("--data-dir")}, ${yamlQuote(dataDir.value)}]`,
    ].join("\n"),
  },
]);

async function onCopy(snippet: AccessSnippet): Promise<void> {
  const ok = await copyText(snippet.text);
  toast(
    ok ? `已复制${snippet.title}接入片段` : "复制失败，请手动选择文本复制",
    !ok,
  );
}
</script>

<template>
  <div class="mcp-card full">
    <h3>MCP 接入</h3>
    <dl>
      <dt>当前可执行文件</dt>
      <dd><code>{{ status.exec_path || "未知" }}</code></dd>
      <dt>实际数据目录</dt>
      <dd><code>{{ status.data_dir || "未知" }}</code></dd>
      <dt>统一命令</dt>
      <dd><pre class="mono mcp-command">{{ command }}</pre></dd>
    </dl>
    <p class="hint">以下片段中的路径按当前运行的客户端自动生成，可直接复制。先启动腾手桌面应用，再让 agent 以 stdio 启动连接器；各家配置字段依据公开文档整理，均未实机验证，以客户端当前界面为准。</p>
    <div v-for="snippet in snippets" :key="snippet.id" class="mcp-access-item">
      <div class="mcp-access-head">
        <h4>{{ snippet.title }}</h4>
        <button type="button" @click="onCopy(snippet)">复制</button>
      </div>
      <p class="hint">{{ snippet.note }}</p>
      <pre class="mono mcp-command">{{ snippet.text }}</pre>
    </div>
  </div>
</template>

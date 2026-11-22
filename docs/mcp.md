# B6 MCP 数据连接器（读 + 写）

本文说明腾手的 MCP 连接器：形态、工具、注册方式、安全边界与验收证据。
对应 bead `te-h7l.16`。

## 定位

腾手是**基层数据中心**：合同、相对方、字段、签名与模板版本由本机录入并
保存；外部部门要求的固定格式表由用户侧 agent 自行按格式填写。MCP 只负责
把结构化数据安全地交给用户侧 agent，**不解析、不生成、不代填外部表**，
也不做平台登录 / 自动填报。

## 形态与架构

MCP 连接器是 `tengshou-cli` 的 stdio 子命令，不是第二个数据库进程：

```
Claude Code / Claude Desktop / 用户侧 agent
        │  MCP（JSON-RPC over stdio）
        ▼
tengshou-cli mcp
        │  本机 HTTP（仅回环）
        ├─ GET  /api/v1/*         只读数据（令牌保护）
        └─ POST /app/api/v1/*     写操作（X-Tengshou-App + X-Tengshou-MCP）
                        │
                        ▼
             腾手桌面应用 / tengshou-cli serve
                        │  internal/service（校验、编号、变更记录）
                        ▼
                   SQLite（本机）
```

- MCP 进程**不打开 SQLite**；所有数据经由正在运行的本地 HTTP 服务。
- 因此使用前必须启动腾手桌面应用（或 `tengshou-cli serve`）。连不上时工具
  返回明确错误，不会静默失败。
- 配置中的端口被占用时，桌面应用会回退到后续 9 个端口；连接器在不带
  `--api-url` 时会自动探测这段端口。

## 快速开始

1. 启动腾手桌面应用（或在终端运行 `tengshou-cli serve`），确认数据目录。
2. 在 MCP 客户端注册下面的 stdio 命令：

```bash
tengshou-cli mcp --data-dir "/path/to/data"
```

`--data-dir` 用于读取 `config.json` 中的端口与只读数据接口令牌，不会打开
数据库。省略时使用平台默认数据目录：

| 平台 | 默认数据目录 |
| --- | --- |
| Windows | `%LOCALAPPDATA%\Tengshou` |
| macOS | `~/Library/Application Support/Tengshou` |
| Linux | `$XDG_DATA_HOME/tengshou` 或 `~/.local/share/tengshou` |

可选参数：

| 参数 / 环境变量 | 说明 |
| --- | --- |
| `--api-url` / `TENGHSHOU_API_URL` | 本机服务地址；默认 `http://127.0.0.1:<配置端口>`。只接受回环地址 |
| `--token` / `TENGHSHOU_API_TOKEN` | 只读接口令牌；默认读取 `config.json` 的 `http.token` |
| `--data-dir` | 数据目录；与全局参数含义一致 |

## 注册到 Claude Code

命令行注册（二进制路径换成实际位置）：

```bash
claude mcp add --transport stdio tengshou -- \
  /path/to/tengshou-cli mcp --data-dir "/path/to/data"
```

项目级 `.mcp.json`：

```json
{
  "mcpServers": {
    "tengshou": {
      "type": "stdio",
      "command": "/path/to/tengshou-cli",
      "args": ["mcp", "--data-dir", "/path/to/data"]
    }
  }
}
```

## 注册到 Claude Desktop

macOS 配置：`~/Library/Application Support/Claude/claude_desktop_config.json`

```json
{
  "mcpServers": {
    "tengshou": {
      "command": "/path/to/tengshou-cli",
      "args": ["mcp", "--data-dir", "/path/to/data"]
    }
  }
}
```

Windows 配置：`%APPDATA%\Claude\claude_desktop_config.json`

```json
{
  "mcpServers": {
    "tengshou": {
      "command": "C:\\Users\\<用户名>\\AppData\\Local\\Tengshou\\tengshou-cli.exe",
      "args": ["mcp", "--data-dir", "C:\\Users\\<用户名>\\AppData\\Local\\Tengshou"]
    }
  }
}
```

Windows 注意：

- JSON 中的反斜杠要写成 `\\`，且 **不会展开 `%LOCALAPPDATA%` 等环境变量**，
  请写完整路径；
- `tengshou-cli.exe` 放在哪里，`command` 就填哪里；`--data-dir` 指向桌面
  应用实际使用的数据目录（默认即 `C:\Users\<用户名>\AppData\Local\Tengshou`）；
- 连接只走本机回环，不触发 Windows 防火墙对外放行。

## 工具清单

工具 schema 由 MCP `tools/list` 直接提供（JSON Schema，`additionalProperties:
false`）。只读工具标注 `readOnlyHint=true`；写工具标注
`readOnlyHint=false, destructiveHint=false`。

### 只读工具

| 工具 | 参数 | 用途 |
| --- | --- | --- |
| `tengshou_status` | 无 | 服务是否可用、版本 / schema、统计、年度计数、MCP 写开关 |
| `tengshou_get_data_schema` | 无 | `tengshou.data.v1` 字段说明（机器可读） |
| `tengshou_list_fields` | `entity`（contract / party） | 动态字段定义：类型、必填、选项、默认值、台账 / 模板开关 |
| `tengshou_list_parties` | `q`、`limit`、`offset` | 搜索相对方档案 |
| `tengshou_get_party` | `id` | 单个相对方档案（含 `data`） |
| `tengshou_list_contracts` | `year`、`status`、`q`、`limit`、`offset` | 合同列表；`year` 省略即跨年查询 |
| `tengshou_get_contract` | `id` | 合同详情：双方、模板版本、签名资产、公章关联、PDF 记录 |
| `tengshou_list_signature_assets` | `contract_id`、`q`、`limit`、`offset` | 签名状态与 `content_sha256`（不含图片内容） |
| `tengshou_list_templates` | `q`、`limit`、`offset` | 模板列表与版本号（不含 HTML） |
| `tengshou_get_template` | `id` | 模板详情（含 HTML 与变量） |

### 写工具

| 工具 | 主要参数 | 用途 |
| --- | --- | --- |
| `tengshou_create_field` | `entity`、`key`、`label`、`type`、`required`、`default_value`、`options`、`in_ledger`、`in_template`、`note` | 新建动态字段，立即用于表单 / Excel / 模板 |
| `tengshou_create_party` | `name`、`kind`、`id_number`、`phone`、`address`、`note`、`data` | 新建相对方档案 |
| `tengshou_create_contract_draft` | `title`、`year`、`type`、`party_b_id` / `party_b`、`amount`、`template_id`、日期、`contract_no`、`data` | 新建合同草稿（状态固定 `draft`） |

写工具的共同约束：

- 全部复用桌面应用的 `internal/service` 校验、合同编号分配与变更记录；不接受
  绕过校验的裸 SQL。
- 变更记录 actor 固定为 `tengshou-mcp`（`X-Tengshou-MCP` 标记），与桌面 UI
  操作者、`config.json` 的 `operator` 区分。
- **不提供**删除、修改既有记录、替换公章、归档字段、批量导入等破坏性工具。
- 合同草稿只能以 `draft` 新建；生效 / 归档仍由用户在桌面应用内完成。

## 返回结构

`tools/call` 的 `result.content[0].text` 是 UTF-8 JSON（已缩进）：

- 列表：`{"items":[...],"total":n,"limit":50,"offset":0}`；
- 写操作：`{"item":{...},"warnings":["..."]}`；
- 工具执行错误：`result.isError=true`，文本为中文说明 + 本地接口原始错误
  JSON，例如：

```json
{
  "error": {
    "code": "validation",
    "message": "合同名称：不能为空",
    "fields": [{"field": "title", "label": "合同名称", "message": "不能为空"}]
  }
}
```

未知工具、未知方法等协议错误使用 JSON-RPC error（`-32602` / `-32601`），
不会伪装成工具结果。

## 写开关

`config.json`：

```json
{
  "mcp": {
    "write_enabled": true
  }
}
```

- 默认 `true`；旧配置没有 `mcp` 段时按开启处理。
- 关闭后：只读工具正常；写工具返回 HTTP 403 `mcp_write_disabled`，桌面 UI
  的写入不受影响。
- 配置在服务启动时读取，修改后需重启桌面应用 / `serve` 进程。

## 安全边界

- **仅本机 stdio + 回环 HTTP**：连接器只接受 `http://127.0.0.1:<端口>` /
  `localhost`；`--api-url` 传非回环地址会在启动时报错退出。不监听端口、不
  暴露公网。
- 只读请求走 `/api/v1`，带 `Authorization: Bearer <token>`；令牌来自
  `config.json`（0600），不打印、不写日志。
- 写请求走 `/app/api/v1`，必须同时满足：本机回环、`X-Tengshou-App: 1`、
  JSON 请求体，以及（MCP 请求）写开关开启。Windows / macOS 的桌面 UI 路径
  不受影响。
- MCP 进程的日志只写 stderr；stdout 仅承载 MCP 协议消息。
- 不解析、不生成、不代填外部台账模板；不做平台登录 / 自动填报。外部表由
  用户侧 agent 用本连接器提供的数据自行填写。

## 返回值示例

只读合同详情（节选）：

```json
{
  "contract": {
    "id": "ctr_...",
    "contract_no": "HT-2026-0001",
    "year": 2026,
    "title": "幸福村房屋租赁合同",
    "status": "active",
    "party_b_id": "pty_...",
    "data": {"rent": 1200}
  },
  "party_b": {"id": "pty_...", "kind": "person", "name": "张三"},
  "template": {"id": "tpl_...", "name": "租赁模板", "version": 1},
  "signatures": [
    {
      "signer_name": "张三",
      "content_sha256": "…",
      "captured_at": "2026-11-22T12:00:00Z",
      "asset_path": "assets/signatures/…png"
    }
  ],
  "seals": [],
  "documents": []
}
```

新建合同草稿：

```json
{
  "item": {
    "id": "ctr_...",
    "contract_no": "HT-2027-0001",
    "year": 2027,
    "title": "MCP 起草合同",
    "status": "draft",
    "party_b_id": "pty_..."
  },
  "warnings": ["已新建相对方档案「王五」，可在档案页补充信息"]
}
```

## 验收与排查

自动化测试：

```bash
go vet ./...
go test -count=1 ./...
go test -count=1 ./internal/mcp/ -v
```

`internal/mcp` 的测试会启动真实的本地 HTTP 服务（含中间件、service 校验与
变更记录），再通过 stdio 协议的进程内调用验证：

- 初始化、`tools/list`、`ping`、未知方法；
- 全部只读工具（合同 / 相对方 / 动态字段 / 签名状态 / 模板版本 / schema）；
- 建字段、建档案、建合同草稿；变更记录 actor = `tengshou-mcp`；
- 校验错误、重复键、未知参数、只读模式下写工具被拒绝；
- 写开关关闭时只读仍可用、桌面 UI 写入不受影响；
- 服务未启动时的清晰错误、端口回退探测、非回环地址拒绝。

手工冒烟：

```bash
tengshou-cli demo --data-dir /tmp/ts-mcp
tengshou-cli serve --data-dir /tmp/ts-mcp --port 18765 --loopback
# 另开终端：
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"tengshou_list_contracts","arguments":{"year":2026}}}' \
  | tengshou-cli mcp --data-dir /tmp/ts-mcp --api-url http://127.0.0.1:18765
```

常见问题：

| 现象 | 处理 |
| --- | --- |
| `无法连接腾手本地服务` | 启动桌面应用；确认 `--data-dir` 与桌面端一致；未带 `--api-url` 时会自动探测端口回退 |
| `401 unauthorized` | 令牌不匹配：使用同一数据目录，或显式传 `--token` |
| `403 mcp_write_disabled` | `config.json` 的 `mcp.write_enabled=false`；改为 `true` 后重启服务 |
| Windows 配置不生效 | 反斜杠写成 `\\`，使用完整路径，确认 `command` 指向 `tengshou-cli.exe` |
| 工具列表为空 | 客户端未完成 `initialize`，或连接的是其它 MCP 服务 |

## 明确不做

- 不解析 / 生成 / 代填外部部门表格；不内置外部表模板引擎。
- 不做平台登录、自动填报、爬取或对外提交。自动填报是未来付费插件。
- 不暴露删除 / 修改 / 批量破坏性写接口；写接口默认只到「新建草稿」。
- 不通过局域网或公网提供 MCP 服务；只有本机 stdio 客户端可用。

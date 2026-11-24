# B6 MCP 数据连接器（读 + 写 + 生成）

本文说明腾手的 MCP 连接器：形态、工具、注册方式、安全边界与验收证据。
对应 bead `te-h7l.16`；合同 PDF 生成与文档查询工具对应 `te-h7l.49`。

## 定位

腾手是**基层数据中心**：合同、相对方、字段、签名与模板版本由本机录入并
保存；外部部门要求的固定格式表由用户侧 agent 自行按格式填写。MCP 只负责
把结构化数据安全地交给用户侧 agent，**不解析、不生成、不代填外部表**，
也不做平台登录 / 自动填报。

## 形态与架构

MCP 连接器内置于桌面客户端：`tengshou-windows-amd64.exe mcp` 就是 stdio
连接器，客户只需要这一个 exe。`tengshou-cli mcp` 调用**同一份实现**
（`internal/mcpconn`），作为独立发布资产保留给高级用途——开发、测试与
发布走同一条代码路径，不存在两份连接器。

```
Claude Code / Claude Desktop / WorkBuddy / 用户侧 agent
        │  MCP（JSON-RPC over stdio）
        ▼
tengshou-windows-amd64.exe mcp   （桌面客户端内置，客户交付形态）
tengshou-cli mcp                 （同一实现，独立 CLI 资产）
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
- 桌面客户端带 `mcp` 参数启动时**不初始化窗口、不监听新端口、不打开
  数据库**，只把 stdin/stdout 接到共享连接器；桌面应用本体的行为不变。

## 快速开始

1. 启动腾手桌面应用（或在终端运行 `tengshou-cli serve`），确认数据目录。
2. 在 MCP 客户端注册下面的 stdio 命令。各平台交付形态不同，请按下表选择：

| 平台 | 连接器命令 | 说明 |
| --- | --- | --- |
| Windows | `tengshou-windows-amd64.exe mcp` | 分发包内桌面 exe（GUI + `mcp` 两种用法），客户唯一需要下载的文件 |
| macOS | `tengshou-darwin-arm64 mcp` | 桌面二进制，内置 `mcp` |
| Linux | `tengshou-cli mcp` | Linux 不构建桌面壳，连接器使用独立 CLI 资产 |

```bash
tengshou-windows-amd64.exe mcp --data-dir "C:\Users\<用户名>\AppData\Local\Tengshou"
# 等价：tengshou-cli mcp --data-dir "/path/to/data"
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

命令行注册（二进制路径换成实际位置；桌面 exe 与 `tengshou-cli` 等价）：

```bash
claude mcp add --transport stdio tengshou -- \
  /path/to/tengshou-darwin-arm64 mcp --data-dir "/path/to/data"
```

项目级 `.mcp.json`：

```json
{
  "mcpServers": {
    "tengshou": {
      "type": "stdio",
      "command": "/path/to/tengshou-darwin-arm64",
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
      "command": "/path/to/tengshou-darwin-arm64",
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
      "command": "C:\\Users\\<用户名>\\tengshou\\tengshou-windows-amd64.exe",
      "args": ["mcp", "--data-dir", "C:\\Users\\<用户名>\\AppData\\Local\\Tengshou"]
    }
  }
}
```

Windows 注意：

- JSON 中的反斜杠要写成 `\\`，且 **不会展开 `%LOCALAPPDATA%` 等环境变量**，
  请写完整路径；
- `command` 指向解压后的 `tengshou-windows-amd64.exe`（桌面客户端本身
  就是连接器）；`--data-dir` 指向桌面应用实际使用的数据目录（默认即
  `C:\Users\<用户名>\AppData\Local\Tengshou`）；
- 连接只走本机回环，不触发 Windows 防火墙对外放行。

## Windows 分发包说明

Windows 预发布版提供一个分发包 `tengshou-windows-amd64.zip`，解压后含
2 个文件：

| 文件 | 用途 |
| --- | --- |
| `tengshou-windows-amd64.exe` | 腾手客户端：桌面应用 + MCP 连接器（`mcp` 子命令） |
| `MCP-接入.txt` | 客户向接入说明：启动顺序、Claude Desktop 配置示例、默认数据目录 |

两条使用路径：

- **只用桌面应用**：双击 `tengshou-windows-amd64.exe`。
- **接入 MCP**：先启动桌面应用，再让 agent 启动
  `tengshou-windows-amd64.exe mcp --data-dir "<数据目录>"`。连接器不
  打开数据库，只连本机正在运行的腾手服务；桌面应用未启动时工具返回明确的
  连接错误。

默认数据目录为 `C:\Users\<用户名>\AppData\Local\Tengshou`
（即 `%LOCALAPPDATA%\Tengshou`）。Claude Desktop 配置见上一节的 Windows
示例：`command` 填桌面 exe 的完整路径，反斜杠写成 `\\`。

发布固定 8 个资产：除上述 zip 外，仍保留独立的 Windows 桌面 exe 与 CLI
exe，以及 macOS / Linux 产物，不因 zip 内容调整而增减资产。独立 CLI
（`serve` / `backup` / `restore` / `demo` 等高级用途）不在 zip 内，客户
只需要桌面 exe。

本地复现同一个包（Windows，dev 与 CI 调用同一脚本）：

```powershell
make frontend
powershell -File scripts/build-windows.ps1
bash scripts/package-windows-zip.sh dist   # Git Bash；需要 python3
```

`unzip -l dist/tengshou-windows-amd64.zip` 应只列出上表中的 2 个文件；
CI 的 tag 发布任务用同一脚本打包，并在发布前断言 zip 内容与资产总数。

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
| `tengshou_list_templates` | `q`、`limit`、`offset` | 模板列表与版本号（HTML / PDF 两类；不含正文） |
| `tengshou_get_template` | `id` | 模板详情（含 kind、变量；HTML 模板含正文，PDF 模板含 pdf_path / page_count / placements） |
| `tengshou_list_documents` | `contract_id`、`status`、`limit`、`offset` | 已生成 PDF 记录：合同编号、模板名称与版本、生成类型（initial / regenerate）、状态、时间与下载路径 |
| `tengshou_preflight_generation` | `template_id`、`contract_id` | 生成前只读预检：`ready`、`issues`、`warnings`、`preview_sha256`、`signatures`、`previous_documents` |

### 写工具

| 工具 | 主要参数 | 用途 |
| --- | --- | --- |
| `tengshou_create_field` | `entity`、`key`、`label`、`type`、`required`、`default_value`、`options`、`in_ledger`、`in_template`、`note` | 新建动态字段，立即用于表单 / Excel / 模板 |
| `tengshou_create_party` | `name`、`kind`、`id_number`、`phone`、`address`、`note`、`data` | 新建相对方档案 |
| `tengshou_create_contract_draft` | `title`、`year`、`type`、`party_b_id` / `party_b`、`amount`、`template_id`、日期、`contract_no`、`data` | 新建合同草稿（状态固定 `draft`） |
| `tengshou_generate_contract` | `template_id`、`contract_id`、`allow_stale_signature`、`kind` | 套用 PDF 模板生成合同文件；内部先预检，未通过不写记录、不产文件 |

写工具的共同约束：

- 全部复用桌面应用的 `internal/service` 校验、合同编号分配与变更记录；不接受
  绕过校验的裸 SQL。
- 变更记录 actor 固定为 `tengshou-mcp`（`X-Tengshou-MCP` 标记），与桌面 UI
  操作者、`config.json` 的 `operator` 区分。
- **不提供**删除、修改既有记录、替换公章、归档字段、批量导入等破坏性工具。
- 合同草稿只能以 `draft` 新建；生效 / 归档仍由用户在桌面应用内完成。

### 生成工具的共同约束

- `tengshou_generate_contract` 内部先执行与服务端一致的预检：未通过时返回
  `isError=true` 并逐条列出阻断项，**不写 `output_documents` 记录、不产生
  文件**；通过后才用预检返回的 `preview_sha256` 调用生成接口（服务端要求
  回传该摘要，防止预览后数据被改动）。
- `allow_stale_signature` 默认 `false`。只有用户明确确认「合同内容未实质
  变化、可复用旧签名」时才可置 `true`；它只放行 `content_changed`，其余
  阻断项仍然生效，确认行为会写入变更记录。
- 生成成功后合同可能被固定为所选模板版本；不提供签名采集、合同激活、
  删除或修改既有记录的工具。

## 典型 agent 流程

1. `tengshou_list_templates` / `tengshou_get_template`：确认 PDF 模板与版本。
2. `tengshou_get_contract`：确认合同字段、相对方、签名状态与已生成记录。
3. `tengshou_create_contract_draft`（如尚无草稿）：录入合同数据。
4. `tengshou_preflight_generation`：确认 `ready=true`；否则把 `issues`
   转告用户先行处理。
5. `tengshou_generate_contract`：生成并返回 `{item, warnings}`。
6. `tengshou_list_documents`：核对记录；成功记录的 `download_path`
   （`/app/api/v1/documents/{id}/file`）可交给用户下载 / 打印。

签名采集、合同激活、删除或修改既有记录不在 MCP 范围内；遇到时提示用户
在桌面应用内完成。

## 生成被阻断：排查表

`tengshou_preflight_generation` 与 `tengshou_generate_contract` 的
`issues[].code` 对应下列阻断项。所有处理都在桌面应用内由用户完成，
agent 不代为签名或激活。

| 阻断项 code | 含义 | 人工处理方式 |
| --- | --- | --- |
| `missing_signature` | 模板有签名位，但合同没有对应签名，或签名图片文件缺失 | 打开该合同，用手机扫码重新采集签名后再试 |
| `missing_seal` | 模板有公章位，但没有可用公章，或公章图片文件缺失 | 在「公章」页导入 / 重新导入公章，并在模板标定中指定 |
| `content_changed` | 签名采集后合同内容被修改，旧签名不能自动复用 | 重新采集签名；若确认内容未实质变化且用户明确同意，可用 `allow_stale_signature=true` |
| `missing_field` | 合同或相对方的必填字段为空 | 补齐字段后重新预检 |
| `missing_party` | 合同缺少相对方（乙方）档案，或模板引用甲方但合同未关联甲方 | 关联或新建相对方档案，或补齐甲方信息 |
| 其它 `code` | 服务端新增的阻断项 | 按 `message` 提示处理，不要绕过预检直接生成 |

## 返回结构

`tools/call` 的 `result.content[0].text` 是 UTF-8 JSON（已缩进）：

- 列表：`{"items":[...],"total":n,"limit":50,"offset":0}`；
- 预检：直接返回服务端预检报告（`ready`、`issues`、`warnings`、
  `preview_sha256`、`signatures`、`previous_documents` 等）；
- 写操作：`{"item":{...},"warnings":[...]}`；生成 PDF 时 `item` 为新增的
  `output_documents` 记录，`warnings` 为版式提示对象数组；
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
- 关闭后：只读工具（含生成预检与文档查询）正常；写工具（含生成合同 PDF）
  返回 HTTP 403 `mcp_write_disabled`，桌面 UI 的写入不受影响。预检虽然走
  POST，但属于只读动作，不带 `X-Tengshou-MCP` 标记，因此不受写开关影响。
- **即时生效（B18）**：桌面应用「设置」页提供读写开关。写开关由 HTTP 服务的
  运行时设置持有，切换后下一次请求立即按新值判定；连接器在每次写工具调用前
  也会重新读取 `config.json`，因此无需重启 MCP 进程或桌面应用。
- 开关变更本身会以 `entity=settings`、actor=当前操作者写入变更记录；MCP
  agent 不能通过接口修改该开关（返回 403 `settings_ui_only`）。
- 状态查询：`GET /app/api/v1/mcp/status` 返回写开关、读/写工具数、agent
  操作次数与连接说明；`PUT|POST /app/api/v1/settings/mcp` 仅接受本机界面
  （回环 + `X-Tengshou-App: 1`）修改 `{"write_enabled": true|false}`。

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

预检通过后生成合同 PDF（节选）：

```json
{
  "item": {
    "id": "doc_...",
    "contract_id": "ctr_...",
    "template_id": "tpl_...",
    "template_version": 2,
    "file_path": "outputs/ctr_...-v2-20261124-080000-123456.pdf",
    "sha256": "…",
    "page_count": 1,
    "operator": "tengshou-mcp",
    "kind": "initial",
    "status": "success",
    "created_at": "2026-11-24T08:00:00Z"
  },
  "warnings": []
}
```

查询已生成文档：

```json
{
  "items": [
    {
      "id": "doc_...",
      "contract_id": "ctr_...",
      "contract_no": "HT-2026-0001",
      "template_id": "tpl_...",
      "template_name": "租赁模板",
      "template_version": 2,
      "kind": "initial",
      "status": "success",
      "created_at": "2026-11-24T08:00:00Z",
      "download_path": "/app/api/v1/documents/doc_.../file"
    }
  ],
  "total": 1,
  "limit": 50,
  "offset": 0
}
```

预检阻断时 `tengshou_generate_contract` 返回 `isError=true`，文本示例：

```
生成前置检查未通过（1 项），未生成文件、未写入 PDF 记录：
- [missing_signature] 签名位置「sig」没有乙方签名，请先扫码采集
请转告用户先在腾手桌面应用内处理这些问题，然后重试。
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
- 预检通过 → 生成 PDF → 文档可查询、可经 `download_path` 下载；变更记录
  actor = `tengshou-mcp`；
- 预检阻断（模板含签名位但合同未签名）→ 生成返回 `isError`，且不产生
  `output_documents` 记录；
- `tengshou_list_documents` 的合同 / 状态过滤与分页；
- 写开关关闭时预检与文档查询仍可用、生成返回 403 `mcp_write_disabled`；
- 校验错误、重复键、未知参数、只读模式下写工具被拒绝；
- 写开关关闭时只读仍可用、桌面 UI 写入不受影响；
- 服务未启动时的清晰错误、端口回退探测、非回环地址拒绝。

`internal/mcpconn` 的测试从桌面与 CLI 共用的 `Run` 出发，用真实本地服务
验证 initialize、`tools/list`、只读与写工具，并锁定非回环地址拒绝——两个
入口只有这一条实现。

发布形态冒烟（CI 的 windows-latest 与 macos-15，二进制即发布形态）：

```bash
# Windows（GUI 子系统 exe，真实 stdin/stdout 管道）
powershell -File scripts/smoke-mcp-desktop.ps1 \
  -Desktop dist\tengshou-windows-amd64.exe -Cli dist\tengshou-cli-windows-amd64.exe
# macOS
bash scripts/smoke-mcp-desktop.sh dist/tengshou-darwin-arm64 dist/tengshou-cli-darwin-arm64
```

两步都用桌面二进制：先在本地服务未启动时断言 `initialize` / `tools/list`，
再启动同版 `tengshou-cli serve` 后调用一次写工具成功。

手工冒烟（桌面二进制；发布资产名为 `dist/tengshou-darwin-arm64`，
本地默认 `./scripts/build.sh` 的产物为 `dist/tengshou`）：

```bash
tengshou-cli demo --data-dir /tmp/ts-mcp
tengshou-cli serve --data-dir /tmp/ts-mcp --port 18765 --loopback
# 另开终端：
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"tengshou_list_contracts","arguments":{"year":2026}}}' \
  | ./dist/tengshou mcp --data-dir /tmp/ts-mcp --api-url http://127.0.0.1:18765
```

常见问题：

| 现象 | 处理 |
| --- | --- |
| `无法连接腾手本地服务` | 启动桌面应用；确认 `--data-dir` 与桌面端一致；未带 `--api-url` 时会自动探测端口回退 |
| `401 unauthorized` | 令牌不匹配：使用同一数据目录，或显式传 `--token` |
| `403 mcp_write_disabled` | `mcp.write_enabled=false`；在桌面应用「设置」页打开写开关即即时生效（也可修改 `config.json`） |
| Windows 配置不生效 | 反斜杠写成 `\\`，使用完整路径，确认 `command` 指向 `tengshou-windows-amd64.exe`（桌面客户端内置 mcp） |
| 工具列表为空 | 客户端未完成 `initialize`，或连接的是其它 MCP 服务 |

## 明确不做

- 不解析 / 生成 / 代填外部部门表格；不内置外部表模板引擎。
- 不做平台登录、自动填报、爬取或对外提交。自动填报是未来付费插件。
- 不暴露删除 / 修改 / 批量破坏性写接口；写接口只到「新建草稿」与
  「生成合同 PDF」，不提供签名采集、合同激活或替换公章。
- 不代填外部部门表格；不承诺自动填报。
- 不通过局域网或公网提供 MCP 服务；只有本机 stdio 客户端可用。

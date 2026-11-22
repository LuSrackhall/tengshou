# B18 实时同步、审计可见与 MCP 状态面板

对应 bead `te-h7l.31`。目标：MCP / agent 从外部改数据后，界面无需重启或刷新
即可反映；合同 / 档案详情能看到谁改了什么并区分人工与 Agent；设置页可查看
MCP 状态并切换读/写开关，且开关即时生效。

## 1. 变更事件流

### 后端

- `GET /app/api/v1/events`（`text/event-stream`，只读、仅回环）。
- 数据源是 append-only 的 `change_log`，不新增表、不引入第三方依赖（只用
  标准库 `net/http` + `time.Ticker`）。
- 游标：`?after=<change_log id>` 或重连时浏览器自动携带的 `Last-Event-ID`。
  `created_at` 只到秒，同一秒内可能有多条记录，因此服务端用隐式 `rowid`
  保证严格追加顺序；对外的游标仍是变更记录 id（也是 SSE 的 `id:`）。
- 事件负载：

```json
{"id":"chg_...","entity":"contract","entity_id":"ctr_...","action":"create",
 "actor":"tengshou-mcp","summary":"新建合同...","created_at":"2026-11-22T16:05:15Z"}
```

- 每 15 秒发送 `: keep-alive` 注释帧；新连接若没有游标，则从“当前最新”开始，
  不回放历史（页面打开时已通过普通接口加载数据）。
- SSE 是长连接：handler 用 `http.NewResponseController(w).SetWriteDeadline
  (time.Time{})` 清除服务级 `WriteTimeout`（该超时是绝对的，心跳不能续期）；
  `statusRecorder` 增加 `Unwrap` 让控制器能穿过日志中间件。

### 前端

- 单例 `EventSource`；收到事件后按 `entity` 合并、350ms 去抖，再增量刷新：
  合同台账、相对方、字段（含表头列）、公章、模板、变更记录页与设置页。
- 不整页 reload，不重建正在编辑的弹窗表单；若事件命中当前打开的合同 / 档案，
  只刷新弹窗底部的「变更记录」区块并提示。
- 降级：EventSource 报错（桌面 WebView / 代理阻断）时自动启动 3 秒轮询
  `GET /app/api/v1/changelog?after=<id>`，页面隐藏（`document.hidden`）时暂停；
  恢复可见时立即补拉。顶栏 `liveStatus` 显示「实时同步 / 轮询同步 / 同步中断」。

## 2. 审计可见

- `GET /app/api/v1/changelog` 新增筛选：
  - `entity` / `entity_id`（原有）；
  - `source=agent|human`：`agent` 等价 actor=`tengshou-mcp`，`human` 排除它；
  - `after=<id>`：只返回该记录之后追加的条目（升序，供轮询降级使用）。
- 合同 / 相对方编辑弹窗底部新增「变更记录」：时间、来源、动作、摘要与字段
  `old → new`（`model.ChangeLog.Changes`）。
- 变更记录页：
  - 顶部「最近操作」条（全局最近 8 条）；
  - 对象类型筛选增加「设置」；
  - 来源筛选（全部 / 人工 / Agent（MCP））；
  - 每条记录显示来源徽章，Agent 使用紫色 `Agent（MCP）` 标签。
- 来源判定：`actor == "tengshou-mcp"`（`service.MCPActor`）为 Agent，其余为
  人工。写入路径沿用 `service.logChange`，未改变 append-only 语义。

## 3. MCP 状态与读写开关

### 运行时设置

`config.Settings` 是进程内的实时配置（`sync.RWMutex`），启动时由
`config.NewSettings(paths, cfg)` 创建，注入 `httpapi.Options.Settings`。
`mcp.write_enabled` 不再只读启动快照：

- `withAppGuard` 每个请求读取 `Settings.MCPWriteEnabled()`；
- `GET /app/api/v1/bootstrap` 与 `/app/api/v1/mcp/status` 返回实时值；
- `SetMCPWriteEnabled` 先 `config.Save`（原子写、0600）成功后再更新内存，
  失败不会出现“界面显示已关闭、重启后又打开”。

MCP stdio 连接器是独立进程，`tengshou-cli mcp` 在每次写工具调用前通过
`config.MCPWriteGate` 重新读取 `config.json`；HTTP 服务仍是权威门禁。

### 接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/app/api/v1/events` | 变更事件流（SSE） |
| GET | `/app/api/v1/mcp/status` | 写开关、读/写工具数、近 24h agent 操作数、最近一次操作、数据目录与注册命令 |
| PUT/POST | `/app/api/v1/settings/mcp` | `{"write_enabled": true\|false}`；仅本机界面（回环 + `X-Tengshou-App: 1`），带 `X-Tengshou-MCP` 的 agent 请求返回 403 `settings_ui_only` |

- 关闭只影响写：MCP 读工具正常，桌面 UI 写入正常；MCP 写返回既有
  `403 mcp_write_disabled`。
- 开关变更以 `entity=settings`、`entity_id=mcp`、`action=update`、actor=当前
  操作者写入 `change_log`，并在 `changes` 中记录 `开启 → 关闭`。
- 设置页只展示状态与开关，不启动 / 停止 MCP 进程，也不提供删除类操作。

## 4. 自动化测试

```bash
go vet ./...
go test -count=1 ./...
go test -count=1 ./internal/httpapi/ -run 'EventStream|ChangeLogAfter|MCPSettings' -v
go test -count=1 ./internal/mcp/ -run 'WriteGate|ToolCounts' -v
```

覆盖点：

- `internal/store`：同一秒内的多条记录按 rowid 追加顺序返回；未知游标回退到
  最近一页；actor / since 过滤。
- `internal/httpapi`：`?after=` 只返回新条目；SSE 首帧类型、按游标补发与实时
  新事件；写开关关闭后 MCP 写 403、读 200、桌面写 201、配置落盘、设置变更
  自带变更记录、来源筛选、重新开启后立即恢复。
- `internal/mcp`：工具数量与 `mcpspec` 一致；连接器在开关关闭时短路写工具
  并返回 `mcp_write_disabled`，读工具不受影响。

## 5. 手工验收

```bash
# 1) 准备演示数据并启动（另开终端保持运行）
tengshou-cli demo  --data-dir /tmp/ts-b18
tengshou-cli serve --data-dir /tmp/ts-b18 --port 18765 --loopback

# 2) 浏览器打开
#    http://127.0.0.1:18765/app/
```

1. **agent 建字段即时出现**：保持「合同台账」页打开，在另一终端运行：

   ```bash
   printf '%s\n' \
     '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"b18","version":"0"}}}' \
     '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
     '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"tengshou_create_field","arguments":{"entity":"contract","key":"b18_area","label":"B18 面积","type":"number","in_ledger":true}}}' \
     | tengshou-cli mcp --data-dir /tmp/ts-b18 --api-url http://127.0.0.1:18765
   ```

   不刷新页面：台账表头应出现「B18 面积」；顶栏状态显示「实时同步」。
   若事件流被阻断，状态显示「轮询同步」，最多约 3 秒后出现。

2. **agent 建档案 / 合同草稿**：调用 `tengshou_create_party` 与
   `tengshou_create_contract_draft`，相对方页与合同台账（全部年度）自动出现
   新记录。

3. **审计区分来源**：打开任一新记录的「编辑」弹窗，底部「变更记录」显示
   `Agent（MCP）` 徽章、时间、摘要与字段差异；「变更记录」页顶部「最近操作」
   与来源筛选可只看 Agent 或人工。人工在界面修改同一记录后，再看到「人工」
   来源条目。

4. **MCP 面板**：打开「设置」页，应看到服务状态、读工具 10 个、写工具 3 个、
   累计 / 近 24h agent 操作数与最近一次操作，以及注册命令。

5. **关闭写开关即时生效**：取消勾选「写工具」并确认；随后再次运行上面的
   `tengshou_create_party`，应返回 `mcp_write_disabled`；`tengshou_list_contracts`
   仍正常；桌面界面新建合同仍成功。`config.json` 中
   `mcp.write_enabled` 应为 `false`。重新打开开关后写工具立即恢复。

6. **事件流冒烟**（可选）：

   ```bash
   curl -N 'http://127.0.0.1:18765/app/api/v1/events'
   # 应看到 retry 帧与每 15 秒一次的 ": keep-alive"；触发一次写入后出现 id/data 帧
   ```

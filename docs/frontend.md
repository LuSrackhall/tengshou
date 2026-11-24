# 前端工程（Vite + Vue 3 + TypeScript）

阶段 1 收口后，整个界面由一个 Vue 应用渲染：`frontend/src/main.ts` 挂载
`App.vue`，`internal/httpapi/web/app/index.html` 只保留 `#app` 宿主、本地
pdf.js vendor 与 Vite 产物。存量 `app.js` 已退役，不得重新引入。

## 目录

```
frontend/                         # 前端工程（Vite root）
  package.json / package-lock.json
  tsconfig.json                   # strict，vue-tsc --noEmit
  vite.config.ts                  # lib 模式，固定产物名
  index.html                      # 仅开发预览页，不参与生产构建
  scripts/clean-dist.mjs          # 清理产物，保留入库的 dist/.keep
  src/
    main.ts                       # 生产入口：挂载 App.vue
    App.vue                       # 外壳：顶栏/tab/统计/toast/视图宿主
    dev.ts                        # 开发预览入口（额外引入真实 app.css）
    api/client.ts                 # fetch 封装：base/错误信封/query/超时归类
    api/endpoints.ts              # typed 端点调用面（按域拆分到 endpoints/）
    api/types.ts                  # API 契约类型入口（按域拆分到 types/）
    api/README.md                 # 类型 ↔ Go 源码对照、端点覆盖清单
    bridge/app.ts                 # 跨域事件、toast/刷新/切 tab 的统一入口
    bridge/live-status.ts         # 顶栏同步状态桥（realtime → 徽标）
    shell/                        # tab 状态、事件契约、SSE/轮询调度
    ui/                           # toast、modal/backdrop、格式化、下载
    components/                   # 按域拆分的 Vue 视图与弹窗
internal/httpapi/web/app/dist/    # Vite 产物（tengshou-ui.js / .css）
```

## 本地构建

```bash
cd frontend
npm ci                # 按 lockfile 安装（首次或依赖变更后）
npm run typecheck     # vue-tsc --noEmit，strict
npm run build         # 清理 dist（保留 .keep）后 vite build
```

产物固定为 `internal/httpapi/web/app/dist/tengshou-ui.js` 与
`tengshou-ui.css`，由 `internal/httpapi/appstatic.go` 的
`//go:embed ... all:web/app/dist` 打进单二进制，运行时不访问公网、不依赖
CDN。`index.html` 只加载本地 pdf.js vendor 与 module 入口。

一键构建（前端 + Go）：

```bash
make build            # 先构建前端，再执行 go run ./tools/tengshoubuild build
make serve            # 先构建前端，再启动本地服务
```

直接运行 `go run ./tools/tengshoubuild build` 时请先执行 `make frontend`
（或上面的 npm 命令）。

### 没有 node 时

`dist/.keep` 入库，保证 `go build` / `go test ./...` 在干净 clone、未安装
node 的机器上仍然可编译（此时没有界面产物，页面需要前端构建后才完整）。
**发布产物不受影响**：CI 的 `frontend` job 始终执行 `npm ci`、类型检查与
构建，`cli` / `desktop` 构建 job 会先取回该产物再 `go build`。

## 开发预览

```bash
cd frontend
npm run dev           # 打开提示的地址，页面里可切换同步状态各档位
```

预览页直接引用仓库里真实的 `app.css`，观感与正式页面一致；它只用于开发
同步状态徽标，不参与生产构建（`vite build` 只以 `src/main.ts` 为入口）。
完整界面通过 `make serve` 后打开 `/app/` 查看。

## agent 操作可见层

MCP agent（`tengshou-mcp`）写入数据时，`components/shell/AgentActivityOverlay.vue`
监听 `tengshou:change`，自动切到实体对应的页面，并用模拟光标、点击波纹与
目标行高亮回放这次操作，顶部同时显示「agent 操作」徽标。它是纯视觉层
（叠加层 `pointer-events: none`，不抢焦点、不改 `shell/realtime.ts` 的派发
语义）；设置页可关闭动画（默认开启，`localStorage` 持久化），
`prefers-reduced-motion: reduce` 下只保留行高亮与徽标。

## API 客户端与类型

阶段 1 起，所有 `/app/api/v1` 调用统一走 `frontend/src/api/`：

- `client.ts`：唯一 fetch 封装。固定 base `/app/api/v1`，自动带
  `X-Tengshou-App: 1`，JSON 请求/响应，`buildQuery` 拼 query（省略
  `undefined`/`null`/空串），`encodePathSegment` 编码路径段，支持
  `AbortSignal` 与可选 `timeoutMs`，并导出 typed `get/post/put/del`
  以及 `getFile/postFile/postForm`（下载、签名导出、备份上传）。
- `endpoints.ts`（按域拆到 `endpoints/`）：每个后端端点一个 typed 函数，
  调用方只从 `api/endpoints` 引入；组件里禁止裸 `fetch`。
- `types.ts`（按域拆到 `types/`）：请求体与响应契约。动态字段值用
  `Record<string, unknown>`，禁止 `any` 兜底（例外须在调用点写明原因）。

### 错误处理

所有失败抛 `ApiError`（`code`/`status`/`fields`/`payload`）：

- 服务端错误信封 `{"error":{"code","message","fields?"}}` 原样映射，
  `fields` 用于表单逐字段提示；
- 连接失败归为 `network`、超过 `timeoutMs` 归为 `timeout`，状态码为 0；
- 成功响应无法解析归为 `invalid_response`；
- 调用方自己的 `AbortSignal` 触发时原样抛 `AbortError`，便于区分取消。

`timeoutMs` 默认不设：生成/导入/备份等长任务不要设超时。Excel 提交的
422 响应同时带 `error` 与 `result`，从 `ApiError.payload` 取
`ImportCommitErrorPayload` 展示逐行错误。

### 契约变更流程

1. 先改 Go 结构或 handler 响应；
2. 先改 `api/types/` 对应类型与 `api/endpoints/` 函数签名，再改调用点；
3. `npm run typecheck` 必须通过；禁止用 `any` 掩盖不一致；
4. 在 PR 说明里记录契约变化。

逐字段核对过的 Go 文件清单、端点覆盖表与序列化约定（int/time/
omitempty/nil slice）见 `frontend/src/api/README.md`。单文件 ≤ 600 行的
预算同样适用于 `api/` 下的 TS 文件，因此类型与端点按域拆分，入口保持
barrel 形式，调用点无需感知拆分。


## CI 与结构预算

`.github/workflows/ci.yml` 的 `frontend` job：

1. `bash scripts/check-frontend-structure.sh`（app.js 退役检查 + 单文件 ≤ 600 行）；
2. `npm ci` → `npm run typecheck` → `npm run build`；
3. 校验 `tengshou-ui.js` / `tengshou-ui.css` 存在并上传 `frontend-dist` 产物；
4. `cli` / `desktop` job 依赖该 job，下载产物后再 `go build`（`go:embed`
   要求编译期目录存在）。

存量 `app.js` 已退役：`scripts/check-frontend-structure.sh` 在它重新出现时
直接失败；新功能一律落 `frontend/src/` 的 Vue 组件。

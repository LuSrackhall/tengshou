# 前端工程（阶段 0：Vite + Vue 3 + TypeScript）

阶段 0 的目标是先把构建管线立起来，不迁移业务视图：界面仍由存量
`internal/httpapi/web/app/app.js` 驱动，Vue 先以一个最小「岛」（顶栏同步状态
徽标）接入，新旧入口共存。后续阶段按域把 `app.js` 逐块迁到 Vue 组件。

## 目录

```
frontend/                         # 前端工程（Vite root）
  package.json / package-lock.json
  tsconfig.json                   # strict，vue-tsc --noEmit
  vite.config.ts                  # lib 模式，固定产物名
  index.html                      # 仅开发预览页，不参与生产构建
  scripts/clean-dist.mjs          # 清理产物，保留入库的 dist/.keep
  src/
    main.ts                       # 生产入口：挂载 Vue 岛
    dev.ts                        # 开发预览入口（额外引入真实 app.css）
    api/types.ts                  # 首批 API 契约类型
    bridge/live-status.ts         # app.js ↔ Vue 岛的状态桥
    components/LiveStatusBadge.vue
    styles/island.css
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
CDN。`index.html` 同时保留旧 `app.js` 与新的 module 入口。

一键构建（前端 + Go）：

```bash
make build            # 先构建前端，再执行 scripts/build.sh
make serve            # 先构建前端，再启动本地服务
```

直接运行 `scripts/build.sh` 时请先执行 `make frontend`（或上面的 npm 命令）。

### 没有 node 时

`dist/.keep` 入库，保证 `go build` / `go test ./...` 在干净 clone、未安装
node 的机器上仍然可编译（只是没有 Vue 岛产物：徽标不显示，其余界面不受
影响）。**发布产物不受影响**：CI 的 `frontend` job 始终执行 `npm ci`、
类型检查与构建，`cli` / `desktop` 构建 job 会先取回该产物再 `go build`。

## 开发预览

```bash
cd frontend
npm run dev           # 打开提示的地址，页面里可切换同步状态各档位
```

预览页直接引用仓库里真实的 `app.css`，观感与正式页面一致；它只用于开发已
迁移的岛，不参与生产构建（`vite build` 只以 `src/main.ts` 为入口）。
完整界面仍通过 `make serve` 后打开 `/app/` 查看。

## API 契约类型

`frontend/src/api/types.ts` 首批覆盖 Bootstrap、Contract、Party、Seal、
Template、Field、Onboarding、Stats 等，逐字段对照：

- `internal/model/model.go`
- `internal/httpapi/appapi.go`（bootstrap 响应）
- `internal/httpapi/onboarding.go`
- `internal/service/onboarding.go`

契约变更时先改类型再改调用点；禁止用 `any` 兜底（确需例外时在代码里写明
原因）。

## CI 与结构预算

`.github/workflows/ci.yml` 的 `frontend` job：

1. `bash scripts/check-frontend-structure.sh`（app.js 基线棘轮 + 单文件 ≤ 600 行）；
2. `npm ci` → `npm run typecheck` → `npm run build`；
3. 校验 `tengshou-ui.js` / `tengshou-ui.css` 存在并上传 `frontend-dist` 产物；
4. `cli` / `desktop` job 依赖该 job，下载产物后再 `go build`（`go:embed`
   要求编译期目录存在）。

存量 `app.js` 只许减不许增：迁移代码时同步删除旧逻辑；显著缩小后下调
`scripts/check-frontend-structure.sh` 里的 `APP_JS_BASELINE`，保持棘轮单调下降。

# 腾手（tengshou）仓库工程约定

本文件对本仓库内的所有代码改动生效，Claude 会话会自动加载。
本文件只写仓库自己的工程标准；评审与合入流程以 Gas City 官方公式
（`implement` / `review` / `publish`）为准，不在本文件重复。

## 通用约束

- **离线单二进制不变**：前端产物通过 `go:embed` 打进 Go 二进制，发布物仍是
  单个可执行文件；运行时不访问公网、不依赖 CDN 或任何外部服务。
- **无运行时前端依赖**：生产构建不引入需要联网获取的运行时依赖；第三方库
  一律随仓库锁定版本（vendor 或构建期打包）。

## CodeGraph（可选本地工具）

CodeGraph 是可选的本地开发工具，未安装不影响构建、测试与运行。
`.codegraph/` 索引自忽略、不入库；本地用 `codegraph init --local-only -y`
建立、`codegraph sync` 更新。仓库根 `codegraph.json` 的 `worktrees/`、
`.gc/` 排除与 `.gitignore` 是有意的双保险（防 `.gitignore` 变动后重新
索引这两个目录）；日常新增排除目录改 `.gitignore` 即可。

## Go 工程约定

- **格式**：`gofmt -l ./cmd ./internal` 必须为空，CI 强制检查。
- **文件组织**：Go 文件按职责组织（如 `internal/store/reader_contracts.go` / `reader_fields.go`），
  不按类型堆砌。Go 没有官方行数上限，本仓库也不设硬门禁；单文件接近
  1000 行时，新增逻辑优先落到新文件，单函数超过约 80 行时考虑拆分，
  不为凑行数而拆。
- **静态检查**：`go vet ./...` 无输出。桌面壳 `./cmd/tengshou` 依赖 cgo，
  在 macOS 桌面构建 job 内 vet；Linux 测试 job 覆盖
  `./internal/... ./cmd/tengshou-cli`。
- **测试**：`go test -count=1 ./...` 全绿；新增逻辑必须有测试，测试不依赖
  外网、不绑定固定端口。
- **错误处理**：向上包装用 `fmt.Errorf("动作: %w", err)`；对用户可见的错误
  写清中文原因；不得静默吞错（无法返回时至少用 `log/slog` 记录）。
- **依赖**：新增第三方依赖需在 `go.mod` 锁定并说明理由；CLI 路径保持纯 Go
  （无 cgo），SQLite 固定使用 `modernc.org/sqlite`。
- **常用命令**：`make vet`、`make test`、`make build`。

## 前端工程约定

前端已迁移到 **Vue 3**，工程标准归属仓库，不写进 city 公式。

### 目标态

- **TypeScript strict**：前端源码全部使用 TS，`strict` 模式通过类型检查。
- **Vue 3 SFC**：界面由单文件组件（`.vue`）组成，优先使用组合式 API。
- **Vite 构建**：`vite build` 产出静态资源，Go 侧以 `//go:embed dist` 嵌入。
- **API 契约类型化**：前端调用后端 `/api/...` 时必须有对应的 TS 类型；契约
  变更先改类型再改调用点，禁止用 `any` 兜底（确需例外时写明原因）。
- **单文件 ≤ 600 行**：任何 `.js` / `.ts` / `.vue` 源文件不得超过 600 行，
  由 `scripts/check-frontend-structure.sh` 在 CI 中强制。

### 界面结构

- 界面全部由 `frontend/src/` 的 Vue 应用渲染，入口为 `frontend/src/main.ts`
  + `frontend/src/App.vue`；`internal/httpapi/web/app/index.html` 只保留
  `#app` 宿主、本地 pdf.js vendor 与 Vite 产物引用。
- 存量 `internal/httpapi/web/app/app.js` 已退役，**不得重新引入**；由
  `scripts/check-frontend-structure.sh` 强制。
- 新增功能一律落 Vue 组件；跨域打开弹窗/表单走 `frontend/src/bridge/app.ts`
  的 window 事件，toast 与 modal 基础设施在 `frontend/src/ui/`。
- 顶栏、tab 激活态、统计刷新与实时事件调度（SSE/轮询）在
  `frontend/src/shell/`；各域不自行创建 EventSource。

### CI 与本地验证

`.github/workflows/ci.yml` 的 `frontend` job 运行结构预算棘轮、类型检查与
构建。本地提交前至少跑：

```bash
bash scripts/check-frontend-structure.sh
cd frontend && npm run typecheck && npm run build
```

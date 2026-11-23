# 腾手（tengshou）仓库工程约定

本文件对本仓库内的所有代码改动生效，Claude 会话会自动加载。
本文件只写仓库自己的工程标准；评审与合入流程以 Gas City 官方公式
（`implement` / `review` / `publish`）为准，不在本文件重复。

## 通用约束

- **离线单二进制不变**：前端产物通过 `go:embed` 打进 Go 二进制，发布物仍是
  单个可执行文件；运行时不访问公网、不依赖 CDN 或任何外部服务。
- **无运行时前端依赖**：生产构建不引入需要联网获取的运行时依赖；第三方库
  一律随仓库锁定版本（vendor 或构建期打包）。

## Go 工程约定

- **格式**：`gofmt -l ./cmd ./internal` 必须为空，CI 强制检查。
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

### 迁移期规则（存量 app.js）

界面主体仍由 `internal/httpapi/web/app/app.js` 驱动；迁移完成前：

- **存量 `app.js` 只许减不许增**：由 `scripts/check-frontend-structure.sh`
  的 `APP_JS_BASELINE` 强制，上浮即失败；`app.js` 显著缩小后同步下调基线，
  基线以脚本为唯一事实源。
- **新功能一律落 Vue 组件**：不再向 `app.js` 添加新功能；新增功能与较大改造
  应在 Vue 组件中实现，并同步减少 `app.js`（至少不增加）。
- 修复类改动尽量顺手把相关逻辑抽成组件迁出；确实无法当天迁移时，改动本身
  也不能让 `app.js` 超过基线。

### CI 与本地验证

`.github/workflows/ci.yml` 的 `frontend` job 运行结构预算棘轮、类型检查与
构建。本地提交前至少跑：

```bash
bash scripts/check-frontend-structure.sh
cd frontend && npm run typecheck && npm run build
```

# 腾手（tengshou）仓库工程约定

本文件对本仓库内的所有代码改动生效，Claude 会话会自动加载。

## 前端工程约定（2026-11-23 起）

前端已确定迁移到 **Vue 3**。工程标准归属仓库，不写进 city 公式。

### 不变约束

- **离线单二进制不变**：前端产物通过 `go:embed` 打进 Go 二进制，发布物仍是
  单个可执行文件；运行时不访问公网、不依赖 CDN 或任何外部服务。
- **无运行时前端依赖**：生产构建不引入需要联网获取的运行时依赖；第三方库
  一律随仓库锁定版本（vendor 或构建期打包）。

### 目标态

- **TypeScript strict**：前端源码全部使用 TS，`strict` 模式通过类型检查。
- **Vue 3 SFC**：界面由单文件组件（`.vue`）组成，优先使用组合式 API。
- **Vite 构建**：`vite build` 产出静态资源，Go 侧以 `//go:embed dist` 嵌入。
- **API 契约类型化**：前端调用后端 `/api/...` 时必须有对应的 TS 类型；契约
  变更先改类型再改调用点，禁止用 `any` 兜底（确需例外时写明原因）。
- **单文件 ≤ 600 行**：任何 `.js` / `.ts` / `.vue` 源文件不得超过 600 行，
  由 `scripts/check-frontend-structure.sh` 在 CI 中强制。

### 迁移期规则（存量 app.js）

阶段 0 已引入 Vite + Vue 3 构建管线（详见 `docs/frontend.md`），界面主体
仍由 `internal/httpapi/web/app/app.js` 驱动。迁移完成前：

- **存量 `app.js` 只许减不许增**：基线 1341 行（阶段 1 迁出新手引导、模板与标定编辑器、合同详情生成面板与批量生成对话框后），
  `scripts/check-frontend-structure.sh` 在 CI 中把关，上浮即失败。
- **新功能一律落 Vue 组件**：不再向 `app.js` 添加新功能；新增功能与较大改造
  应在 Vue 组件中实现，并同步减少 `app.js`（至少不增加）。
- 修复类改动尽量顺手把相关逻辑抽成组件迁出；确实无法当天迁移时，改动本身
  也不能让 `app.js` 超过基线。

### CI

`.github/workflows/ci.yml` 的 `frontend` job 运行
`scripts/check-frontend-structure.sh`。本地提交前请先跑一遍：

```bash
bash scripts/check-frontend-structure.sh
```

基线只在下调时更新：当 `app.js` 显著缩小后，把脚本中的 `APP_JS_BASELINE`
调整为当前行数，让棘轮保持单调下降。

## 质量门与工作流归属

- 合入质量门由 Gas City 的 review 角色执行：每个 bead 合入前须通过三条只读
  独立评审车道（`implementation-reviewer`、`design-test-risk-reviewer`、
  `design-implementation-reviewer`），再由 `review-synthesizer` 汇总裁决；
  reviewer 只读不改码，findings 与 verdict 写入对应 bead。
- 评审与 CI 并行执行，两者都通过方可合入；返工由 mayor 在 worktree 修复后
  重跑受影响车道，不重派实现 worker。
- 本文件只写工程标准；city 侧的公式、角色与质量门配置以 city 配置为准，不由
  本文件覆盖。早期「不要用 implement/drain 工作流」的约定作废。

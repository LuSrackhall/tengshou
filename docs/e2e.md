# 界面端到端测试（E2E）

本仓库用 Playwright 驱动**发布形态**的 `tengshou-cli serve`，把「人」这条使用
路径纳入发布门禁：测试跑的是 `go:embed` 打进二进制的同一份 Vue 界面与本地服务，
不连 Vite 开发服务器，也不访问公网。

E2E 只断言 UI 层事实（页面出现什么、切到哪一页、文件能否下载）；业务规则仍由
Go 单元/集成测试负责，二者不重复。

## 场景

### 场景 A：合同主流程

来源：`e2e/tests/contract-flow.spec.ts`

覆盖用户第一次把软件用起来的最小完整路径：

1. 在「模板」页导入一份 PDF 模板（首装向导的模板导入同一入口）；
2. 在「字段」页新建一个合同字段；
3. 在「相对方档案」页新建一份档案；
4. 在「合同台账」页新建合同并保存为草稿（编号由系统自动分配）；
5. 服务端预检通过（必填字段、相对方齐全）；
6. 在生成面板预览，确认版式后生成 PDF；
7. 从生成历史下载 PDF，断言文件非空且以 `%PDF-` 开头。

全程断言浏览器控制台没有 error、没有未捕获异常。

### 场景 B：MCP 跨层联动

来源：`e2e/tests/mcp-activity.spec.ts`

对应客户最初「看不到 agent 操作」的抱怨：页面打开时，测试以真实 stdio 管道运行
`tengshou-cli mcp --api-url <同一 serve>`，调用 `tengshou_create_field`。
断言：

- 界面自动切到「字段」页；
- 新字段行出现在台账中；
- 顶部出现「agent 操作」徽标（含变更摘要）；
- 假光标滑入并高亮目标行（agent 操作可见层）。

## 本地运行

前置（首次）：

```bash
# 1. 构建前端产物（go:embed 的输入）
cd frontend && npm ci && npm run build && cd ..

# 2. 构建发布形态 CLI
go run ./tools/tengshoubuild build cli

# 3. 安装 E2E 依赖与浏览器
cd e2e
npm ci
npx playwright install chromium   # macOS 需要 webkit：npx playwright install webkit
```

运行：

```bash
cd e2e
npm test                          # 当前系统默认浏览器（macOS=WebKit，其余=Chromium）
E2E_BROWSER=chromium npm test     # 本地临时改用 Chromium
npm run report                    # 打开 HTML 报告
```

也可以从仓库根用 `make e2e`（会先构建前端再构建 CLI）。

可选环境变量：

| 变量 | 作用 |
| --- | --- |
| `PLAYWRIGHT_BASE_URL` | 复用外部已启动的 `serve`，测试自身不再拉起进程 |
| `TENGHSHOU_CLI` | 指定 CLI 路径（默认在 `dist/` 按三端命名规则查找） |
| `TENGHSHOU_DATA_DIR` | 外部模式下读取 `config.json` 的接口令牌 |
| `TENGHSHOU_API_TOKEN` | 直接指定接口令牌 |
| `TENGHSHOU_FONT` | 中文字体（Linux CI 必设，见下方） |
| `E2E_BROWSER` | `chromium` / `webkit`，覆盖默认浏览器 |
| `E2E_KEEP_DATA` | 设为 `1` 时保留临时数据目录，便于排查 |

未设置 `PLAYWRIGHT_BASE_URL` 时，每个测试文件会自行：取空闲端口 → 在系统临时
目录建数据目录 → 启动 `tengshou-cli serve --loopback` → 测试结束回收进程与目录。

## 三端与浏览器

| 平台 | 浏览器 | 理由 |
| --- | --- | --- |
| Linux | Chromium | 标准无头环境 |
| Windows | Chromium | 近似桌面壳的 WebView2 |
| macOS | WebKit | 近似桌面壳的 WKWebView |

三端跑同一套用例。CI 里 `e2e` job 先取 `frontend` job 的构建产物，再用
`go run ./tools/tengshoubuild build cli` 构建本端 CLI，然后执行套件；PR 与
`main` 均会执行，失败阻塞合入。

Linux 需要中文字体，CI 与 Go 测试一致安装 `fonts-arphic-uming` 并设置
`TENGHSHOU_FONT`；macOS / Windows runner 使用系统自带中文字体。

## 可视报告与工件

Playwright 配置为：

- `video: "on"`：每条用例都有录屏；
- `trace: "retain-on-failure"`：失败时保留 trace；
- 关键业务步骤通过 `testInfo.attach` 附加全页截图（中文步骤名）；
- HTML 报告离线可打开，按中文 `test.step` 展示截图、视频与失败堆栈。

CI 在 `e2e` job 中**始终**上传 `e2e/playwright-report` 与 `e2e/test-results`
（每 OS 一份，保留 90 天），命名 `e2e-report-linux-chromium` /
`e2e-report-windows-chromium` / `e2e-report-macos-webkit`。下载后在本地解压，
打开 `playwright-report/index.html` 即可离线查看。

## 定位界面回归

E2E 失败时会指出具体业务步骤、期望文案与选择器。例如把「新建字段」按钮文案
临时改坏：

1. 修改 `frontend/src/components/fields/FieldsView.vue` 中的按钮文案；
2. 重新 `npm run build` + `go run ./tools/tengshoubuild build cli`；
3. `cd e2e && npm test`。

失败会停在「界面新建字段」步骤，报告里附带当时的页面截图、录屏与 trace；
`npx playwright show-trace test-results/<用例>/trace.zip` 可逐步回放 DOM 与网络。
验证完请还原改动。

## 约束

- E2E 只依赖本机回环服务与仓库内的 PDF 夹具（`e2e/fixtures/template.pdf`），
  不访问外网、不依赖 CDN；
- 临时数据目录、空闲端口，避免与本地正在运行的腾手实例冲突；
- 不修改现有 Go 测试与 MCP 冒烟断言；
- `e2e/` 内的 TS 文件同样受 `scripts/check-frontend-structure.sh` 的 600 行
  上限约束。

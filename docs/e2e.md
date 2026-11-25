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

### 场景 C：真实业务场景验收报告（E2E-2a）

来源：`e2e/tests/business-acceptance.spec.ts`

这是给产品负责人看的一条**真实业务主线**，不是工程师视角的组件测试：
幸福村股份经济合作社向农户王建国采购红富士苹果，从一份 Word 合同模板开始，
在界面里完成字段标定、双方档案、合同录入、手机扫码手写签名、公章落位，最后
生成并下载可以自己打开的合同 PDF。测试按业务步骤记录「预期结果 / 实际结果 /
判定」，并为每个交互步骤采集多张**过程分镜**，结束（含失败）生成一份自包含
中文报告：

```
e2e/test-results/business-acceptance/业务验收报告.html
e2e/test-results/business-acceptance/验收过程录像.webm
e2e/test-results/business-acceptance/农产品购销合同-已签字盖章.pdf
```

报告 v2 的读法（E2E-2a）：

- 每个业务步骤按真实顺序放 2–7 张分镜，图注说明「做了什么 / 界面发生了什么 /
  红框看哪里」；循环操作（建字段、标定位置）取首次完整过程 + 代表样本 + 最终
  汇总，图注写明实际数量；
- 分镜优先裁剪到弹窗 / 表单 / 表格行 / 标定页局部，目标元素画红色高亮框与
  编号徽标；图片可点击放大（灯箱）；
- 最后的「核对最终合同 PDF」用页面自带 pdf.js 把合同逐页渲染进报告：第 1/2 页
  关键值、第 3 页签名与甲方公章画红框（坐标取模板 v2 的 placements），并附
  签名+公章区域放大图；
- 全过程 WebM 由 `e2e/tests/support/report-video-reporter.ts` 在测试结束后复制
  到报告目录并补全播放器；每步标题旁有「▶ 看本步过程（约 00:xx）」，按步骤
  开始时间 seek（误差约 1–3 秒）。视频缺失时报告显示说明而不是坏掉的播放器；
- 报告顶部有「怎么看这份报告」与「报告自检」：交互步骤分镜不足、图注为空、
  PDF 页面/放大图缺失都会列出来。报告自检只影响报告呈现，不改变业务断言与
  判定——单个分镜采集失败只记录「截图采集失败：原因」。

报告为单文件（视频为同目录相对引用）、分镜 base64 内嵌、离线可打开；最终 PDF
与报告同目录，报告内提供相对链接。合同里的单位、姓名、证件号、电话、地址、
金额与公章编号**全部虚构**，报告中显著声明，仅用于演示与测试。

主流程：

1. 导入 `e2e/fixtures/contracts/农产品购销合同.docx`，产品调用转换器（Windows
   优先 Microsoft Word，其余平台用 LibreOffice）转成 PDF 并自动进入标定编辑器；
2. 在「字段」页建立合同专有字段（标的名称、规格、数量、单价、大写金额、
   交货时间/地点、验收标准、付款方式、违约责任、争议解决）与相对方联系人字段；
3. 建立甲方（单位）与乙方（个人）档案；
4. 在 PDF 页面上逐框添加并拖动字段、乙方签名、甲方公章位置，保存为新版本；
5. 新建合同：选择甲方（本方）、相对方，填写标的与金额，保存草稿；
6. 生成一次性二维码 → 第二个浏览器页面打开手机签名页 → 鼠标手写三笔并提交 →
   桌面端轮询到「签名已保存」；
7. 导入白底红章 PNG（验证产品自动去底）→ 关联到合同；
8. 用目标合同预览 → 生成 PDF → 下载；
9. 核对最终 PDF：页数与模板一致、文本含编号/金额/双方名称/标的等关键值、
   签名与公章两张图片确实套印、全程无 console error。

夹具可复现：

- `e2e/fixtures/contracts/generate-docx.py`：Python 标准库 zipfile + OOXML，
  重新生成 `农产品购销合同.docx`（A4、10.5pt、第 2 页签字盖章栏）；
- `e2e/fixtures/contracts/generate-seal.mjs`：Playwright 把内联 SVG 截图为
  白底红章 `采购方公章.png`（400×400 RGB；本地没有 Playwright Chromium 时
  脚本会退回系统 Chrome）。

本地前置与平台：

- 本场景在 Linux、Windows、macOS 三端 CI 都真实运行（见下方「场景 × 平台矩阵」）；
  CI 三端都安装转换器并设 `E2E_REQUIRE_DOCX=1`，转换器缺失必须失败，不允许静默跳过。
- 本地运行前需自行安装转换器：
  - Windows：Microsoft Word（产品优先用 COM 导出）或 LibreOffice；
  - macOS / Linux：LibreOffice（产品的 Word COM 转换仅支持 Windows，macOS 上的
    Word 不参与自动转换）。
- 本机没有可用转换器且未设 `E2E_REQUIRE_DOCX=1` 时，用例以中文原因 `test.skip`，
  提示安装 LibreOffice 或 Windows 上的 Microsoft Word。
- 报告「运行环境」行写明平台与浏览器项目；转换器步骤展示 `/app/api/v1/converters`
  的真实状态，导入后以模板详情的 `converter` 字段说明**实际使用**的转换器
  （Windows 未安装 Word 时会回退 LibreOffice，报告不会声称 Word 可用）。
- 扫码签名要求 `allow_lan=true`，本场景启动的 serve **不带** `--loopback`
  （`business-fixture.ts`），测试仍以 127.0.0.1 访问。

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
| `E2E_REQUIRE_DOCX` | 设为 `1` 时，场景 C 缺少 Word→PDF 转换器必须失败（CI 三端均设） |

未设置 `PLAYWRIGHT_BASE_URL` 时，每个测试文件会自行：取空闲端口 → 在系统临时
目录建数据目录 → 启动 `tengshou-cli serve --loopback` → 测试结束回收进程与目录。

## 场景 × 平台矩阵

三端跑同一套场景，场景 C 不再有平台门槛：

| 场景 | Linux · Chromium | Windows · Chromium | macOS · WebKit |
| --- | --- | --- | --- |
| A 合同主流程 | 运行 | 运行 | 运行 |
| B MCP 跨层联动 | 运行 | 运行 | 运行 |
| C 真实业务场景验收 | 运行（LibreOffice） | 运行（装有 Word 时优先 Word，否则回退 LibreOffice） | 运行（LibreOffice） |

CI 的 `e2e` job 在每一端都安装转换器，并统一设置 `E2E_REQUIRE_DOCX=1`：
转换器缺失时任务失败，不会跳过场景 C。

| 平台 | 安装方式 | 产品检测路径 |
| --- | --- | --- |
| Linux | `apt-get install libreoffice-writer`（另装 `fonts-noto-cjk` 供渲染） | `/usr/bin/soffice` 等 |
| Windows | `choco install libreoffice-fresh -y --no-progress` | `C:\Program Files\LibreOffice\program\soffice.exe` |
| macOS | `brew install --cask libreoffice` | `/Applications/LibreOffice.app/Contents/MacOS/soffice` |

Windows 上产品的转换链先尝试 Microsoft Word（COM）。该检测只确认 PowerShell
可启动，不代表 Word 已安装；转换失败会回退 LibreOffice。报告以导入模板记录的
`converter` 字段说明实际使用的转换器，不会把「平台可尝试 Word」写成「Word 可用」。

## 三端与浏览器

| 平台 | 浏览器 | 理由 |
| --- | --- | --- |
| Linux | Chromium | 标准无头环境 |
| Windows | Chromium | 近似桌面壳的 WebView2 |
| macOS | WebKit | 近似桌面壳的 WKWebView |

桌面壳（Wails）用系统 WebView 渲染同一份 `go:embed` 界面，但 Wails 原生窗口
无法被 Playwright 附加——这是已知技术限制。E2E 因此用**同一份 UI 资源**在
Chromium / WebKit 中驱动发布形态服务：Windows 的 Chromium 近似 WebView2，
macOS 的 WebKit 近似 WKWebView。三端验证覆盖界面层与发布形态服务，不覆盖
原生窗口外壳本身。

三端都运行场景 A、B、C。CI 里 `e2e` job 先取 `frontend` job 的构建产物，
再用 `go run ./tools/tengshoubuild build cli` 构建本端 CLI，然后执行套件；
PR 与 `main` 均会执行，失败阻塞合入。

Linux 需要中文字体，CI 与 Go 测试一致安装 `fonts-arphic-uming` 并设置
`TENGHSHOU_FONT`；场景 C 另装 `fonts-noto-cjk` 与 `libreoffice-writer` 供
Word 转换。macOS / Windows runner 使用系统自带中文字体。

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

三端还会各自上传场景 C 的**业务验收工件**：
`business-acceptance-linux-chromium` / `business-acceptance-windows-chromium` /
`business-acceptance-macos-webkit`，内容是
`e2e/test-results/business-acceptance/`（中文报告 + 过程录像 WebM + 最终合同
PDF）。产品负责人按平台下载对应工件，解压后双击 `业务验收报告.html`，无需
安装任何软件；报告内的逐页 PDF 图已内嵌，过程视频为同目录相对引用。报告
「运行环境」行标明平台与浏览器，转换器步骤与实际转换器信息随平台如实呈现。

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

- E2E 只依赖本机回环服务与仓库内的夹具（`e2e/fixtures/template.pdf`、
  `e2e/fixtures/contracts/`），不访问外网、不依赖 CDN；
- 临时数据目录、空闲端口，避免与本地正在运行的腾手实例冲突；
- 不修改现有 Go 测试与 MCP 冒烟断言；
- `e2e/` 内的 TS 文件同样受 `scripts/check-frontend-structure.sh` 的 600 行
  上限约束。

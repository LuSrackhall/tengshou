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
幸福村股份经济合作社把「幸福村 3 组 12 号 101」出租给王建国，从一份 Word
租赁合同模板开始，在界面里完成字段标定、出租方/承租方/房产三方档案、合同
录入（三方选择 + 租期 + 租金押金）、手机扫码手写签名、公章落位，最后生成并
下载可以自己打开的合同 PDF。测试按业务步骤记录「预期结果 / 实际结果 /
判定」，并为每个交互步骤采集多张**过程分镜**，结束（含失败）生成一份自包含
中文报告：

```
e2e/test-results/business-acceptance/业务验收报告.html
e2e/test-results/business-acceptance/验收过程录像.webm
e2e/test-results/business-acceptance/房屋租赁合同-已签字盖章.pdf
```

报告 v2 的读法（E2E-2a）：

- 每个业务步骤按真实顺序放 2–7 张分镜，图注说明「做了什么 / 界面发生了什么 /
  红框看哪里」；循环操作（建字段、标定位置）取首次完整过程 + 代表样本 + 最终
  汇总，图注写明实际数量；
- 分镜优先裁剪到弹窗 / 表单 / 表格行 / 标定页局部，目标元素画红色高亮框与
  编号徽标；图片可点击放大（灯箱）；
- 最后的「核对最终合同 PDF」用页面自带 pdf.js 把合同逐页渲染进报告：第 1/2 页
  关键值（双方名称与证件号、房产地址/房号/面积、租期、月租金/押金及大写）、
  第 3 页签名与甲方公章画红框（坐标取模板 v2 的 placements），并附签名+公章
  区域放大图；定稿模板正文不含合同编号位置，编号在台账与生成记录核对；
- 全过程 WebM 由 `e2e/tests/support/report-video-reporter.ts` 在测试结束后复制
  到报告目录并补全播放器；每步标题旁有「▶ 看本步过程（约 00:xx）」，按步骤
  开始时间 seek（误差约 1–3 秒）。视频缺失时报告显示说明而不是坏掉的播放器；
- 「打印自助录入海报，租户手机扫码建档」与「扫码手写签名」两步在**分镜之前**
  内嵌**手机侧实录**：Playwright 对 context 内每个 page 各录一份，但只有 fixture
  页面的录像会作为附件交给报告器，手机页录像此前从未被取用（te-cfgvj）。现在
  这两步在手机页 `close()` 后用 `video().saveAs()` 取回独立 WebM（VP9
  1440×960），读入 Buffer 后以 `data:video/webm;base64,` 内联进报告与教程；
  播放器标题写明「手机侧实录（扫码后手机上的完整录入过程）」，放在步骤标题下方、
  截图之前，临时文件随即删除——报告目录仍只有 4 个交付文件，打包清单不变。
  录像不可用时步骤照常判定（pass），报告在该处标注「手机侧录像缺失：原因」；
- **主录像画中画（te-fp48w）**：测试结束、Playwright 录像落盘后，报告器
  （`report-video-reporter.ts`）用本机系统 ffmpeg 把两段手机侧录像合成进
  `验收过程录像.webm`：等比缩到画面宽 1/3（480×320）、右下角、绿色细边框，
  按手机页创建/关闭的墙钟换算到报告时钟（与步骤 `startedAtSeconds` 同一时钟），
  只在对应时间窗内叠加；主录像时间轴不变，仍是 VP9 1440×960、无音频。用户
  直接看录像文件或点「▶ 看本步过程」就能看到扫码之后手机上的录入过程。报告
  模式下手机页操作按人速填写/停顿（逐字输入、选类型/上传后停顿，手机侧片段
  ≥8 秒），教程模式不重复减速（已有 `slowMo: 500`）；
- **ffmpeg 依赖与降级**：合成只探测 PATH 中的系统 ffmpeg（`E2E_FFMPEG` 可指定
  路径，`E2E_FFMPEG=off` 显式禁用）；Playwright 自带的 ffmpeg 没有 overlay
  滤镜，不能使用。ffmpeg 不可用或合成失败时跳过画中画、在报告视频区写明中文
  原因，步骤内嵌播放器与业务判定不受影响，E2E 不会因此变红。CI 三端（以及
  教程任务）显式安装并校验 ffmpeg 的 overlay 与 libvpx-vp9，保证发布工件里
  一定有画中画；合成计划与手机侧暂存录像放在报告目录的隐藏 `.pip/` 下，结束
  时整体删除，交付件仍是 4 个文件；
- 报告顶部有「怎么看这份报告」与「报告自检」：交互步骤分镜不足、图注为空、
  PDF 页面/放大图缺失都会列出来。报告自检只影响报告呈现，不改变业务断言与
  判定——单个分镜采集失败只记录「截图采集失败：原因」。

报告为单文件（视频为同目录相对引用）、分镜 base64 内嵌、离线可打开；最终 PDF
与报告同目录，报告内提供相对链接。合同里的单位、姓名、证件号、电话、地址、
金额与公章编号**全部虚构**，报告中显著声明，仅用于演示与测试。

### 场景 H：长操作真实进度（job 化）

来源：`e2e/tests/job-progress.spec.ts`

覆盖「等待像卡住」的反馈：长操作统一为后台任务 + 真实阶段进度。

1. 导入 Word 模板时，弹窗内出现进度面板，阶段从「保存文件」切到
   「正在转换为 PDF」（ACC-1 要求至少该阶段可见），并显示「已用 N 秒」；
   等待期间按钮文案为「转到后台」，完成后弹窗关闭、自动打开标定编辑器；
2. 批量生成 2 份合同：预检与生成都显示真实 i/N（`data-job-total="2"`，
   `data-job-current` 随完成份数递增，文案含「第 x/2 份」），结束后结果表
   逐份成功；同一用例用接口旁证 job 快照结束于 `current=2,total=2`。
3. Excel 导入的预览与提交走 job 端点：空白录入模板能正常预览并提交，
   结果区显示计数徽标（预览/提交失败时错误文案与同步接口一致）。

同步端点不在此场景重测：它们由 Go 单元/集成测试与场景 A/C 继续覆盖，
job 化不改变 MCP 与既有 API 的行为。

### 使用教程（教程模式）

场景 C 与验收报告同源再生成一份**面向用户的教程**
`e2e/test-results/business-acceptance/腾手使用教程.html`：

- 每个业务步骤按已确认样张渲染「你要做什么 / 你会看到什么 / 注意什么」三段式，
  保留红框标注、点击放大（灯箱）、过程分镜与「看本步视频」章节跳转，结尾可
  打开示例最终合同 PDF；
- 教程不渲染测试元信息：控制台检查步骤没有教程文案（不进教程），报告自检与
  转换器 API 状态卡也不出现在教程里；
- 教程文案与业务步骤标题绑定在 `e2e/tests/support/business-tutorial-copy.ts`；
  界面流程变化时文案随步骤一起改，教程随版本自动更新（测试未覆盖的功能不会
  出现在教程里）；
- 「自助建档」「扫码签名」两步在教程里同样内嵌手机侧实录（见上文报告 v2 读法）；
  `e2e/tests/tutorial-html.spec.ts` 的最小模型断言覆盖内嵌录像渲染与缺失降级；
- 设 `E2E_TUTORIAL=1` 进入**教程录制模式**：Playwright 录像尺寸显式设为
  1440×960（缺省会缩到 800×800，教程会糊）、浏览器操作用 `slowMo: 500`
  （每步 0.4–0.6s）、每次点击显示圆点 + 波纹、每步开头显示 1 秒章节标题卡并
  在步骤结束停顿约 1.2 秒。教程 HTML 始终生成；慢速与点击指示只在教程模式下
  存在，普通测试运行保持原速（录像仍是高清）。

发版标签会额外运行一个 macOS · WebKit 的教程任务（单平台即可），把
`腾手使用教程.html`、`验收过程录像.webm`、`房屋租赁合同-已签字盖章.pdf` 与
`SHA256SUMS` 打成 `tengshou-tutorial-<tag>.zip` 作为发布资产。

预发布标签还会额外运行一个「汇总验收证据」任务：先在 `main` 上找到同一
commit 的 push run，等它 completed 且 conclusion=success（把「打 tag 前
main 已全绿」的发版前提变成机制，不重跑 E2E），再用 `gh run download`
取回三端 `business-acceptance-*` 工件，按平台各打一个
`tengshou-acceptance-<tag>-<platform>.zip`（只保留业务验收报告、过程录像、
示例 PDF 与 SHA256SUMS；工件里重复的教程 HTML 不进入）。

发布资产按版本号分两档，清单唯一事实源 = `tengshoubuild release-plan`：

| 版本 | 资产数 | 内容 |
| --- | --- | --- |
| 正式 `vX.Y.Z` | 5 | 4 个客户端资产 + 1 个教程包 |
| 预发布 `vX.Y.Z-(alpha\|beta\|rc).N` | 8 | 上述 5 个 + 三端各一个验收包 |

主流程：

1. 导入 `e2e/fixtures/contracts/房屋租赁合同.docx`（v0.3 租赁定稿：3 页、
   24 个标定位置、12 个动态字段），产品调用转换器（Windows 优先 Microsoft
   Word，其余平台用 LibreOffice）转成 PDF 并自动进入标定编辑器；
2. 在「字段」页建立 12 个租赁合同字段（租赁用途、月租金/大写、付款周期、
   押金/大写、交付日期、提前退租通知天数、逾期天数、违约金、费用承担方、
   其他约定），并按「租期 / 租金与押金 / 费用与违约 / 其他约定」分组；
3. 建立出租方（单位）与承租方（个人）档案，再建立出租房产档案（地址、房号、
   面积）；
4. 在 PDF 页面上逐框添加并拖动 22 个字段、乙方签名、甲方公章位置（共 24 个），
   保存为新版本；
5. 新建合同：选择出租方、承租方与房产（三方），填写租期与月租金/押金/违约金，
   保存草稿；月租金与押金大写由 A 珠能力自动生成；
6. 生成一次性二维码 → 第二个浏览器页面打开手机签名页 → 鼠标手写三笔并提交 →
   桌面端轮询到「签名已保存」；
7. 导入白底红章 PNG（验证产品自动去底）→ 关联到合同；
8. 用目标合同预览 → 生成 PDF → 下载；
9. 核对最终 PDF：页数与模板一致、文本含双方名称与证件号、房产地址/房号/面积、
   租期、月租金/押金及大写、签名与公章两张图片确实套印、全程无 console error。

夹具可复现：

- `e2e/fixtures/contracts/房屋租赁合同.docx`：二进制夹具，来源是 v0.3 租赁定稿
  `plans/v03-lease/房屋租赁合同-定稿.docx`（`plans/` 不入库）。版面与下划线
  宽度以定稿为准，脚本不重新拼近似文档；
- `e2e/fixtures/contracts/generate-docx.py`：零第三方依赖的**校验/刷新**脚本，
  默认校验夹具包含全部 24 个标定锚点且无【】残留；`--from <定稿.docx>` 可从
  定稿刷新夹具；
- `e2e/fixtures/contracts/generate-seal.mjs`：Playwright 把内联 SVG 截图为
  白底红章 `出租方公章.png`（400×400 RGB，章面文字「幸福村股份经济合作社
  合同专用章」；本地没有 Playwright Chromium 时脚本会退回系统 Chrome）。

本地前置与平台：

- 本场景在 Linux、Windows、macOS 三端 CI 都真实运行（见下方「场景 × 平台矩阵」）；
  CI 三端都安装转换器并设 `E2E_REQUIRE_DOCX=1`，转换器缺失必须失败，不允许静默跳过。
- 本地运行前需自行安装转换器：
  - Windows：Microsoft Word（产品优先用 COM 导出）或 LibreOffice；
  - macOS / Linux：LibreOffice（产品的 Word COM 转换仅支持 Windows，macOS 上的
    Word 不参与自动转换）。
- 本机没有可用转换器且未设 `E2E_REQUIRE_DOCX=1` 时，用例以中文原因 `test.skip`，
  提示安装 LibreOffice 或 Windows 上的 Microsoft Word。
- 主录像画中画合成需要 PATH 中的系统 ffmpeg（带 `overlay` 滤镜与 `libvpx-vp9`
  编码器）；本机没有时 E2E 照常运行，报告视频区会写明「未合成手机侧画中画」
  并保留步骤内嵌播放器。macOS 可用 `brew install ffmpeg`，Windows 可用
  `choco install ffmpeg`；CI 三端与教程任务都会安装并校验。
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
| `E2E_TUTORIAL` | 设为 `1` 时进入教程录制模式（slowMo 0.5s、点击圆点 + 波纹、章节标题卡）；发版时由教程任务设置 |
| `E2E_FFMPEG` | 画中画合成使用的 ffmpeg 路径（缺省探测 PATH）；设为 `off` 显式禁用合成（用于验证降级路径） |

未设置 `PLAYWRIGHT_BASE_URL` 时，每个测试文件会自行：取空闲端口 → 在系统临时
目录建数据目录 → 启动 `tengshou-cli serve --loopback` → 测试结束回收进程与目录。

## 场景 × 平台矩阵

三端跑同一套场景，场景 C 不再有平台门槛：

| 场景 | Linux · Chromium | Windows · Chromium | macOS · WebKit |
| --- | --- | --- | --- |
| A 合同主流程 | 运行 | 运行 | 运行 |
| B MCP 跨层联动 | 运行 | 运行 | 运行 |
| C 真实业务场景验收 | 运行（LibreOffice） | 运行（装有 Word 时优先 Word，否则回退 LibreOffice） | 运行（LibreOffice） |
| H 长操作真实进度 | 运行（LibreOffice） | 运行（同 C 的转换链） | 运行（LibreOffice） |

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

- `video: { mode: "on", size: 1440×960 }`：每条用例都有原生高清录屏
  （显式指定尺寸，避免缺省缩到 800×800）；
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
`e2e/test-results/business-acceptance/`（中文报告 + 使用教程 + 过程录像 WebM +
最终合同 PDF）。产品负责人按平台下载对应工件，解压后双击
`业务验收报告.html` 或 `腾手使用教程.html`，无需安装任何软件；报告内的逐页
PDF 图已内嵌，过程视频为同目录相对引用。报告「运行环境」行标明平台与浏览器，
转换器步骤与实际转换器信息随平台如实呈现。预发布时，这些工件还会被发布任务
汇总为三端各一个验收包（见上文「汇总验收证据」），随 Release 一起发布。

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

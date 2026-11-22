# 腾手（tengshou）

让村里的签合同变成录台账：合同数据录一次，程序产出合同 PDF 与自有台账；
外部表格格式通过只读数据接口交给用户侧 AI agent 处理，核心不做平台适配。

当前仓库处于 **B3 模板管理**：导入用户自己的 Word 合同模板，转换为 PDF
后以所见即所得页面展示，在页面上拖框标定字段、签名与公章位置，按版本保存，
再用真实合同数据预览并套印生成最终 PDF。B2 动态字段、B5 局域网签名、B13 公章
图片、B6 MCP 数据连接器均在同一条数据链路上。

## 当前能力（B2 + B3 + B5 + B13 + B6）

- **模板管理（B3）**：导入 `.docx`（Windows 优先用本机 Word 导出，其他平台用
  LibreOffice headless；也可直接导入已导出的 PDF）；界面用 PDF.js 显示转换后的
  PDF 页面，拖框绑定合同/相对方字段、甲乙方签名与公章位置；每次保存生成新版本，
  旧合同仍引用旧版本重出；用样例或真实合同预览后生成套印 PDF。细节见
  [docs/b3-templates.md](docs/b3-templates.md)。
- **合同台账**：本地 Web 界面（`/app/`）内置表格录入、年度切换与跨年查询、
  搜索、状态筛选；合同编号稳定唯一（`HT-YYYY-NNNN` 或自定义）且创建后不可修改。
- **动态字段**：运行时新增/修改/归档字段（名称、类型、必填、默认值、是否出现在
  台账/模板），无需重启或发版；旧数据保留；新增字段立即用于表单、Excel、模板占位符。
- **相对方档案**：名称、类型、证件号、电话、地址、备注 + 自定义字段；合同录入时
  输入新名称自动建档；被合同引用时禁止删除。
- **局域网扫码手写签名（B5）**：合同行「扫码签名」生成一次性短时二维码；手机在
  同一局域网浏览器手写、清除重写、提交，无需装 App、不经过公网；签名透明底黑墨
  落盘，关联合同与内容版本（`content_sha256`），模板用
  `{{with signaturePath .Signatures .PartyB}}` 定位到 PDF；令牌复用 / 过期被拒绝，
  非局域网来源不可达。细节见 [docs/b5-signature.md](docs/b5-signature.md)。
- **公章图片（B13）**：导入 PNG / JPG，图片落盘为独立资产（路径 + SHA-256），
  白底自动转透明底；可关联到单位与多份合同（单份 / 批量）；模板用
  `{{with sealPath .Seals .PartyA}}` 或 `contractSealPath` 落位，换模板后按新
  位置重新落位；删除 / 替换有确认并写入变更记录。细节见
  [docs/b13-seals.md](docs/b13-seals.md)。
- **Excel 入口**：按当前字段定义生成录入模板（表头动态生成）；导入支持列名映射、
  逐行预览、新增/更新合并、错误行报告；台账导出使用同一套列定义，可导出后再导回。
- **变更记录**：谁在何时改了什么（字段级 diff），操作者取系统用户或 `config.json`
  的 `operator`；签名采集也写入变更记录。
- **Wails v3 桌面壳**：`cmd/tengshou`，窗口加载 `/app/` 台账界面。
- **内嵌 Go 本地服务**：默认监听 `0.0.0.0:8765`；局域网测试页公开，只读数据接口
  需令牌；应用写入接口只允许本机（回环）并校验请求头 / Origin；`/sign/*` 签名页
  只接受局域网来源。
- **SQLite 持久化**：纯 Go 驱动，内置迁移（v6）；B1 旧库自动补合同编号与年度。
- **HTML → PDF**：模板 + 合同数据生成带页码的 A4 PDF（中文、分页、图片、签名）。
- **只读数据接口**：`/api/v1/*`，供 B6 MCP 连接器与用户侧 agent 取数；
  新增 `/api/v1/fields` 暴露字段定义，签名资产可按合同查询。
- **MCP 数据连接器（B6）**：`tengshou-cli mcp` 以 stdio 启动，经本机回环
  HTTP 读取合同 / 相对方 / 动态字段 / 签名状态 / 模板版本；写工具仅限新建
  字段、相对方档案与合同草稿，复用同一 `internal/service` 校验，并以
  `tengshou-mcp` 记入变更记录；`config.json` 的 `mcp.write_enabled` 可关闭
  写入。注册方式、工具 schema 与安全边界见 [docs/mcp.md](docs/mcp.md)。
- **备份约定**：`VACUUM INTO` 备份到 `<data>/backups/`（含字段定义与数据）。
- **构建与发布**：`scripts/build.sh`、`scripts/build-windows.ps1`；GitHub Actions。

## 快速开始

需要 Go 1.26+。生成示例数据与 PDF：

```bash
go run ./cmd/tengshou-cli demo --data-dir ./data
```

启动本地服务与台账界面：

```bash
go run ./cmd/tengshou-cli serve --data-dir ./data
# 电脑浏览器打开 http://127.0.0.1:8765/app/
# 终端会列出本机 / 局域网地址与只读数据接口令牌
```

启动 MCP 连接器（桌面应用或 `serve` 运行后，在 Claude Code / Claude Desktop
中注册；详见 [docs/mcp.md](docs/mcp.md)）：

```bash
go run ./cmd/tengshou-cli mcp --data-dir ./data
```

启动桌面应用：

```bash
go run ./cmd/tengshou
# 或先构建：./scripts/build.sh && ./dist/tengshou
```

Excel 模板下载、导入与台账导出都在 `/app/` 的「合同台账」页；也可直接访问
`/app/api/v1/excel/template` 与 `/app/api/v1/excel/ledger`（仅本机）。

重新生成合同 PDF：

```bash
go run ./cmd/tengshou-cli render --data-dir ./data --contract demo-contract-1
```

备份数据库：

```bash
go run ./cmd/tengshou-cli backup --data-dir ./data
```

## 数据目录

默认位置：

| 平台 | 路径 |
| --- | --- |
| Windows | `%LOCALAPPDATA%\Tengshou` |
| macOS | `~/Library/Application Support/Tengshou` |
| Linux | `$XDG_DATA_HOME/tengshou` 或 `~/.local/share/tengshou` |

可用 `--data-dir` 或 `TENGHSHOU_DATA_DIR` 覆盖。

```
<data>/
  config.json     # 服务端口、API 令牌、字体、页边距、签名有效期
  tengshou.db     # SQLite 数据库（WAL）
  assets/         # 签名（signatures/）、公章（seals/）等模板资源
  templates/      # 用户 HTML 模板；PDF 模板在 templates/pdf/<模板id>/
  outputs/        # 生成的 PDF
  backups/        # 数据库备份
  logs/           # JSON 日志（启动时按大小轮转）
  fonts/          # 可选：用户自带中文字体（优先于系统字体）
```

签名二维码有效期由 `config.json` 的 `signature.ttl_minutes` 控制（默认 10 分钟，
范围 1–120）；签名会话是内存态的，重启服务后旧二维码自动失效。

## 中文字体

PDF 流水线需要一份覆盖中文的字体。解析顺序：

1. `TENGHSHOU_FONT` 环境变量 / `config.json` 的 `pdf.font_path`；
2. `<data>/fonts/` 下的 `.ttf/.ttc/.otf`；
3. 系统字体（macOS：Arial Unicode / 冬青黑体 / 华文黑体；Windows：微软雅黑 /
   黑体 / 宋体；Linux：Noto Sans CJK / 文泉驿）。

没有可用字体时 PDF 生成会给出明确错误，HTTP 服务仍可启动。

## 本地接口

只读 JSON 接口位于 `http://127.0.0.1:8765/api/v1`，详见
[docs/api.md](docs/api.md)。请求令牌保存在 `config.json` 的 `http.token`，
通过 `Authorization: Bearer <token>` 或 `X-Api-Token` 传递。

## 构建与 CI

```bash
./scripts/build.sh                  # 当前平台：dist/tengshou + dist/tengshou-cli
powershell -File scripts/build-windows.ps1   # Windows zip 产物
```

GitHub Actions：
- push / PR：静态检查 + 单元与集成测试（含真实中文 PDF 生成）；
- 构建 Linux / Windows / macOS CLI 与 macOS / Windows 桌面壳；
- `v*` 标签：把产物发布为 GitHub pre-release。

## 开发

```bash
go test ./internal/... ./cmd/tengshou-cli
go vet ./internal/... ./cmd/tengshou-cli
./scripts/demo.sh        # 一键示例
```

目录：

```
cmd/tengshou/           Wails v3 桌面壳
cmd/tengshou-cli/       无界面命令行
internal/app/           资源装配
internal/config/        配置与数据目录
internal/store/         SQLite、迁移、只读查询
internal/httpapi/       本地 HTTP 服务、台账界面、签名页与应用接口
internal/sign/          一次性签名会话与手写 PNG 归一化
internal/seal/          公章图片校验、白底透明化与裁剪
internal/service/       可写业务层：字段、档案、合同、签名、模板、Excel、变更记录
internal/doctopdf/      Word → PDF 转换（Word COM / LibreOffice headless）
internal/overlay/       PDF 套印引擎：定位文字、签名与公章并输出最终 PDF
internal/mcp/           B6 MCP stdio 连接器与回环 HTTP 客户端
internal/xlsx/          Excel 模板生成、台账导出与解析
internal/pdfdoc/        HTML 解析 → 排版 → PDF
internal/demo/          示例数据与签名 PNG 生成
internal/model/         稳定数据结构（对外 JSON 契约）
```

## 范围边界

- 不做云服务、多租户、跨单位协同；全部数据留在本机。
- 不代替上级办文 / 采购流程；公章只作为图片落到 PDF 指定位置，不承担其它用途。
- 不内置外部台账模板引擎；外部格式经只读数据接口 / MCP 交给用户侧 agent，
  由 agent 自行填写。
- B3 模板做得是「Word 是唯一编辑入口 → PDF 高保真 → 位置映射」；不做 HTML
  直接编辑、不做向导式生成、v0.1 不做动态循环表格。PDF 模板需要本机有 Word
  （Windows）或 LibreOffice 才能从 .docx 转换，否则可导入已导出的 PDF。
- B6 MCP 连接器已交付：只读数据 + 新建字段 / 档案 / 合同草稿，不含删除、修改
  或批量破坏性写接口。
- 手机扫码页 B5 只做手写签名；手机填表 / 信息采集由 D7 边界决定。
- Excel 导入只接受 `.xlsx`，单次最多 10000 行。
- Windows 安装包、代码签名与 Microsoft Store 上架属于 B10；B1 只保证
  Windows 构建链在 CI 上可用。

## 许可证

尚未确定；在确定前请勿对外分发。

# 架构说明（B1 工程地基）

## 设计目标

- **单机离线**：数据与模板都在本机，无对外服务器；电脑可正常上网。
- **一次录入**：合同结构化数据是唯一事实来源，PDF 与台账都由它产出。
- **可替换的壳**：Wails 桌面壳只是窗口；同一套本地 Web UI 与 HTTP 服务
  也可用 CLI 启动，便于测试、演示与无图形环境运行。
- **为后续 bead 留接口**：B2 动态字段、B3 模板管理、B5 签名页、B6 MCP、
  B9 备份恢复、B10 Windows 打包都基于本层扩展。

## 组件

```
┌──────────────────────────────┐
│ cmd/tengshou（Wails v3 壳）   │
│  ├─ 应用窗口（WebView）       │
│  └─ Assets.Handler ───────┐  │
└───────────────────────────┼──┘
                            ▼
┌──────────────────────────────────────────────┐
│ internal/httpapi（本机 HTTP 服务）            │
│  GET /                测试页（公开）          │
│  GET /app/            台账应用（本机界面）    │
│  GET /api/v1/health   健康检查（公开）        │
│  GET /api/v1/*        只读数据（需令牌）      │
│  /app/api/v1/*        应用写入面（仅回环）    │
└───────────────┬──────────────────────────────┘
                │ service：校验 / 编号 / 变更记录
┌───────────────▼──────────────┐   ┌──────────────────────────┐
│ internal/service             │   │ internal/xlsx            │
│  字段 / 档案 / 合同 / Excel  │   │  动态模板 / 台账 / 解析  │
└───────────────┬──────────────┘   └──────────────────────────┘
                │ 只读连接（mode=ro + query_only）
┌───────────────▼──────────────┐   ┌──────────────────────────┐
│ internal/store（SQLite）      │   │ internal/pdfdoc           │
│  迁移 / 写入 / 只读查询       │◄──┤  HTML → 布块 → 排版 → PDF │
│  parties/contracts/...        │   │  中文 / 分页 / 图片 / 表格 │
└───────────────▲──────────────┘   └──────────────┬───────────┘
                │                                  │
┌───────────────┴──────────────┐   ┌───────────────▼───────────┐
│ internal/doctopdf             │   │ internal/overlay          │
│  Word COM / LibreOffice → PDF │   │  套印层 → pdfcpu 合成     │
└──────────────────────────────┘   └───────────────────────────┘

┌──────────────────────────────┐   ┌───────────────────────────┐
│ cmd/tengshou-cli             │   │ internal/demo             │
│  demo / serve / render /     │   │  示例数据 + 签名 PNG      │
│  backup / mcp                │   └───────────────────────────┘
└──────────────────────────────┘
```

`internal/app` 负责装配：配置 → 日志 → 数据库 → 只读连接 → PDF 生成器（懒加载）。

`tengshou-cli mcp` 是 B6 的 stdio MCP 连接器：不打开 SQLite，经回环 HTTP
读取 `/api/v1`、写入 `/app/api/v1`；写工具受 `mcp.write_enabled` 控制，变更
记录 actor 为 `tengshou-mcp`。连接器自身不监听端口、不接受非回环地址。

## 数据模型

| 表 | 作用 |
| --- | --- |
| `parties` | 相对方档案（个人 / 组织），`data_json` 存动态字段 |
| `contracts` | 合同台账（`contract_no` 唯一不可变、`year`、类型、金额、双方、状态、日期、`data_json`；`source_contract_id` 记录续签来源，仅创建时写入且不进入内容指纹） |
| `field_definitions` | 可配置字段定义（实体、键、类型、必填、默认值、台账/模板开关、内置、归档、排序） |
| `change_log` | 追加式变更记录（谁、何时、改了什么、字段级 diff） |
| `templates` | 模板版本，按 `(name, version)` 唯一；`kind=html` 存 HTML，`kind=pdf` 存原始 Word/PDF 路径、转换 PDF、页数与位置映射（`mapping_json`） |
| `signature_assets` | 签名图片元数据（路径、SHA-256、尺寸、采集时间、合同与内容版本关联） |
| `output_documents` | 已生成 PDF（合同、模板版本、路径、摘要、页数） |
| `schema_migrations` | 迁移记录 |
| `app_meta` | 应用元数据键值 |

设计取舍：

- **内置字段 + JSON 扩展**：合同编号、年度、名称、类型、相对方、金额、日期、状态
  映射为一等列；用户字段存 `data_json`。两者都由 `field_definitions` 驱动表单、
  Excel 与模板，运行时新增字段无需迁移表结构。
- **签名与模板解耦**：签名是独立资产，合同只引用；同内容换模板重出 PDF
  不需要重新签名（对应需求「同内容换壳」）。签名记录同时保存采集时的
  `content_sha256`，可判断它对应哪一版合同内容。
- **签名采集与流程解耦**：签名是数据而非流程步骤；手机扫码页只负责采集，
  合同是否生效、什么时候出 PDF 由业务决定。
- **迁移文件嵌入二进制**：`internal/store/migrations/*.sql` 按版本顺序执行，
  空库 / 旧库都能升级；迁移在事务内完成并记录。

## PDF 流水线

1. `html/template` 以 `TemplateData` 渲染模板（严格模式：缺失键报错）。
2. `ParseHTML` 把 HTML 解析为受支持的块模型（标题、段落、列表、表格、图片、
   分页、水平线），解析有限的行内样式。
3. `Render` 在 A4 画布上排版：中文按字断行、行首行尾标点避让、西文按词断行；
   段落跨页、表格行跨页（重复表头）、图片按可用空间缩放换页。
4. `WritePDFFile` 用 `tdewolff/canvas` 的 PDF 渲染器输出，字体自动子集化，
   写入临时文件后原子重命名，同时计算 SHA-256 与页数。
5. 生成记录写入 `output_documents`。

**文字可提取性**：CJK 字体在 GSUB 替换后可能产生「无反向 cmap」的字形，
导致 PDF 的 ToUnicode 缺字。流水线显式关闭 `ccmp/locl/vert/...` 替换，
保证每个汉字都能被复制 / 搜索 / 提取。

## 应用写入面与 Excel

- `/app/` 是内嵌的单页台账界面：年度切换、合同/档案表单、字段管理、变更记录、
  Excel 模板/导入/导出、PDF 模板导入与拖框标定。纯原生 JS，无构建步骤；PDF.js
  随包本地提供，CSP 只允许自身脚本、样式与同源 worker。
- `/app/api/v1/*` 是唯一写入面：仅本机回环、写请求需 `X-Tengshou-App: 1`、
  校验同源/回环/桌面 WebView 的 Origin；所有写入经 `internal/service`。
- Excel 模板与台账列由当前字段定义生成；导入支持列名映射、预览、错误行报告、
  同编号合并更新（空白单元格保持原值）。导出→改→导回与 B14 共用合并语义。
- 续签（B7）走 `GET /app/api/v1/contracts/{id}/renewal-draft`：服务端返回预填的
  新建合同输入与来源摘要，保存时在 `POST /app/api/v1/contracts` 带
  `source_contract_id`。续签只复制合同字段与动态数据，不复制签名 / 公章 / PDF；
  新合同仍按 B4 预检与签名流程出件。详见 [b7-renewal.md](b7-renewal.md)。

## PDF 模板与套印（B3）

1. 导入 `.docx`：Windows 优先用 Word COM（PowerShell 调用 Word.Application）导出
   PDF，其他情况用 LibreOffice headless；也可直接导入用户已导出的 PDF。
2. `internal/overlay` 用 tdewolff/canvas 生成与模板页等尺寸的透明套印层：按标定
   位置绘制中文文字（自动换行/缩字号）、签名与公章 PNG。
3. pdfcpu v0.11.0 以多页 PDF 水印方式把套印层逐页贴到模板 PDF（Scale=1、
   左下角对齐、无旋转），保持原 PDF 的矢量内容与分页。
4. 预览与生成都走同一套渲染；生成前必须持有针对该合同内容与模板版本的预览摘要。
5. 模板每个版本一行，合同 `template_id` 固定具体版本，旧合同按旧版本重出。

细节（坐标、绑定键、版本、接口与警告）见 [b3-templates.md](b3-templates.md)。

## 本地服务与安全

- 默认绑定 `0.0.0.0:8765`，供同一局域网手机打开测试页 / 签名页；`--loopback` 或
  `allow_lan=false` 时只绑定 `127.0.0.1`。
- 测试页与 `/api/v1/health` 公开；数据端点要求
  `Authorization: Bearer <token>`（或 `X-Api-Token`）。
- 令牌首次运行自动生成并写入 `config.json`（0600）。
- 服务只注册 GET 路由；路由层天然拒绝其它方法，事务层另有 `PRAGMA query_only`
  的只读连接，双重保证无写入面。
- **B5 签名页**是唯一对局域网开放的写入口：`/sign/<token>` 使用一次性短时令牌，
  路由守卫拒绝公网来源（只接受回环 / 私网 / 链路本地地址），提交要求 JSON +
  `X-Tengshou-Sign` 头（强制预检），CSP 禁止外部资源。应用写接口 `/app/api/*`
  仍只允许回环。
- **B6 MCP 写工具**复用 `/app/api/v1`：带 `X-Tengshou-MCP: 1` 的写请求在
  `mcp.write_enabled=false` 时返回 403，并以 actor=tengshou-mcp 记入变更记录；
  连接器只接受回环地址，自身不监听端口。详见 [mcp.md](mcp.md)。
- **B18 实时同步**：`GET /app/api/v1/events`（SSE）以 change_log 为游标推送
  外部写入，界面按实体增量刷新；SSE 不可用时前端降级为 3 秒轮询。MCP 写开关
  由 `config.Settings` 运行时持有并持久化到 `config.json`，切换即时生效；
  合同 / 档案详情与变更记录页展示谁改了什么，并区分人工与 Agent 来源。详见
  [b18-realtime-audit.md](b18-realtime-audit.md)。
- 响应带 `nosniff`、`no-referrer`、`X-Frame-Options: DENY`、页面 CSP。
- HTTP 服务设置读写 / 空闲超时与最大请求头，避免慢连接占用。

## 配置与日志

- `config.json`：HTTP 端口 / 绑定 / 令牌、PDF 字体与页边距、日志级别与大小、
  签名有效期、MCP 写开关（`mcp.write_enabled`，默认开）。
- 环境变量：`TENGHSHOU_DATA_DIR`、`TENGHSHOU_FONT`。
- 日志：`log/slog` JSON 输出到 `<data>/logs/tengshou.log` 并镜像 stderr；
  启动时超过大小阈值轮转为 `.1`。

## 备份

`VACUUM INTO` 生成一致性快照到 `<data>/backups/tengshou-YYYYMMDD-HHMMSS.db`。
备份文件是完整独立数据库，可直接打开或替换恢复。自动定时备份属于 B9。

## 平台与构建

- Windows / macOS / Linux 均可编译；SQLite 使用纯 Go 驱动，Windows 桌面壳
  在 `CGO_ENABLED=0` 下交叉编译通过。
- macOS 桌面壳需要 cgo + 系统 WebKit（本机构建已通过）。
- Wails v3 面向 Windows 使用纯 Go WebView2 绑定，无需 mingw。
- 安装包、签名、代码签名与 Microsoft Store 上架属于 B10；B1 交付可执行文件
  构建链与 CI 产物。

## 扩展点

| 后续 bead | 在本层的落点 |
| --- | --- |
| B2 数据底座 / 动态字段 | 已交付：`field_definitions` + `/app/` + service + xlsx |
| B3 模板管理 | 已交付：Word → PDF 转换、PDF.js 标定、pdfcpu 套印、版本与影响统计；见 [b3-templates.md](b3-templates.md) |
| B5 局域网签名页 | 已交付：`internal/sign` + `/sign/<token>` + 一次性令牌；签名关联合同与内容版本，`signaturePath` 模板占位 |
| B7 续签与档案复用 | 已交付：续签草稿端点 + `contracts.source_contract_id` 溯源 + 档案历史合同；不复制签名，第三层按 D3 不提供；见 [b7-renewal.md](b7-renewal.md) |
| B6 MCP 连接器 | 已交付：`tengshou-cli mcp` stdio 服务 + `internal/mcp`；读 `/api/v1`，写经 `/app/api/v1`，不直接开库；见 [mcp.md](mcp.md) |
| B9 备份恢复 | `store.Backup` 已有；恢复 = 校验 + 原子替换 + 重启 |
| B10 Windows 打包 | `scripts/build-windows.ps1` + CI 产物；补安装器 / 签名 |

## 已知限制

- HTML 子集有限：不支持 CSS 文件 / 选择器、浮动布局、页眉页脚自定义、
  行内图片（图片需独立成段或块级）。
- 字体依赖系统或用户提供；不内置字体文件（许可与体积原因）。
- 测试页只做连通性验证，不展示数据；签名页见 [b5-signature.md](b5-signature.md)。
- Windows 产物尚未在干净 Windows 环境实机验证（B1 验收允许后置到 B10 前）。

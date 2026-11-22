# 腾手（tengshou）

让村里的签合同变成录台账：合同数据录一次，程序产出合同 PDF 与自有台账；
外部表格格式通过只读数据接口交给用户侧 AI agent 处理，核心不做平台适配。

当前仓库处于 **B1 工程地基**：桌面壳、本地服务、SQLite、HTML→PDF 流水线、
构建与 CI。业务功能（相对方档案、模板管理、签名页等）按 bead 后续交付。

## 当前能力（B1）

- **Wails v3 桌面壳**：`cmd/tengshou`，窗口直接加载本地 Web UI。
- **内嵌 Go 本地服务**：默认监听 `0.0.0.0:8765`，同一局域网手机可打开测试页；
  数据接口需令牌，测试页与健康检查公开。
- **SQLite 持久化**：纯 Go 驱动（无 cgo），内置迁移；空库 / 旧库均可升级；
  重启不丢数据。
- **HTML → PDF**：模板 + 合同数据生成带页码的 A4 PDF，支持中文、表格、
  分页、图片嵌入（签名图片）。
- **示例闭环**：一条合同数据 + 一份 HTML 模板 + 一张签名图片 → 一份 PDF。
- **只读数据接口**：`/api/v1/*`，供 B6 MCP 连接器与用户侧 agent 取数。
- **备份约定**：`VACUUM INTO` 备份到 `<data>/backups/`。
- **构建与发布**：`scripts/build.sh`、`scripts/build-windows.ps1`；
  GitHub Actions 测试 + 多平台构建，打 `v*` 标签时发布 pre-release。

## 快速开始

需要 Go 1.26+。生成示例数据与 PDF：

```bash
go run ./cmd/tengshou-cli demo --data-dir ./data
```

启动本地服务与局域网测试页：

```bash
go run ./cmd/tengshou-cli serve --data-dir ./data
# 终端会列出本机 / 局域网地址与数据接口令牌
```

启动桌面应用：

```bash
go run ./cmd/tengshou
# 或先构建：./scripts/build.sh && ./dist/tengshou
```

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
  config.json     # 服务端口、API 令牌、字体、页边距
  tengshou.db     # SQLite 数据库（WAL）
  assets/         # 签名图片等模板资源
  templates/      # 用户 HTML 模板
  outputs/        # 生成的 PDF
  backups/        # 数据库备份
  logs/           # JSON 日志（启动时按大小轮转）
  fonts/          # 可选：用户自带中文字体（优先于系统字体）
```

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
internal/httpapi/       本地 HTTP 服务与测试页
internal/pdfdoc/        HTML 解析 → 排版 → PDF
internal/demo/          示例数据与签名 PNG 生成
internal/model/         稳定数据结构（对外 JSON 契约）
```

## 范围边界

- 不做云服务、多租户、跨单位协同；全部数据留在本机。
- 不代替上级办文 / 采购流程，不做公章法定电子认证（只处理图片落位）。
- 不内置外部台账模板引擎；外部格式经只读数据接口交给用户侧 agent。
- 本版本（B1）不包含签名采集页、模板编辑器、Excel 导入、MCP 连接器；
  这些分别在 B5 / B3 / B2 / B6 交付。
- Windows 安装包、代码签名与 Microsoft Store 上架属于 B10；B1 只保证
  Windows 构建链在 CI 上可用。

## 许可证

尚未确定；在确定前请勿对外分发。

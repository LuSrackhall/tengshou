# B1 验收对照

B1（te-h7l.11）范围：Wails v3 桌面壳 + 本地服务 + SQLite + HTML→PDF +
构建 / 发布流水线，无外部服务器依赖。

| 验收项 | 实现 | 验证 |
| --- | --- | --- |
| 开发机可启动桌面应用 | `cmd/tengshou`（Wails v3），窗口加载本地 UI | 本机启动 `dist/tengshou`，窗口创建，`/api/v1/health` 与 `/` 均 200，SIGTERM 干净退出 |
| 本地服务可被同局域网手机访问测试页 | `httpapi` 默认绑定 `0.0.0.0`，`/` 公开测试页 | 启动后列出局域网地址（如 `http://192.168.x.x:8765/`）；`GET /` 返回 200；手机访问受网络环境限制，未在真机复测 |
| Windows 构建可用 | `tools/tengshoubuild` 的 Windows 目标；CI `desktop` job（windows-latest） | 本机 `GOOS=windows CGO_ENABLED=0 go build` 通过；CI 产出 `tengshou-windows-amd64.zip` |
| 干净 Windows 环境验证 | 后置到 B10 前（B1 描述允许） | 待 B10 |
| SQLite 持久化，重启不丢数据 | `internal/store`，WAL，写入路径与只读连接分离 | `TestPartyContractRoundTripAndPersistence`（关闭重开校验） |
| 迁移可在空库 / 旧库执行 | `0001_init.sql` + `0002_api_indexes.sql`，事务内迁移 | `TestMigrationsOnEmptyDatabase`、`TestMigrationFromOldDatabase` |
| 样例 HTML 模板生成 PDF | `internal/demo/assets/contract_basic.html` + `pdfdoc` | `TestRenderChineseContractPDF`（≥2 页、中文字形无缺失、图片嵌入、文本可提取）；`tengshou-cli demo` 实跑 |
| 中文字体 | 系统 / 用户字体自动解析，强制关闭导致 ToUnicode 缺字的 GSUB 替换 | 同上；`Missing` 为空；PDF 文本提取包含「甲方 / 房屋租赁合同」 |
| 分页 | 段落 / 表格 / 图片自动换页，重复表头，页脚页码 | `TestRenderHardPageBreak`、`TestRenderTableAcrossPages`、demo PDF（第 1 页 / 共 2 页） |
| 签名图片落位 | `assets/signatures/*.png` 安全解析与嵌入 | demo + PDF 内嵌图片断言 |
| 一键构建工具 | `tools/tengshoubuild`（三端同一 Go 命令） | 本机执行 `go run ./tools/tengshoubuild build` 产出桌面壳与 CLI |
| 基础测试通过 | `go test ./internal/... ./cmd/tengshou-cli` | 见下方最终验证 |
| 不依赖外部服务器 | 全部本地；SQLite 纯 Go；PDF 纯 Go；无运行时网络调用 | 代码审查 + 测试（无网络依赖） |
| 稳定只读数据 API（B6 前置） | `/api/v1/*`，`tengshou.data.v1` 契约，Bearer 令牌，双重只读 | `internal/httpapi` 测试；`docs/api.md`；`/api/v1/schema` |
| GitHub Actions + pre-release | `.github/workflows/ci.yml`：测试、多平台构建、`v*` 标签发布 pre-release | 工作流文件；本地无法触发远端，待首次 push / tag |

## 最终验证命令

```bash
go vet ./internal/... ./cmd/tengshou-cli
go test -count=1 ./internal/... ./cmd/tengshou-cli
go run ./tools/tengshoubuild build
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build \
  -trimpath -ldflags "-s -w -H windowsgui" -o dist/tengshou-windows-amd64.exe ./cmd/tengshou
```

结果：全部通过（详见实现摘要的 Verification 段）。

## 未覆盖 / 风险

- Windows 实机运行、安装包、签名、商店上架：B10。
- 手机真机访问：受当前工作环境网络限制，仅验证监听地址与 HTTP 200。
- 模板能力为受控 HTML 子集，非完整浏览器排版；见 `docs/templates.md`。
- 系统字体嵌入受字体许可约束；正式分发前需确定字体策略（B10 / 法务）。

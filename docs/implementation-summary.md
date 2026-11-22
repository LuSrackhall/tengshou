---
schema: gc.build.implementation-summary.v1
workflow:
  id: te-20g
  formula: do-work
methodology:
  pack: gascity
  name: build-basic
producer:
  formula: do-work
  stage: implement
  attempt: 1
status: approved
trace:
  upstream:
    - path: beads/te-h7l.11
      hash: bead:te-h7l.11
      ids:
        - REQ-001
        - REQ-002
        - REQ-003
        - REQ-004
        - REQ-005
        - REQ-006
        - REQ-007
        - REQ-008
        - REQ-009
        - REQ-010
        - REQ-011
    - path: internal/pdfdoc/pdf.go
      hash: sha256:cce5a13d84b2491d1b20d8cd03a2b27758bb18a872a90a898545f552012c70f6
    - path: internal/store/store.go
      hash: sha256:026d7406a08a35e5e801e40838b9a105c3bbf63e418177a1dc14763b39af2efa
    - path: .github/workflows/ci.yml
      hash: sha256:7649747c354b64a7dc35183a73dcf8b458ce4fd59668162f53cb986e6b88c318
    - path: internal/demo/assets/contract_basic.html
      hash: sha256:7cfaac26fb9247004d158b8ba3acdd2bbccca0f385512938fa136713888641c8
  coverage:
    - id: REQ-001
      status: covered
    - id: REQ-002
      status: covered
    - id: REQ-003
      status: covered
    - id: REQ-004
      status: covered
    - id: REQ-005
      status: covered
    - id: REQ-006
      status: covered
    - id: REQ-007
      status: covered
    - id: REQ-008
      status: covered
    - id: REQ-009
      status: covered
    - id: REQ-010
      status: covered
    - id: REQ-011
      status: deferred
      rationale: B1 bead 明确允许 Windows 干净环境实机验证后置到 B10 前；本次交付 Windows 构建脚本与 CI 构建产物，实机运行与安装包留给 B10。
---

## Summary

需求覆盖对照：

| ID | Status |
| --- | --- |
| REQ-001 | covered |
| REQ-002 | covered |
| REQ-003 | covered |
| REQ-004 | covered |
| REQ-005 | covered |
| REQ-006 | covered |
| REQ-007 | covered |
| REQ-008 | covered |
| REQ-009 | covered |
| REQ-010 | covered |
| REQ-011 | deferred |

本步完成 B1 工程地基：在空仓库中建立 Go 模块与三层结构（Wails v3 桌面壳 /
无界面 CLI、内嵌本地 HTTP 服务、SQLite + 只读数据 API），并实现受控 HTML
子集到 A4 PDF 的纯 Go 流水线（中文字体、分页、表格、签名图片嵌入）。示例
闭环可一条命令产出「一条合同数据 + 一份 HTML 模板 + 一张签名图片 → 一份
2 页 PDF」。代码提交 `537026d`。

关键决策：

- Wails 只做窗口，桌面 UI 与局域网测试页复用同一个 `http.Handler`，
  因此同一套逻辑可用 CLI 启动、测试与演示。
- SQLite 使用纯 Go 驱动 `modernc.org/sqlite`，Windows 桌面壳在
  `CGO_ENABLED=0` 下可构建；迁移 SQL 嵌入二进制，空库/旧库均升级。
- PDF 使用 `tdewolff/canvas` 纯 Go 渲染；显式关闭 CJK 字体 GSUB 替换
  （`ccmp/locl/vert/...`），避免 ToUnicode 缺字导致复制/搜索出现 U+FFFD。
- 数据接口只注册 GET 路由，并使用 `mode=ro` + `PRAGMA query_only` 的独立
  只读连接，双重保证无写入面；令牌首次运行生成，测试页不展示合同数据。
- 合同 / 相对方保留 `data` JSON 扩展字段，为 B2 动态字段留空间；签名资产
  与模板解耦，支持「同内容换壳」。

## Intended Behavior

- `tengshou-cli demo --data-dir <dir>`：创建数据目录、示例相对方、签名 PNG、
  模板与合同，渲染 2 页 PDF，并把输出记录写入数据库；可重复执行。
- `tengshou-cli serve`：启动 `0.0.0.0:8765` 本地服务，打印本机 / 局域网地址
  与 API 令牌；手机浏览器可打开公开测试页；数据接口需 Bearer 令牌。
- `tengshou-cli render --contract <id>`：按合同与模板重新生成 PDF 并登记。
- `tengshou-cli backup`：`VACUUM INTO` 备份数据库到 `backups/`。
- `tengshou` 桌面应用：启动本地服务并打开窗口加载同一 UI；端口被占用时
  在后续 9 个端口内自动回退；SIGTERM 干净退出。
- PDF：A4、20mm 页边距、页脚「第 N 页 / 共 M 页」、次要页页眉；段落/表格/
  图片自动跨页，表格表头重复；中文按字断行并做基础标点避让。
- API：`/api/v1/{health,schema,stats,parties,contracts,templates,signature-assets,documents}`，
  契约版本 `tengshou.data.v1`，分页上限 500。

## Changed Files

| 文件 | 说明 |
| --- | --- |
| `cmd/tengshou/main.go` | Wails v3 桌面壳、端口回退、优雅退出 |
| `cmd/tengshou-cli/main.go` | demo / serve / render / backup / version |
| `internal/app/app.go` | 配置、日志、数据库、只读连接、PDF 生成器装配 |
| `internal/config/config.go` | 数据目录布局、config.json、令牌、字体与端口归一化 |
| `internal/logging/logging.go` | slog JSON 文件日志与启动轮转 |
| `internal/model/model.go` | 对外稳定的数据结构与 JSON 契约 |
| `internal/store/store.go` | SQLite 打开、WAL、迁移执行、备份 |
| `internal/store/reader.go` | 只读查询：列表、详情、统计、schema 版本 |
| `internal/store/writer.go` | 相对方/合同/模板/签名/输出记录写入 |
| `internal/store/migrations/0001_init.sql` | 基础 schema 与索引 |
| `internal/store/migrations/0002_api_indexes.sql` | API 索引与 app_meta |
| `internal/httpapi/server.go` | 本地服务、监听、URL、日志/安全/恢复中间件、令牌认证 |
| `internal/httpapi/handlers.go` | 只读 JSON 端点与测试页 |
| `internal/httpapi/schema.go` | `tengshou.data.v1` 字段说明 |
| `internal/httpapi/web/index.html` | 局域网连通性测试页 |
| `internal/pdfdoc/fonts.go` | 系统/用户 CJK 字体解析、字形覆盖检查、GSUB 关闭 |
| `internal/pdfdoc/html.go` | 受控 HTML 子集与内联样式解析 |
| `internal/pdfdoc/layout.go` | 中文断行、分页、表格、图片、页眉页脚 |
| `internal/pdfdoc/pdf.go` | 模板执行、PDF 原子写入、SHA-256、变量提取 |
| `internal/demo/demo.go` | 示例数据、签名 PNG 生成、端到端渲染 |
| `internal/demo/assets/contract_basic.html` | 两页示例租赁合同模板 |
| `internal/*/*_test.go` | 配置、存储、迁移、API、PDF、demo 集成测试 |
| `docs/api.md` / `docs/architecture.md` / `docs/templates.md` / `docs/b1-acceptance.md` | 接口、架构、模板与验收说明 |
| `.github/workflows/ci.yml` | 测试、多平台构建、`v*` 标签 pre-release |
| `scripts/build.sh` / `scripts/build-windows.ps1` / `scripts/demo.sh` | 一键构建与示例 |
| `Makefile` / `README.md` / `go.mod` / `go.sum` | 工程入口与依赖锁定 |

## Verification

首个验证命令（PDF 中文可提取性探针）：

```
TENGHSHOU_FONT="/System/Library/Fonts/Supplemental/Arial Unicode.ttf" \
  go test ./internal/pdfdoc/ -run TestRenderChineseContractPDF -v
```

首次运行暴露问题：PDF 渲染成功，但文本提取出现 U+FFFD——CJK 字体 GSUB
把部分汉字替换成无反向 cmap 的变体字形，ToUnicode 缺项。修复为加载字体后
关闭 `ccmp/locl/vert/vrt2/fwid/...` 替换。修复后结果：`PASS`，PDF 文本
包含「甲方」「房屋租赁合同」，2 页、含内嵌图片、`Missing` 为空。

最终证明命令与观察结果：

```
go vet ./internal/... ./cmd/tengshou-cli          # 无输出，通过
go test -count=1 ./internal/... ./cmd/tengshou-cli
# ok config / demo / httpapi / pdfdoc / store；其余包无测试文件
./scripts/build.sh
# 产出 dist/tengshou (20M) 与 dist/tengshou-cli (18M)
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build \
  -trimpath -ldflags "-s -w -H windowsgui" \
  -o dist/tengshou-windows-amd64.exe ./cmd/tengshou
# 产出 22M Windows 可执行文件
./dist/tengshou-cli demo --data-dir /tmp/ts-final-demo
# 2 页 PDF，字体 Arial Unicode.ttf，签名图片 assets/signatures/demo-signature.png
```

桌面运行时验证（`TENGHSHOU_DATA_DIR=/tmp/ts-final-desktop ./dist/tengshou`）：

- `GET /api/v1/health` → 200 `{"database_schema":2,"status":"ok",...}`
- `GET /` → 200，包含「本地服务运行中」
- 未带令牌 `GET /api/v1/stats` → 401
- 带令牌 `GET /api/v1/stats` → 200 `{...}`
- SIGTERM 后进程干净退出，端口释放

## Remaining Risks

- Windows 实机运行、安装包、代码签名与 Microsoft Store 上架未验证；B1
  允许后置到 B10 前，本次只交付构建脚本与 CI 产物。
- 手机真机访问未复测：当前环境仅确认监听 `0.0.0.0:8765`、局域网 URL
  生成与测试页 200；防火墙 / AP 隔离等现场因素需实地确认。
- 字体依赖系统或用户提供：正式分发前需确定可再分发的字体与嵌入许可策略。
- HTML 模板为受控子集（无外部 CSS、浮动、页内混排图片），复杂模板需在
  B3 模板管理中评估扩展。
- `tdewolff/canvas` 与 `modernc.org/sqlite` 为较大依赖；canvas 使用伪版本
  （该仓库无稳定 tag），升级需回归 PDF 中文与分页测试。
- Wails v3 桌面壳在 macOS 依赖 cgo/WebKit；Windows 使用纯 Go 构建。桌面
  窗口的视觉验收（WebView 实际渲染）未截图存档，仅验证进程、窗口创建与
  HTTP 端点。
- 本地服务默认对局域网开放测试页；数据接口有令牌保护，但令牌在终端与
  `config.json` 中为明文（单机工具的权衡，B5 签名页需再评估会话与 CSRF）。
